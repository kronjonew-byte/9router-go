// Per-plugin request transformations. Each Apply* reads the body document, and
// returns nil when nothing changed (callers keep the original bytes).
package customplugins

import (
	"regexp"
	"strings"
)

// ApplyImageVision marks a request as vision-capable so the translator keeps
// raw image blocks intact. In the Go core the translator already routes on the
// request's own multimodal content, so this plugin is a light system nudge
// that tells capable models to describe attached images — honest pass-through
// rather than fake text extraction.
func ApplyImageVision(body []byte) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	msgs, ok := doc["messages"].([]any)
	if !ok {
		return nil
	}
	hasImages := false
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, b := range content {
			block, ok := b.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "image", "image_url":
				hasImages = true
			case "tool_result":
				if inner, ok := block["content"].([]any); ok {
					for _, ib := range inner {
						if ibm, ok := ib.(map[string]any); ok {
							if ibm["type"] == "image" || ibm["type"] == "image_url" {
								hasImages = true
							}
						}
					}
				}
			}
		}
	}
	if !hasImages {
		return nil
	}
	// system nudge
	if sys, ok := doc["system"].(string); ok {
		if !strings.Contains(sys, "Image Vision") {
			doc["system"] = "Image Vision is active: process all attached images and answer questions about their visual content.\n\n" + sys
		}
	} else {
		// inject a system message
		local := "Image Vision is active: process all attached images and answer questions about their visual content."
		found := false
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if msg["role"] == "system" {
				if s, ok := msg["content"].(string); ok {
					msg["content"] = local + "\n\n" + s
					found = true
					break
				}
			}
		}
		if !found {
			prepend := []any{map[string]any{"role": "system", "content": local}}
			doc["messages"] = append(prepend, msgs...)
		}
	}
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

// ApplyThinkDeeper sets native reasoning parameters so the provider routes to
// its deepest reasoning tier.
func ApplyThinkDeeper(body []byte) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	changed := false
	if claudeThinking(doc) {
		t, _ := doc["thinking"].(map[string]any)
		if t == nil {
			t = map[string]any{}
			doc["thinking"] = t
		}
		if t["type"] != "enabled" || t["budget_tokens"] == nil {
			t["type"] = "enabled"
			t["budget_tokens"] = 10240
			changed = true
		}
	} else {
		if v, _ := doc["reasoning_effort"].(string); v != "high" {
			doc["reasoning_effort"] = "high"
			changed = true
		}
		if _, ok := doc["reasoning"]; !ok {
			doc["reasoning"] = map[string]any{"effort": "high"}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

// ApplySpeedMode disables reasoning / thinking so the model answers directly.
func ApplySpeedMode(body []byte) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	changed := false
	if claudeThinking(doc) {
		if t, ok := doc["thinking"].(map[string]any); ok {
			_ = t
		}
		doc["thinking"] = map[string]any{"type": "disabled"}
		changed = true
	} else {
		if v, _ := doc["reasoning_effort"].(string); v != "none" {
			doc["reasoning_effort"] = "none"
			changed = true
		}
	}
	// strip any thinking leftovers (works on both dialects)
	if _, ok := doc["enable_thinking"]; ok {
		delete(doc, "enable_thinking")
		changed = true
	}
	if _, ok := doc["thinking_budget"]; ok {
		delete(doc, "thinking_budget")
		changed = true
	}
	if !changed {
		return nil
	}
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

// ApplyJsonGuardRequest fills response_format when the client only hinted at
// JSON in prose, so models that ignore hints still answer machine-readable.
func ApplyJsonGuardRequest(body []byte) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	if rf, ok := doc["response_format"].(map[string]any); ok {
		if rf["type"] == "json_object" || rf["type"] == "json_schema" {
			return nil
		}
	}
	haystack := ""
	if sys, ok := doc["system"].(string); ok {
		haystack += " " + sys
	}
	if msgs, ok := doc["messages"].([]any); ok {
		n := 0
		for _, m := range msgs {
			if n >= 4 {
				break
			}
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if msg["role"] == "system" || msg["role"] == "user" {
				haystack += " " + jsonStringCoerce(msg["content"])
				n++
			}
		}
	}
	if len(haystack) > 4000 {
		haystack = haystack[:4000]
	}
	if !wantsJSON(haystack) {
		return nil
	}
	if claudeThinking(doc) {
		// Claude has no response_format; use a system instruction.
		if sys, ok := doc["system"].(string); ok {
			doc["system"] = sys + "\n\nJSON Guard is active: answer with one valid JSON value and nothing else — no prose, no markdown fence."
		} else {
			doc["system"] = "JSON Guard is active: answer with one valid JSON value and nothing else — no prose, no markdown fence."
		}
	} else {
		doc["response_format"] = map[string]any{"type": "json_object"}
	}
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

var wantsJSONRe = regexp.MustCompile(`(?i)(^|\s)(respond|reply|answer|return|output|produce)\b[^.!?]{0,40}\bjson\b|json\s+only|only\s+(output|return)\s+json`)

func wantsJSON(s string) bool {
	return wantsJSONRe.MatchString(s)
}

// ApplyContextSqueezer trims a conversation that no longer fits the model's
// window, keeping the newest turns and dropping the oldest behind a recap.
func ApplyContextSqueezer(body []byte, provider, model string) ([]byte, *Stats) {
	doc, err := decode(body)
	if err != nil {
		return nil, nil
	}
	msgs, ok := doc["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return nil, nil
	}
	// Keep it simple and deterministic: if the serialized messages are larger
	// than the soft ceiling (approx 60k tokens worth), drop the oldest system
	// and user/assistant turns down to a bounded tail and prepend a recap.
	const target = 240_000 // bytes; rough upper bound
	payload, err := encode(map[string]any{"messages": msgs})
	if err != nil || len(payload) <= target {
		return nil, nil
	}
	if len(msgs) <= 4 {
		return nil, nil
	}
	tail := msgs[len(msgs)-8:]
	if len(tail) >= len(msgs) {
		tail = msgs[:4]
	}
	recap := map[string]any{"role": "system", "content": "Earlier conversation context was compacted."}
	out := append([]any{recap}, tail...)
	doc["messages"] = out
	enc, err := encode(doc)
	if err != nil {
		return nil, nil
	}
	return enc, &Stats{MessagesBefore: len(msgs), MessagesAfter: len(out)}
}

// ApplyOpenaiToolBridge re-writes OpenAI `functions` declarations into the
// `tools` form where a provider requires the tools schema.
func ApplyOpenaiToolBridge(body []byte) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	if _, ok := doc["tools"]; ok {
		// Already bridged; leave as-is.
		return nil
	}
	functions, ok := doc["functions"].([]any)
	if !ok || len(functions) == 0 {
		return nil
	}
	tools := make([]any, 0, len(functions))
	for _, f := range functions {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		tools = append(tools, map[string]any{
			"type":     "function",
			"function": fm,
		})
	}
	doc["tools"] = tools
	delete(doc, "functions")
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

// ApplyAntiSlop strips repetitive AI filler phrases from outgoing text.
// Level "lite" removes the most egregious filler; "full" is more aggressive.
func ApplyAntiSlop(body []byte, level string) []byte {
	doc, err := decode(body)
	if err != nil {
		return nil
	}
	repl := antiSlopReplacements(level)
	changed := false
	if sys, ok := doc["system"].(string); ok {
		if ns := applyReplacements(sys, repl); ns != sys {
			doc["system"] = ns
			changed = true
		}
	}
	if msgs, ok := doc["messages"].([]any); ok {
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := msg["content"].(string); ok {
				if ns := applyReplacements(s, repl); ns != s {
					msg["content"] = ns
					changed = true
				}
			} else if blocks, ok := msg["content"].([]any); ok {
				for _, b := range blocks {
					bm, ok := b.(map[string]any)
					if !ok {
						continue
					}
					if s, ok := bm["text"].(string); ok {
						if ns := applyReplacements(s, repl); ns != s {
							bm["text"] = ns
							changed = true
						}
					}
				}
			}
		}
	}
	if !changed {
		return nil
	}
	out, err := encode(doc)
	if err != nil {
		return nil
	}
	return out
}

type replacement struct {
	re   *regexp.Regexp
	with string
}

var antiSlopFull = []replacement{
	{regexp.MustCompile(`(?i)\bsure(,|!) I'd be happy to\b`), "Sure"},
	{regexp.MustCompile(`(?i)\bI'd be happy to (help|assist)\b`), "I'll"},
	{regexp.MustCompile(`(?i)\bas an AI (language model|assistant)\b`), "As an AI"},
	{regexp.MustCompile(`(?i)\bIn conclusion\b,?`), ""},
	{regexp.MustCompile(`(?i)\bOverall\b,?\s+`), ""},
}

var antiSlopLite = []replacement{
	{regexp.MustCompile(`(?i)\bI'd be happy to help\b`), "I can help"},
	{regexp.MustCompile(`(?i)\bas an AI language model\b`), "As an AI"},
}

func antiSlopReplacements(level string) []replacement {
	if level == "lite" {
		return antiSlopLite
	}
	return antiSlopFull
}

func applyReplacements(s string, repl []replacement) string {
	out := s
	for _, r := range repl {
		out = r.re.ReplaceAllString(out, r.with)
	}
	return out
}

// claudeThinking reports whether the body targets the Claude Messages wire
// (which routes thinking through the top-level `thinking` object).
func claudeThinking(doc map[string]any) bool {
	if _, ok := doc["thinking"]; ok {
		return true
	}
	if _, ok := doc["anthropic_version"]; ok {
		return true
	}
	return false
}
