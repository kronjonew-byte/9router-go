package dashboard

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
)

var harvesterState = struct {
	sync.Mutex
	running             bool
	loopIntervalMinutes int
	targetCount         int
	lastRunAt           string
	lastAdded           int
	lastTested          int
	lastMessage         string
	timer               *time.Timer
	inCycle             bool
}{
	lastMessage: "Idle",
}

const tcpPrecheckTimeout = 800 * time.Millisecond
const probeTimeout = 2500 * time.Millisecond

var harvestFeeds = []string{
	"https://raw.githubusercontent.com/stormsia/proxy-list/main/working_proxies.txt",
	"https://raw.githubusercontent.com/jetkai/proxy-list/main/online-proxies/txt/proxies-http.txt",
	"https://raw.githubusercontent.com/sunny9577/proxy-scraper/master/generated/http_proxies.txt",
	"https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/http.txt",
	"https://raw.githubusercontent.com/vakhov/fresh-proxy-list/master/http.txt",
	"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
	"https://raw.githubusercontent.com/clarketm/proxy-list/master/proxy-list-raw.txt",
	"https://api.proxyscrape.com/v4/free-proxy-list/get?request=display_proxies&proxy_format=ipport&format=text&timeout=2500",
	"https://raw.githubusercontent.com/prxchk/proxy-list/main/http.txt",
	"https://raw.githubusercontent.com/roosterkid/openproxylist/main/HTTPS_RAW.txt",
}

func tcpPrecheckAsync(hostPort string) bool {
	conn, err := net.DialTimeout("tcp", hostPort, tcpPrecheckTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func fetchCandidates(ctx context.Context, target int) []string {
	client := &http.Client{Timeout: 4 * time.Second}
	candidates := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, feedURL := range harvestFeeds {
		wg.Add(1)
		go func(urlStr string) {
			defer wg.Done()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0")
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				return
			}
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				clean := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "http://"), "https://"), "socks5://"), "socks4://")
				clean = strings.Split(clean, "/")[0]
				if strings.Contains(clean, ":") {
					mu.Lock()
					candidates[clean] = true
					mu.Unlock()
				}
			}
			resp.Body.Close()
		}(feedURL)
	}
	wg.Wait()

	result := make([]string, 0, len(candidates))
	for c := range candidates {
		result = append(result, c)
	}
	return result
}

func scheduleNextHarvestLoop(h *DashboardHandler) {
	harvesterState.Lock()
	if harvesterState.timer != nil {
		harvesterState.timer.Stop()
		harvesterState.timer = nil
	}
	if harvesterState.running && harvesterState.loopIntervalMinutes > 0 {
		duration := time.Duration(harvesterState.loopIntervalMinutes) * time.Minute
		harvesterState.timer = time.AfterFunc(duration, func() {
			harvesterState.Lock()
			running := harvesterState.running
			interval := harvesterState.loopIntervalMinutes
			target := harvesterState.targetCount
			harvesterState.Unlock()

			if running && interval > 0 {
				executeHarvestCycle(h, target)
				scheduleNextHarvestLoop(h)
			}
		})
	}
	harvesterState.Unlock()
}

func executeHarvestCycle(h *DashboardHandler, target int) {
	harvesterState.Lock()
	if harvesterState.inCycle {
		harvesterState.Unlock()
		return
	}
	harvesterState.inCycle = true
	harvesterState.lastRunAt = time.Now().UTC().Format(time.RFC3339)
	harvesterState.lastMessage = "Fetching fresh proxy candidate feeds..."
	harvesterState.Unlock()

	defer func() {
		harvesterState.Lock()
		harvesterState.inCycle = false
		if harvesterState.loopIntervalMinutes == 0 {
			harvesterState.running = false
		}
		harvesterState.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	existingPools, _ := h.Repo.ListProxyPools()
	existingUrls := make(map[string]bool)
	existingEgressIps := make(map[string]bool)
	for _, p := range existingPools {
		if u, ok := p["proxyUrl"].(string); ok {
			existingUrls[strings.ToLower(strings.TrimSpace(u))] = true
		}
		if ip, ok := p["egressIp"].(string); ok && ip != "" {
			existingEgressIps[strings.ToLower(strings.TrimSpace(ip))] = true
		}
	}

	candidates := fetchCandidates(ctx, target)
	var unadded []string
	for _, c := range candidates {
		if !existingUrls["http://"+strings.ToLower(c)] && !existingUrls["https://"+strings.ToLower(c)] {
			unadded = append(unadded, c)
		}
	}

	harvesterState.Lock()
	harvesterState.lastMessage = fmt.Sprintf("Scanning %d candidate proxies (found %d total)...", len(unadded), len(candidates))
	harvesterState.Unlock()

	addedCount := 0
	scannedCount := 0
	var mu sync.Mutex

	sem := make(chan struct{}, 40)
	var wg sync.WaitGroup

	for _, hostPort := range unadded {
		harvesterState.Lock()
		running := harvesterState.running
		interval := harvesterState.loopIntervalMinutes
		harvesterState.Unlock()
		if !running && interval == 0 && addedCount >= target {
			break
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(hp string) {
			defer wg.Done()
			defer func() { <-sem }()

			if addedCount >= target {
				return
			}

			if !tcpPrecheckAsync(hp) {
				mu.Lock()
				scannedCount++
				mu.Unlock()
				return
			}

			proxyURL := "http://" + hp
			proxyParsed, err := url.Parse(proxyURL)
			if err != nil {
				mu.Lock()
				scannedCount++
				mu.Unlock()
				return
			}

			tr := &http.Transport{
				Proxy:             http.ProxyURL(proxyParsed),
				DisableKeepAlives: true,
			}
			defer tr.CloseIdleConnections()
			client := &http.Client{
				Transport: tr,
				Timeout:   probeTimeout,
			}

			ok, _, egressIp, loc := probeViaProxyExtended(ctx, client)
			mu.Lock()
			scannedCount++
			if ok && addedCount < target {
				if egressIp != "" && existingEgressIps[strings.ToLower(egressIp)] {
					// duplicate egress IP, skip
				} else {
					if egressIp != "" {
						existingEgressIps[strings.ToLower(egressIp)] = true
					}
					locSuffix := ""
					if loc != "" {
						locSuffix = fmt.Sprintf("-[%s]", loc)
					}
					name := fmt.Sprintf("FreeProxy-%s%s", strings.ReplaceAll(hp, ":", "_"), locSuffix)

					poolData := db.ProxyPoolData{
						Name:        name,
						ProxyURL:    proxyURL,
						Type:        "http",
						StrictProxy: false,
					}
					if _, err := h.Repo.InsertProxyPool(poolData); err == nil {
						addedCount++
						existingUrls[strings.ToLower(proxyURL)] = true
						harvesterState.Lock()
						harvesterState.lastAdded = addedCount
						harvesterState.lastMessage = fmt.Sprintf("Scanning: %d/%d tested (%d/%d live proxies imported)...", scannedCount, len(unadded), addedCount, target)
						harvesterState.Unlock()
					}
				}
			}
			if scannedCount%25 == 0 {
				harvesterState.Lock()
				harvesterState.lastMessage = fmt.Sprintf("Scanning: %d/%d tested (%d/%d live proxies imported)...", scannedCount, len(unadded), addedCount, target)
				harvesterState.Unlock()
			}
			mu.Unlock()
		}(hostPort)
	}
	wg.Wait()

	harvesterState.Lock()
	harvesterState.lastAdded = addedCount
	harvesterState.lastTested = len(unadded)
	harvesterState.lastMessage = fmt.Sprintf("Finished: Added %d live proxies (tested %d).", addedCount, len(unadded))
	harvesterState.Unlock()
}

func probeViaProxyExtended(ctx context.Context, client *http.Client) (ok bool, latencyMs int64, egressIp string, loc string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://1.1.1.1/cdn-cgi/trace", nil)
	if err != nil {
		return false, 0, "", ""
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, 0, "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return false, 0, "", ""
	}

	latency := time.Since(start).Milliseconds()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ip=") {
			egressIp = strings.TrimPrefix(line, "ip=")
		}
		if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
	}

	return true, latency, egressIp, loc
}

// HandleHarvestProxyPools handles GET (status) and POST (start/configure/stop harvester) for proxy pools.
func (h *DashboardHandler) HandleHarvestProxyPools(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		harvesterState.Lock()
		defer harvesterState.Unlock()
		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"running":             harvesterState.running,
			"targetCount":         harvesterState.targetCount,
			"loopIntervalMinutes": harvesterState.loopIntervalMinutes,
			"lastRunAt":           harvesterState.lastRunAt,
			"lastAdded":           harvesterState.lastAdded,
			"lastTested":          harvesterState.lastTested,
			"lastMessage":         harvesterState.lastMessage,
		})
		return
	}

	if r.Method == http.MethodPost {
		bodyBytes, _ := io.ReadAll(r.Body)
		action := "start"
		target := 5
		loopMinutes := 0

		if len(bodyBytes) > 0 {
			var m map[string]any
			if json.Unmarshal(bodyBytes, &m) == nil {
				if act, ok := m["action"].(string); ok {
					action = act
				}
				if t, ok := m["target"].(float64); ok {
					target = int(t)
				}
				if lm, ok := m["loopMinutes"].(float64); ok {
					loopMinutes = int(lm)
				}
			}
		}

		if action == "stop" {
			harvesterState.Lock()
			harvesterState.running = false
			harvesterState.loopIntervalMinutes = 0
			if harvesterState.timer != nil {
				harvesterState.timer.Stop()
				harvesterState.timer = nil
			}
			harvesterState.lastMessage = "Harvester stopped by user."
			harvesterState.Unlock()

			handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"running": false,
				"message": "Harvester stopped.",
			})
			return
		}

		if target <= 0 {
			target = 5
		}
		if target > 200 {
			target = 200
		}
		if loopMinutes < 0 {
			loopMinutes = 0
		}

		harvesterState.Lock()
		harvesterState.targetCount = target
		harvesterState.loopIntervalMinutes = loopMinutes
		harvesterState.running = true
		harvesterState.Unlock()

		scheduleNextHarvestLoop(h)
		go executeHarvestCycle(h, target)

		handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
			"success":             true,
			"running":             true,
			"targetCount":         target,
			"loopIntervalMinutes": loopMinutes,
			"message":             fmt.Sprintf("Harvester started: Target %d proxies%s. Running in background.", target, func() string {
				if loopMinutes > 0 {
					return fmt.Sprintf(", looping every %dm", loopMinutes)
				}
				return ""
			}()),
		})
		return
	}

	handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

// HandlePruneDeadProxyPools deletes all unbound proxy pools that have failed or are inactive.
func (h *DashboardHandler) HandlePruneDeadProxyPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	pools, err := h.Repo.ListProxyPools()
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	boundCounts := h.countProxyPoolBindings()

	deletedCount := 0
	for _, p := range pools {
		id, _ := p["id"].(string)
		if id == "" {
			continue
		}
		isActive, _ := p["isActive"].(bool)
		testStatus, _ := p["testStatus"].(string)
		isDead := !isActive || testStatus == "error" || testStatus == "failed"

		if isDead && boundCounts[id] == 0 {
			if err := h.Repo.DeleteProxyPool(id); err == nil {
				deletedCount++
			}
		}
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"deletedCount": deletedCount,
		"message":      fmt.Sprintf("Pruned %d dead proxy pool(s)", deletedCount),
	})
}

// HandlePruneOldProxyPools marks proxy pools as inactive if last_verified is older than threshold days.
func (h *DashboardHandler) HandlePruneOldProxyPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		handlerutil.WriteJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	daysStr := r.URL.Query().Get("days")
	if daysStr == "" {
		daysStr = "7"
	}
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		handlerutil.WriteJSONError(w, http.StatusBadRequest, "invalid days parameter")
		return
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	result, err := h.Repo.UpdateProxyPoolsStatusOlderThan(cutoff, "inactive")
	if err != nil {
		handlerutil.WriteJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"affected":    result,
		"message":     fmt.Sprintf("Pruned %d old proxy pool(s) (older than %d days)", result, days),
	})
}
