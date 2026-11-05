// Package customplugins implements the seven X-Router custom plugins in Go,
// operating directly on the OpenAI-compatible request body before it is
// forwarded upstream. Each plugin is a pure function from body bytes to body
// bytes (nil result = no change), so the package has no runtime state and is
// trivially testable. Configuration is persisted in the settings blob under
// "customPlugins" (see Repo methods in the db package).
package customplugins

import (
	"fmt"
	"strings"

	json "9router/proxy/internal/fastjson"
)

// Plugin ids (mirror the X-Router CUSTOM_PLUGIN_KEYS so dashboards and config
// speak one vocabulary).
const (
	PluginImageVision      = "imageVision"
	PluginThinkDeeper      = "thinkDeeper"
	PluginSpeedMode        = "speedMode"
	PluginJsonGuard        = "jsonGuard"
	PluginContextSqueezer  = "contextSqueezer"
	PluginOpenaiToolBridge = "openaiToolBridge"
	PluginAntiSlop         = "antiSlop"
)

// PluginConfig is the per-plugin state stored in settings.customPlugins.<id>.
// Enabled controls whether the plugin runs at all; Models is the allow-list of
// model keys (bare names match any provider prefix). An empty Models list
// means the plugin applies to every model.
type PluginConfig struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
	// AntiSlop only: level is "lite" or "full".
	Level string `json:"level,omitempty"`
}

// Config is the whole custom-plugins settings blob.
type Config map[string]PluginConfig

// Defaults returns the factory-default plugin config (everything disabled).
func Defaults() Config {
	return Config{
		PluginImageVision:      {Enabled: false},
		PluginThinkDeeper:      {Enabled: false},
		PluginSpeedMode:        {Enabled: false},
		PluginJsonGuard:        {Enabled: false},
		PluginContextSqueezer:  {Enabled: false},
		PluginOpenaiToolBridge: {Enabled: false},
		PluginAntiSlop:         {Enabled: false, Level: "full"},
	}
}

// Enabled reports whether the named plugin is enabled in config.
func (c Config) Enabled(name string) bool {
	if c == nil {
		return false
	}
	return c[name].Enabled
}

// AnyEnabled reports whether any plugin at all is switched on, letting the
// request path skip the whole pipeline in the (default) all-off case.
func (c Config) AnyEnabled() bool {
	if c == nil {
		return false
	}
	for _, p := range c {
		if p.Enabled {
			return true
		}
	}
	return false
}

// Level returns the antiSlop level, defaulting to "full".
func (c Config) Level(name string) string {
	if c == nil {
		return "full"
	}
	lvl := c[name].Level
	if lvl == "" {
		return "full"
	}
	return lvl
}

// Models returns the allow-list for a plugin, nil when empty.
func (c Config) Models(name string) []string {
	if c == nil {
		return nil
	}
	return c[name].Models
}

// ParseConfig converts a persisted map into Config, tolerating missing fields
// and errant types (the blob is hand-editable through backup import).
func ParseConfig(raw map[string]any) Config {
	out := Defaults()
	for key, val := range raw {
		if _, known := internalConfig[key]; !known {
			continue
		}
		obj, ok := val.(map[string]any)
		if !ok {
			continue
		}
		p := out[key]
		if e, ok := obj["enabled"].(bool); ok {
			p.Enabled = e
		}
		if lvl, ok := obj["level"].(string); ok {
			p.Level = lvl
		}
		if models, ok := obj["models"].([]any); ok {
			for _, m := range models {
				if s, ok := m.(string); ok {
					p.Models = append(p.Models, s)
				}
			}
		}
		out[key] = p
	}
	return out
}

var internalConfig = map[string]struct{}{
	PluginImageVision:      {},
	PluginThinkDeeper:      {},
	PluginSpeedMode:        {},
	PluginJsonGuard:        {},
	PluginContextSqueezer:  {},
	PluginOpenaiToolBridge: {},
	PluginAntiSlop:         {},
}

// Apply runs every enabled plugin that matches the target model against body
// and returns the transformed bytes. Apply is safe on arbitrary JSON: each
// plugin opens the document lazily and returns nil if nothing changes, in which
// case original body bytes are returned without re-marshalling (so an
// unconfigured gateway pays zero cost on the hot path — the common case here
// is "all disabled", which must stay free).
func Apply(body []byte, provider, model, requestedModel string, cfg Config) []byte {
	if cfg == nil {
		return body
	}
	out := body
	changed := false
	if cfg.Enabled(PluginImageVision) && matchesAny(cfg.Models(PluginImageVision), model, requestedModel) {
		if nb := ApplyImageVision(out); nb != nil {
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginThinkDeeper) && matchesAny(cfg.Models(PluginThinkDeeper), model, requestedModel) {
		if nb := ApplyThinkDeeper(out); nb != nil {
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginSpeedMode) && matchesAny(cfg.Models(PluginSpeedMode), model, requestedModel) {
		if nb := ApplySpeedMode(out); nb != nil {
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginJsonGuard) && matchesAny(cfg.Models(PluginJsonGuard), model, requestedModel) {
		if nb := ApplyJsonGuardRequest(out); nb != nil {
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginContextSqueezer) && matchesAny(cfg.Models(PluginContextSqueezer), model, requestedModel) {
		var stats *Stats
		var nb []byte
		nb, stats = ApplyContextSqueezer(out, provider, model)
		if stats != nil {
			// keep stats for observability; body replace only when changed
			_ = stats
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginOpenaiToolBridge) && matchesAny(cfg.Models(PluginOpenaiToolBridge), model, requestedModel) {
		if nb := ApplyOpenaiToolBridge(out); nb != nil {
			out, changed = nb, true
		}
	}
	if cfg.Enabled(PluginAntiSlop) && matchesAny(cfg.Models(PluginAntiSlop), model, requestedModel) {
		if nb := ApplyAntiSlop(out, cfg.Level(PluginAntiSlop)); nb != nil {
			out, changed = nb, true
		}
	}
	if !changed {
		return body
	}
	return out
}

// matchesAny reports whether model or requestedModel appears in the allow-list.
// Matches bare names against provider/name and name forms, mirroring the
// X-Router modelKeysToTest semantics. An empty list matches everything.
func matchesAny(list []string, model, requestedModel string) bool {
	if len(list) == 0 {
		return true
	}
	keys := []string{requestedModel, model}
	for _, k := range keys {
		if k == "" {
			continue
		}
		lower := strings.ToLower(k)
		bare := strings.ToLower(lastSlash(lower))
		for _, m := range list {
			s := strings.ToLower(m)
			if s == lower || s == bare || lower == s || lastSlash(s) == bare {
				return true
			}
		}
	}
	return false
}

func lastSlash(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Stats reports a context-squeezer application so the gateway can log it.
type Stats struct {
	MessagesBefore int
	MessagesAfter  int
	Recap          string
}

// decode opens the body as a generic document. It returns nil when the body is
// not a JSON object (a request this pipeline cannot meaningfully transform).
func decode(body []byte) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("customplugins: decode body: %w", err)
	}
	return doc, nil
}

// encode re-serializes a document back to the request body. It is deterministic
// (fastjson sorts map keys) so upstream prompt caches stay stable.
func encode(doc map[string]any) ([]byte, error) {
	return json.Marshal(doc)
}

// jsonStringCoerce extracts a plain string from a message content that may be a
// string or a block array.
func jsonStringCoerce(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		for _, b := range t {
			if m, ok := b.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

// jsonStringCoerce is used by the context squeezer and anti-slop to read text
// from either a plain string or a typed block array.
