package runner

import (
	"fmt"
	"testing"
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
		wantType string
		wantErr  bool
	}{
		{"ollama", "ollama", "mistral", "*ollama.LLM", false},
		{"unknown provider", "unknown", "model", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model, err := buildLLM(&RunOptions{}, tt.provider, tt.model)
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
