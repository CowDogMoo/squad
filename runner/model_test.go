package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/responses"
)

func TestNormalizeProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"OpenAI", " OpenAI ", "openai"},
		{"ollama with spaces", " ollama ", "ollama"},
		{"empty", "", ""},
		{"GEMINI with spaces", "  GEMINI  ", "gemini"},
		{"already lowercase", "openai", "openai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeProvider(tt.input); got != tt.want {
				t.Fatalf("normalizeProvider(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsOpenAICompatProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		want     bool
	}{
		{"empty", "", true},
		{"openai", "openai", true},
		{"ollama", "ollama", false},
		{"anthropic", "anthropic", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isOpenAICompatProvider(tt.provider); got != tt.want {
				t.Fatalf("isOpenAICompatProvider(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestBuildCallOpts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		opts        *RunOptions
		provider    string
		temperature float64
		maxTokens   int
		wantLen     int
	}{
		{"openai with temp only", &RunOptions{}, "openai", 0.3, 0, 1},
		{"ollama with temp and max", &RunOptions{}, "ollama", 0.3, 100, 2},
		{"openai legacy max tokens", &RunOptions{OpenAICompatMax: true}, "openai", -1, 200, 2},
		{"openai max completion tokens", &RunOptions{}, "openai", 0.0, 300, 2},
		{"negative temp skips temp opt", &RunOptions{}, "openai", -1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildCallOpts(tt.opts, tt.provider, tt.temperature, tt.maxTokens)
			if len(got) != tt.wantLen {
				t.Fatalf("buildCallOpts() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestBuildLLMVariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		model    string
		opts     *RunOptions
		wantType string
		wantErr  bool
	}{
		{"ollama", "ollama", "mistral", &RunOptions{}, "*ollama.LLM", false},
		{"openai", "openai", "gpt-4o", &RunOptions{APIKey: "token"}, "*openai.LLM", false},
		{"anthropic", "anthropic", "claude-3", &RunOptions{APIKey: "token"}, "*anthropic.LLM", false},
		{"unknown provider", "unknown", "model", &RunOptions{}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, err := buildLLM(tt.opts, tt.provider, tt.model)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildLLM() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if got := fmt.Sprintf("%T", model); got != tt.wantType {
					t.Fatalf("expected %s, got %s", tt.wantType, got)
				}
			}
		})
	}
}

func TestBuildOpenAICompatLLM(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{
		APIKey:     "token",
		BaseURL:    "https://example.com",
		Org:        "org",
		APIVersion: "2024-01-01",
		APIType:    "azure",
	}
	model, err := buildOpenAICompatLLM(opts, "openai", "gpt-4o")
	if err != nil || model == nil {
		t.Fatalf("buildOpenAICompatLLM() error = %v", err)
	}
}

func TestBuildAnthropicLLM(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{APIKey: "token", BaseURL: "https://example.com"}
	model, err := buildAnthropicLLM(opts, "claude-3")
	if err != nil || model == nil {
		t.Fatalf("buildAnthropicLLM() error = %v", err)
	}
}

func TestCallLangChainLLMWithOllama(t *testing.T) {
	t.Parallel()
	reqErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			reqErr <- fmt.Errorf("unexpected path: %s", r.URL.Path)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			reqErr <- fmt.Errorf("decode request: %w", err)
			return
		}
		reqErr <- nil
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"mistral",
			"message":{"role":"assistant","content":"hello"},
			"done":true,
			"prompt_eval_count":1,
			"eval_count":1
		}`))
	}))
	defer server.Close()

	opts := &RunOptions{Provider: "ollama", BaseURL: server.URL, MaxIterations: 1}
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	response, err := callLangChainLLM(
		context.Background(),
		opts,
		"ollama",
		"mistral",
		bundle.System,
		bundle,
		0.4,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("callLangChainLLM() error = %v", err)
	}
	if err := <-reqErr; err != nil {
		t.Fatalf("request error: %v", err)
	}
	if response != "hello" {
		t.Fatalf("response = %q, want hello", response)
	}
}

func TestCallResponsesAPIRoundTrip(t *testing.T) {
	maxTokensCh := make(chan int, 1)
	reqErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			reqErr <- fmt.Errorf("unexpected path: %s", r.URL.Path)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			reqErr <- fmt.Errorf("decode request: %w", err)
			return
		}
		if raw, ok := payload["max_output_tokens"]; ok {
			if val, ok := raw.(float64); ok {
				maxTokensCh <- int(val)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		reqErr <- json.NewEncoder(w).Encode(map[string]any{
			"id":                  "resp-1",
			"object":              "response",
			"created_at":          0,
			"model":               "gpt-5",
			"parallel_tool_calls": false,
			"temperature":         0,
			"tool_choice":         "auto",
			"tools":               []any{},
			"top_p":               1,
			"error": map[string]any{
				"code":    "server_error",
				"message": "",
			},
			"incomplete_details": map[string]any{"reason": ""},
			"instructions":       "system",
			"metadata":           map[string]any{},
			"output": []map[string]any{
				{
					"id":     "msg-1",
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []map[string]any{
						{"type": "output_text", "text": "hello"},
					},
				},
			},
		})
	}))
	defer server.Close()

	opts := &RunOptions{APIKey: "key", BaseURL: server.URL, MaxIterations: 1}
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	response, err := callResponsesAPI(
		context.Background(),
		opts,
		"gpt-5",
		"system",
		bundle,
		0.4,
		100,
		nil,
	)
	if err != nil {
		t.Fatalf("callResponsesAPI() error = %v", err)
	}
	if response != "hello" {
		t.Fatalf("response = %q, want hello", response)
	}
	if err := <-reqErr; err != nil {
		t.Fatalf("handler error: %v", err)
	}
	select {
	case got := <-maxTokensCh:
		if got != responses.DefaultMaxOutputTokens {
			t.Fatalf(
				"max_output_tokens = %d, want %d",
				got,
				responses.DefaultMaxOutputTokens,
			)
		}
	default:
		t.Fatalf("expected max_output_tokens in request")
	}
}

func TestBuildTaskConfigCallModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	agentsDir := t.TempDir()
	agentName := "child"
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := strings.Join([]string{
		"name: child",
		"version: '1.0'",
		"entrypoint: system.md",
		"wrapper: agent.md",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("system"), 0o644); err != nil {
		t.Fatalf("WriteFile system: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper: %v", err)
	}

	opts := &RunOptions{
		AgentsDir:     agentsDir,
		WorkingDir:    t.TempDir(),
		Provider:      "openai-responses",
		Model:         "gpt-5",
		MaxIterations: 1,
	}
	cfg := buildTaskConfig(opts)
	if cfg == nil {
		t.Fatalf("expected task config")
	}
	_, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallModelRoutes(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		setup    func(t *testing.T) *httptest.Server
		want     string
	}{
		{
			name:     "ollama",
			provider: "ollama",
			model:    "mistral",
			setup: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/api/chat" {
						t.Fatalf("unexpected path: %s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"ok"},"done":true}`))
				}))
			},
			want: "ok",
		},
		{
			name:     "responses api",
			provider: "openai-responses",
			model:    "gpt-4o",
			setup: func(t *testing.T) *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/responses" {
						t.Fatalf("unexpected path: %s", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"id":                  "resp-1",
						"object":              "response",
						"created_at":          0,
						"model":               "gpt-4o",
						"parallel_tool_calls": false,
						"temperature":         0,
						"tool_choice":         "auto",
						"tools":               []any{},
						"top_p":               1,
						"error":               map[string]any{"code": "server_error", "message": ""},
						"incomplete_details":  map[string]any{"reason": ""},
						"instructions":        "system",
						"metadata":            map[string]any{},
						"output": []map[string]any{
							{
								"id":     "msg-1",
								"type":   "message",
								"role":   "assistant",
								"status": "completed",
								"content": []map[string]any{
									{"type": "output_text", "text": "ok"},
								},
							},
						},
					})
				}))
			},
			want: "ok",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := tt.setup(t)
			defer server.Close()

			opts := &RunOptions{APIKey: "key", BaseURL: server.URL, MaxIterations: 1}
			if tt.provider == "ollama" {
				opts.Provider = "ollama"
				opts.APIKey = ""
			}

			bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
			got, err := callModel(
				context.Background(),
				opts,
				tt.provider,
				tt.model,
				bundle.System,
				bundle,
				0.4,
				0,
				nil,
			)
			if err != nil {
				t.Fatalf("callModel() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCallLangChainLLMUnknownProvider(t *testing.T) {
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	opts := &RunOptions{Provider: "unknown"}
	_, err := callLangChainLLM(
		context.Background(),
		opts,
		"unknown",
		"model",
		bundle.System,
		bundle,
		0.2,
		0,
		nil,
	)
	if err == nil {
		t.Fatalf("expected error")
	}
}
