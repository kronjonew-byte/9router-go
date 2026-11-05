package customplugins

import (
	"strings"
	"testing"

	json "9router/proxy/internal/fastjson"
)

func mustDecode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return doc
}

func TestApplyDisabledReturnsSameBytes(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", Defaults())
	if string(out) != string(body) {
		t.Fatalf("disabled config must not change bytes: %s", out)
	}
}

func TestThinkDeeperSetsReasoningEffort(t *testing.T) {
	cfg := Defaults()
	p := cfg["thinkDeeper"]
	p.Enabled = true
	cfg["thinkDeeper"] = p

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"math"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	doc := mustDecode(t, out)
	if doc["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort=high, got %v", doc["reasoning_effort"])
	}
}

func TestSpeedModeDisablesThinking(t *testing.T) {
	cfg := Defaults()
	p := cfg["speedMode"]
	p.Enabled = true
	cfg["speedMode"] = p

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"quick"}],"enable_thinking":true}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	doc := mustDecode(t, out)
	if doc["reasoning_effort"] != "none" {
		t.Fatalf("expected reasoning_effort=none, got %v", doc["reasoning_effort"])
	}
	if _, ok := doc["enable_thinking"]; ok {
		t.Fatalf("enable_thinking should be stripped")
	}
}

func TestJsonGuardAddsResponseFormatOnJSONHint(t *testing.T) {
	cfg := Defaults()
	p := cfg["jsonGuard"]
	p.Enabled = true
	cfg["jsonGuard"] = p

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Return JSON only with the fields"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	doc := mustDecode(t, out)
	rf, ok := doc["response_format"].(map[string]any)
	if !ok || rf["type"] != "json_object" {
		t.Fatalf("expected response_format json_object, got %v", doc["response_format"])
	}
}

func TestJsonGuardSkipsWhenAlreadyAsked(t *testing.T) {
	cfg := Defaults()
	p := cfg["jsonGuard"]
	p.Enabled = true
	cfg["jsonGuard"] = p

	body := []byte(`{"model":"gpt-4o","response_format":{"type":"json_schema"},"messages":[{"role":"user","content":"Return JSON"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	if string(out) != string(body) {
		t.Fatalf("must not touch body when response_format is set: %s", out)
	}
}

func TestOpenaiToolBridgeConvertsFunctionsToTools(t *testing.T) {
	cfg := Defaults()
	p := cfg["openaiToolBridge"]
	p.Enabled = true
	cfg["openaiToolBridge"] = p

	body := []byte(`{"model":"gpt-4o","functions":[{"name":"get_weather","parameters":{"type":"object"}}],"messages":[{"role":"user","content":"weather"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	doc := mustDecode(t, out)
	tools, ok := doc["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", doc["tools"])
	}
	if _, ok := doc["functions"]; ok {
		t.Fatalf("functions must be removed after bridging")
	}
}

func TestAntiSlopFullStripsFiller(t *testing.T) {
	cfg := Defaults()
	p := cfg["antiSlop"]
	p.Enabled = true
	p.Level = "full"
	cfg["antiSlop"] = p

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"assistant","content":"Sure! I'd be happy to help with that. In conclusion, here is the answer."}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	doc := mustDecode(t, out)
	content := doc["messages"].([]any)[0].(map[string]any)["content"].(string)
	if strings.Contains(content, "I'd be happy to") {
		t.Fatalf("filler not stripped: %s", content)
	}
	if strings.Contains(content, "In conclusion") {
		t.Fatalf("conclusion filler not stripped: %s", content)
	}
}

func TestModelAllowListRestrictsApplication(t *testing.T) {
	cfg := Defaults()
	p := cfg["thinkDeeper"]
	p.Enabled = true
	p.Models = []string{"claude-sonnet"}
	cfg["thinkDeeper"] = p

	// requested gpt-4o should NOT match
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"x"}]}`)
	out := Apply(body, "openai", "gpt-4o", "gpt-4o", cfg)
	if string(out) != string(body) {
		t.Fatalf("gpt-4o must not match claude-sonnet list")
	}

	// requested claude-sonnet SHOULD match (bare name)
	body2 := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":"x"}]}`)
	out2 := Apply(body2, "openai", "claude-sonnet", "claude-sonnet", cfg)
	if string(out2) == string(body2) {
		t.Fatalf("claude-sonnet should have been transformed")
	}
}

func TestParseConfigToleratesGarbage(t *testing.T) {
	raw := map[string]any{
		"thinkDeeper": map[string]any{"enabled": true, "models": []any{"claude-sonnet"}},
		"antiSlop":    map[string]any{"enabled": true, "level": "lite"},
		"bogus":       "not-a-plugin",
	}
	cfg := ParseConfig(raw)
	if !cfg.Enabled("thinkDeeper") {
		t.Fatalf("thinkDeeper should be enabled")
	}
	if cfg.Level("antiSlop") != "lite" {
		t.Fatalf("antiSlop level should be lite")
	}
	if cfg.Enabled("bogus") {
		t.Fatalf("unknown plugin must stay disabled")
	}
}
