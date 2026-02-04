package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cowdogmoo/squad/agent"
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
