package executor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

	// Run devin -p. Print mode is non-interactive: without an explicit
	// permission mode the CLI defaults to "auto" and rejects any tool call
	// that needs confirmation, printing "rejected a tool call that requires
	// confirmation" into the response body. Default to "dangerous" (gateway
	// use is unattended) and allow an env override.
	permissionMode := strings.TrimSpace(os.Getenv("DEVIN_CLI_PERMISSION_MODE"))
	if permissionMode == "" {
		permissionMode = "dangerous"
	}
	cmd := exec.CommandContext(req.Ctx, "devin", "-p", prompt, "--permission-mode", permissionMode)
	// Devin refuses to run in an untrusted workspace (its default cwd is the
	// gateway binary's directory), so pin the child to a trusted directory.
	// Operators can point DEVIN_CLI_WORKSPACE at the directory they trusted
	// interactively; otherwise use $HOME/workspace when it exists.
	cmd.Dir = devinWorkspaceDir()
	outBytes, err := cmd.CombinedOutput()
	output := string(outBytes)
	if err != nil {
		output = fmt.Sprintf("devin execution error: %v\nOutput: %s", err, output)
	}
	output = strings.TrimSpace(output)

	created := time.Now().Unix()
	respID := fmt.Sprintf("devin-%d", created)
	// Label the reply with the model the client asked for. The chat handler
	// does not populate req.ModelName on this path, so read the body; fall
	// back to the executor field and only then to the historical default.
	modelName := strings.TrimSpace(chatReq.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(req.ModelName)
	}
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

// devinWorkspaceDir returns the directory the devin CLI child should run in.
// Devin only runs non-interactively in a workspace it has been told to trust;
// the gateway's own cwd (/usr/local/bin) is not one. Empty means "inherit",
// which preserves the previous behavior when no trusted dir is available.
func devinWorkspaceDir() string {
	if dir := strings.TrimSpace(os.Getenv("DEVIN_CLI_WORKSPACE")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "workspace")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir
	}
	return ""
}
