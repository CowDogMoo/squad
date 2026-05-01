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
	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/responses"
	"github.com/cowdogmoo/squad/tools"
	"github.com/tmc/langchaingo/llms"
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
		{"nvidia", "nvidia", true},
		{"databricks", "databricks", true},
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
		{"anthropic with temp and max", &RunOptions{}, "anthropic", 0.5, 2048, 3},
		{"anthropic no max tokens", &RunOptions{}, "anthropic", 0.5, 0, 2},
		{"streaming adds func", &RunOptions{Stream: true}, "openai", 0.3, 0, 2},
		{"streaming with anthropic", &RunOptions{Stream: true}, "anthropic", 0.5, 2048, 4},
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
		{"gemini", "gemini", "gemini-2.0-flash", &RunOptions{APIKey: "token"}, "*googleai.GoogleAI", false},
		{"nvidia provider", "nvidia", "meta/llama-3.1-8b-instruct", &RunOptions{APIKey: "nvapi-test-key"}, "*openai.LLM", false},
		{"unknown provider", "unknown", "model", &RunOptions{}, "", true},
		{
			"databricks provider",
			"databricks",
			"databricks-gpt-5-5-pro",
			&RunOptions{
				APIKey:  "dapi-test-token",
				BaseURL: "https://7474657828785521.ai-gateway.cloud.databricks.com/mlflow/v1",
			},
			"*openai.LLM",
			false,
		},
		{
			"databricks provider missing base URL",
			"databricks",
			"databricks-gpt-5-5-pro",
			&RunOptions{APIKey: "dapi-test-token"},
			"",
			true,
		},
		{
			"databricks provider missing api key",
			"databricks",
			"databricks-gpt-5-5-pro",
			&RunOptions{BaseURL: "https://7474657828785521.ai-gateway.cloud.databricks.com/mlflow/v1"},
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, err := buildLLM(context.Background(), tt.opts, tt.provider, tt.model)
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

func TestBuildGeminiLLM(t *testing.T) {
	t.Parallel()
	opts := &RunOptions{APIKey: "test-key"}
	model, err := buildGeminiLLM(context.Background(), opts, "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("buildGeminiLLM() error = %v", err)
	}
	if model == nil {
		t.Fatal("expected non-nil model")
	}
}

func TestBuildNativeOllamaLLM(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts *RunOptions
	}{
		{"defaults", &RunOptions{}},
		{"custom", &RunOptions{BaseURL: "http://custom:11434", NumCtx: 65536}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm := buildNativeOllamaLLM(tt.opts, "llama3")
			if llm == nil {
				t.Fatal("buildNativeOllamaLLM returned nil")
			}
			if got := fmt.Sprintf("%T", llm); got != "*ollama.LLM" {
				t.Fatalf("type = %s, want *ollama.LLM", got)
			}
		})
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
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}
	m := metrics.New("ollama", "mistral")
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
		ex,
		m,
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
	ex2 := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}
	m := metrics.New("openai", "gpt-5")
	response, err := callResponsesAPI(
		context.Background(),
		opts,
		"gpt-5",
		"system",
		bundle,
		0.4,
		100,
		nil,
		ex2,
		m,
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
	_, _, err := cfg.CallModel(
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
			ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}
			m := metrics.New(tt.provider, tt.model)
			got, _, err := callModel(
				context.Background(),
				opts,
				tt.provider,
				tt.model,
				bundle.System,
				bundle,
				0.4,
				0,
				nil,
				ex,
				m,
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

func TestInvokeModel_ExecutorError(t *testing.T) {
	t.Parallel()
	bundle := &agent.Bundle{
		System:  "system",
		User:    "user",
		WorkDir: t.TempDir(),
		Environment: &executor.Config{
			Type:    "quantum", // unknown executor type
			Options: map[string]string{},
		},
	}
	opts := &RunOptions{Provider: "openai", Model: "gpt-4o", APIKey: "key"}

	_, _, err := InvokeModel(context.Background(), opts, bundle)
	if err == nil {
		t.Fatal("expected error from unknown executor type")
	}
	if !strings.Contains(err.Error(), "failed to create executor") {
		t.Fatalf("error = %q, want 'failed to create executor'", err)
	}
}

func TestInvokeModel_SystemOverride(t *testing.T) {
	t.Parallel()
	// This tests the system override path in InvokeModel.
	// It will fail on API call but exercises the system override code path.
	bundle := &agent.Bundle{
		System:  "base system",
		User:    "user",
		WorkDir: t.TempDir(),
	}
	opts := &RunOptions{
		Provider: "openai-responses",
		Model:    "gpt-4o",
		System:   "extra instructions",
		// No API key → will fail at API call
	}

	_, _, err := InvokeModel(context.Background(), opts, bundle)
	if err == nil {
		t.Fatal("expected error (no API key)")
	}
	// The error should be about API key, not about system override
	if !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("error = %q, want API key error", err)
	}
}

func TestCallLangChainLLMUnknownProvider(t *testing.T) {
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	opts := &RunOptions{Provider: "unknown"}
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}
	m := metrics.New("unknown", "model")
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
		ex,
		m,
	)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestReasoningPrefixes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts *RunOptions
		want []string
	}{
		{
			"nil config uses defaults",
			&RunOptions{Config: nil},
			config.Defaults().Model.ReasoningPrefixes,
		},
		{
			"empty prefixes uses defaults",
			&RunOptions{Config: &config.Config{}},
			config.Defaults().Model.ReasoningPrefixes,
		},
		{
			"custom prefixes",
			&RunOptions{Config: &config.Config{
				Model: config.ModelConfig{
					ReasoningPrefixes: []string{"o1", "o3"},
				},
			}},
			[]string{"o1", "o3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := reasoningPrefixes(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("reasoningPrefixes() len = %d, want %d", len(got), len(tt.want))
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Fatalf("reasoningPrefixes()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestInferMaxTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		maxTokens   int
		hasTaskTool bool
		want        int
	}{
		{"explicit override above floor with task tool", 32768, true, 32768},
		{"explicit override below floor with task tool", 1024, true, DefaultMaxTokensWithTask},
		{"config default with task tool", 1024, true, DefaultMaxTokensWithTask},
		{"explicit override without task tool", 2048, false, 2048},
		{"no override with task tool", 0, true, DefaultMaxTokensWithTask},
		{"no override without task tool", 0, false, DefaultMaxTokensLeaf},
		{"negative with task tool", -1, true, DefaultMaxTokensWithTask},
		{"negative without task tool", -1, false, DefaultMaxTokensLeaf},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := inferMaxTokens(tt.maxTokens, tt.hasTaskTool)
			if got != tt.want {
				t.Fatalf("inferMaxTokens(%d, %v) = %d, want %d", tt.maxTokens, tt.hasTaskTool, got, tt.want)
			}
		})
	}
}

func TestBuildTaskConfigBudgetExhausted(t *testing.T) {
	t.Parallel()
	agentsDir := t.TempDir()
	agentName := "child"
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := "name: child\nversion: '1.0'\nentrypoint: system.md\nwrapper: agent.md\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("system"), 0o644); err != nil {
		t.Fatalf("WriteFile system: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper: %v", err)
	}

	// Set up parent metrics with exhausted budget.
	parentMetrics := metrics.New("openai", "gpt-4o")
	parentMetrics.SetMaxCost(0.0001)
	parentMetrics.AddTokens(10_000_000, 10_000_000) // huge cost

	opts := &RunOptions{
		AgentsDir:     agentsDir,
		WorkingDir:    t.TempDir(),
		Provider:      "openai-responses",
		Model:         "gpt-4o",
		MaxIterations: 1,
		MaxCost:       1.00,
	}
	cfg := buildTaskConfig(opts)
	cfg.ParentMetrics = parentMetrics

	_, _, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatal("expected error when budget is exhausted")
	}
	if !strings.Contains(err.Error(), "budget exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCallResponsesAPIMissingKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	opts := &RunOptions{APIKey: "", MaxIterations: 1}
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}

	m := metrics.New("openai", "gpt-4o")
	_, err := callResponsesAPI(
		context.Background(),
		opts,
		"gpt-4o",
		"system",
		bundle,
		0.4,
		100,
		nil,
		ex,
		m,
	)
	if err == nil || !strings.Contains(err.Error(), "API key required") {
		t.Fatalf("expected API key error, got: %v", err)
	}
}

func TestCallResponsesAPIOpenAIResponsesProvider(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	// Use openai-responses provider explicitly.
	opts := &RunOptions{
		APIKey:        "key",
		BaseURL:       server.URL,
		Provider:      "openai-responses",
		MaxIterations: 1,
	}
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}

	m := metrics.New("openai-responses", "gpt-4o")
	response, err := callResponsesAPI(
		context.Background(),
		opts,
		"gpt-4o",
		"system",
		bundle,
		0.4,
		4096,
		nil,
		ex,
		m,
	)
	if err != nil {
		t.Fatalf("callResponsesAPI() error = %v", err)
	}
	if response != "ok" {
		t.Fatalf("response = %q, want ok", response)
	}
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
}

func TestCallLangChainLLMWithTaskConfig(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"partial"},"done":true}`))
	}))
	defer server.Close()

	opts := &RunOptions{Provider: "ollama", BaseURL: server.URL, MaxIterations: 1, MaxCost: 1.00}
	bundle := &agent.Bundle{System: "system", User: "user", WorkDir: t.TempDir()}
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}

	// Pass a non-nil TaskConfig to exercise the ParentMetrics assignment path.
	taskCfg := &tools.TaskConfig{}
	m := metrics.New("ollama", "mistral")
	response, err := callLangChainLLM(
		context.Background(),
		opts,
		"ollama",
		"mistral",
		bundle.System,
		bundle,
		0.4,
		0,
		taskCfg,
		ex,
		m,
	)
	if err != nil {
		t.Fatalf("callLangChainLLM() error = %v", err)
	}
	if response != "partial" {
		t.Fatalf("response = %q, want partial", response)
	}
	// The function should have set ParentMetrics on the taskCfg.
	if taskCfg.ParentMetrics == nil {
		t.Fatal("taskCfg.ParentMetrics should be set by callLangChainLLM")
	}
}

func TestConvertMCPHandlers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		handlers []mcp.ToolHandler
		wantLen  int
	}{
		{
			name:     "nil input",
			handlers: nil,
			wantLen:  0,
		},
		{
			name: "single handler",
			handlers: []mcp.ToolHandler{
				{
					Def: llms.Tool{
						Type: "function",
						Function: &llms.FunctionDefinition{
							Name:        "mcp__test__tool1",
							Description: "Test tool",
						},
					},
					Call: func(_ context.Context, _ []byte) (string, error) {
						return "ok", nil
					},
				},
			},
			wantLen: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := convertMCPHandlers(tt.handlers)
			if len(result) != tt.wantLen {
				t.Fatalf("expected %d handlers, got %d", tt.wantLen, len(result))
			}
			if tt.wantLen > 0 {
				if result[0].Def.Function.Name != "mcp__test__tool1" {
					t.Errorf("name = %q, want mcp__test__tool1", result[0].Def.Function.Name)
				}
				out, err := result[0].Call(context.Background(), nil)
				if err != nil || out != "ok" {
					t.Fatalf("Call() = (%q, %v), want (ok, nil)", out, err)
				}
			}
		})
	}
}

func TestCloseMCPClients(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		clients []*mcp.Client
	}{
		{"nil slice", nil},
		{"empty slice", []*mcp.Client{}},
		{
			"with clients",
			[]*mcp.Client{
				mcp.NewTestClient("a", nil),
				mcp.NewTestClient("b", nil),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Should not panic.
			closeMCPClients(tt.clients)
		})
	}
}

func TestDisableTaskNilsTaskConfig(t *testing.T) {
	t.Parallel()

	// Set up a fake Ollama server that returns a simple response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"mistral","message":{"role":"assistant","content":"done"},"done":true}`))
	}))
	defer server.Close()

	opts := &RunOptions{Provider: "ollama", BaseURL: server.URL, MaxIterations: 1}
	bundle := &agent.Bundle{
		System:      "system",
		User:        "user",
		WorkDir:     t.TempDir(),
		DisableTask: true,
	}
	ex := &executor.LocalExecutor{WorkingDir: bundle.WorkDir}
	m := metrics.New("ollama", "mistral")

	// When DisableTask is true, model.go nils CallModel and Registry but keeps
	// taskCfg alive so MCP ExtraTools can still be attached. tools.go then
	// checks CallModel != nil before registering Task/TaskResult.

	taskCfg := buildTaskConfig(opts)
	if bundle.DisableTask {
		taskCfg.CallModel = nil
		taskCfg.Registry = nil
	}

	response, err := callLangChainLLM(
		context.Background(),
		opts,
		"ollama",
		"mistral",
		bundle.System,
		bundle,
		0.4,
		0,
		taskCfg,
		ex,
		m,
	)
	if err != nil {
		t.Fatalf("callLangChainLLM() error = %v", err)
	}
	if response != "done" {
		t.Fatalf("response = %q, want done", response)
	}
	// taskCfg is non-nil but CallModel should still be nil (Task tool not registered).
	if taskCfg.CallModel != nil {
		t.Fatal("expected taskCfg.CallModel to be nil when DisableTask is true")
	}
}

func TestApplyBundleBudget(t *testing.T) {
	t.Parallel()

	t.Run("sets ChildMaxCost from budget", func(t *testing.T) {
		t.Parallel()
		cfg := &tools.TaskConfig{}
		bundle := &agent.Bundle{
			Budget: &agent.BudgetConfig{
				Children: []agent.ChildBudget{
					{Name: "agent-a", MaxCost: 5.0},
					{Name: "agent-b"},
				},
			},
		}
		applyBundleBudget(cfg, bundle)
		if cfg.ChildMaxCost == nil {
			t.Fatal("expected ChildMaxCost to be set")
		}
		if got := cfg.ChildMaxCost("agent-a"); got != 5.0 {
			t.Fatalf("ChildMaxCost(agent-a) = %f, want 5.0", got)
		}
		if got := cfg.ChildMaxCost("agent-b"); got != 0 {
			t.Fatalf("ChildMaxCost(agent-b) = %f, want 0", got)
		}
	})

	t.Run("nil budget leaves ChildMaxCost nil", func(t *testing.T) {
		t.Parallel()
		cfg := &tools.TaskConfig{}
		bundle := &agent.Bundle{}
		applyBundleBudget(cfg, bundle)
		if cfg.ChildMaxCost != nil {
			t.Fatal("expected ChildMaxCost to remain nil")
		}
	})

	t.Run("empty children leaves ChildMaxCost nil", func(t *testing.T) {
		t.Parallel()
		cfg := &tools.TaskConfig{}
		bundle := &agent.Bundle{
			Budget: &agent.BudgetConfig{},
		}
		applyBundleBudget(cfg, bundle)
		if cfg.ChildMaxCost != nil {
			t.Fatal("expected ChildMaxCost to remain nil for empty children")
		}
	})
}

func TestBuildTaskConfigChildModelOverride(t *testing.T) {
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
		"model: claude-haiku-4-5",
		"provider: anthropic",
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
		Model:         "gpt-4o",
		MaxIterations: 1,
	}
	cfg := buildTaskConfig(opts)

	_, _, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "API key") && !strings.Contains(err.Error(), "failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildTaskConfigChildBudgetDedicated(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	agentsDir := t.TempDir()
	agentName := "child"
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := "name: child\nversion: '1.0'\nentrypoint: system.md\nwrapper: agent.md\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("system"), 0o644); err != nil {
		t.Fatalf("WriteFile system: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper: %v", err)
	}

	parentMetrics := metrics.New("openai", "gpt-4o")
	parentMetrics.SetMaxCost(10.0)

	opts := &RunOptions{
		AgentsDir:     agentsDir,
		WorkingDir:    t.TempDir(),
		Provider:      "openai-responses",
		Model:         "gpt-4o",
		MaxIterations: 1,
		MaxCost:       10.0,
	}
	cfg := buildTaskConfig(opts)
	cfg.ParentMetrics = parentMetrics
	cfg.ChildMaxCost = func(name string) float64 {
		if name == "child" {
			return 2.50
		}
		return 0
	}

	_, _, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatal("expected error (no API key)")
	}
}

func TestBuildTaskConfigChildBudgetCappedByRemaining(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")

	agentsDir := t.TempDir()
	agentName := "child"
	agentDir := filepath.Join(agentsDir, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	manifest := "name: child\nversion: '1.0'\nentrypoint: system.md\nwrapper: agent.md\n"
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.md"), []byte("system"), 0o644); err != nil {
		t.Fatalf("WriteFile system: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.md"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("WriteFile wrapper: %v", err)
	}

	parentMetrics := metrics.New("openai", "gpt-4o")
	parentMetrics.SetMaxCost(1.0)

	opts := &RunOptions{
		AgentsDir:     agentsDir,
		WorkingDir:    t.TempDir(),
		Provider:      "openai-responses",
		Model:         "gpt-4o",
		MaxIterations: 1,
		MaxCost:       1.0,
	}
	cfg := buildTaskConfig(opts)
	cfg.ParentMetrics = parentMetrics
	cfg.ChildMaxCost = func(name string) float64 {
		return 5.0
	}

	_, _, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatal("expected error (no API key)")
	}
}

func TestBuildTaskConfigChildAlternateModels(t *testing.T) {
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
		"models:",
		"  - model: gemini-2.5-flash",
		"    provider: gemini",
		"  - model: gpt-4.1-mini",
		"    provider: openai",
		"  - model: claude-haiku-4-5",
		"    provider: anthropic",
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
		Model:         "gpt-4o",
		MaxIterations: 1,
	}
	cfg := buildTaskConfig(opts)

	_, _, err := cfg.CallModel(
		context.Background(),
		agentsDir,
		agentName,
		"prompt",
		opts.WorkingDir,
		"",
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestConnectMCPServers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		servers []mcp.ServerConfig
		wantErr bool
	}{
		{
			name:    "empty",
			servers: nil,
			wantErr: false,
		},
		{
			name:    "invalid command",
			servers: []mcp.ServerConfig{{Name: "bad", Command: "/nonexistent/binary/xyz"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clients, err := connectMCPServers(context.Background(), tt.servers)
			if (err != nil) != tt.wantErr {
				t.Fatalf("connectMCPServers() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(clients) != 0 {
				t.Fatalf("expected 0 clients, got %d", len(clients))
			}
			closeMCPClients(clients)
		})
	}
}
