package executor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ForwardDevinACP handles requests for devin-cli by invoking `devin -p` locally.
func ForwardDevinACP(w http.ResponseWriter, req *Request) error {
	// Parse OpenAI chat completion request body
	var chatReq struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}

	if len(req.Body) > 0 {
		_ = json.Unmarshal(req.Body, &chatReq)
	}

	// Extract last user message or default prompt
	prompt := "Hello"
	for i := len(chatReq.Messages) - 1; i >= 0; i-- {
		if chatReq.Messages[i].Role == "user" && chatReq.Messages[i].Content != "" {
			prompt = chatReq.Messages[i].Content
			break
		}
	}

	// Run devin -p
	cmd := exec.CommandContext(req.Ctx, "devin", "-p", prompt)
	outBytes, err := cmd.CombinedOutput()
	output := string(outBytes)
	if err != nil {
		output = fmt.Sprintf("devin execution error: %v\nOutput: %s", err, output)
	}
	output = strings.TrimSpace(output)

	created := time.Now().Unix()
	respID := fmt.Sprintf("devin-%d", created)
	modelName := req.ModelName
	if modelName == "" {
		modelName = "gpt-5.4-high"
	}

	if req.IsStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		// Send initial chunk
		chunk1 := map[string]any{
			"id":      respID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []map[string]any{
				{
					"index": 0,
					"delta": map[string]any{
						"role":    "assistant",
						"content": output,
					},
					"finish_reason": nil,
				},
			},
		}
		b1, _ := json.Marshal(chunk1)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b1))

		// Send finish chunk
		chunk2 := map[string]any{
			"id":      respID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelName,
			"choices": []map[string]any{
				{
					"index":         0,
					"delta":         map[string]any{},
					"finish_reason": "stop",
				},
			},
		}
		b2, _ := json.Marshal(chunk2)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b2))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		return nil
	}

	// Non-streaming response
	resp := map[string]any{
		"id":      respID,
		"object":  "chat.completion",
		"created": created,
		"model":   modelName,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": output,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": len(output) / 4,
			"total_tokens":      10 + len(output)/4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(resp)
}
