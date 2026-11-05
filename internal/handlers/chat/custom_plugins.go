package chat

import (
	json "9router/proxy/internal/fastjson"
	"9router/proxy/internal/customplugins"
	"9router/proxy/internal/log"
)

// loadCustomPlugins returns the parsed plugin config from the settings blob.
// A read failure falls back to empty (all plugins off): the request path must
// never fail a forward because a settings row was corrupt. It is read per
// request (a single settings row scan) and deliberately not cached — the
// dashboard writes the blob through the same repo, so a cache would need
// invalidation wiring for almost no gain.
func (h *ChatHandler) loadCustomPlugins() customplugins.Config {
	cfg := customplugins.Defaults()
	if h != nil && h.Repo != nil {
		if raw := h.Repo.GetCustomPlugins(); raw != nil {
			cfg = customplugins.ParseConfig(raw)
		}
	}
	return cfg
}

// applyCustomPlugins runs the enabled custom plugins against an outgoing
// request body. It returns the body unchanged when nothing applies — the
// common case with the feature disabled, which must stay cheap.
func (h *ChatHandler) applyCustomPlugins(body []byte, provider string) []byte {
	cfg := h.loadCustomPlugins()
	if cfg == nil || !cfg.AnyEnabled() {
		return body
	}
	// Resolve the requested model from the body (the only place it exists on
	// this lane — the forward path does not carry the resolved model id).
	model, requested := modelFromBody(body)
	out := customplugins.Apply(body, provider, "", requested, cfg)
	if !bytesEqual(out, body) {
		log.Debug("customplugins", "provider", provider, "model", model, "changed", true)
	}
	return out
}

func modelFromBody(body []byte) (model, requested string) {
	var doc struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", ""
	}
	return doc.Model, doc.Model
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
