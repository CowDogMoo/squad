package main

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
)

func TestUseResponsesAPI(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		want     bool
	}{
		{"openai", "gpt-5.2", true},
		{"openai", "gpt-5", true},
		{"openai", "gpt-5.1-mini", true},
		{"openai", "gpt-4o", false},
		{"openai", "gpt-4.1", false},
		{"openai", "o3", false},
		{"openai-responses", "gpt-4o", true},
		{"openai-responses", "o3", true},
		{"OpenAI-Responses", "anything", true},
		{"anthropic", "claude-sonnet-4-20250514", false},
		{"ollama", "llama3", false},
		{"", "gpt-5.2", false},
	}
	for _, tt := range tests {
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			got := useResponsesAPI(tt.provider, tt.model)
			if got != tt.want {
				t.Errorf("useResponsesAPI(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestConvertToolsToResponses(t *testing.T) {
	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "Read",
				Description: "Read a file.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string"},
					},
					"required": []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "Bash",
				Description: "Run a command.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
					"required": []string{"command"},
				},
			},
		},
		{Type: "function", Function: nil}, // should be skipped
	}

	result := convertToolsToResponses(tools)
	if len(result) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result))
	}

	// Verify first tool.
	if result[0].OfFunction == nil {
		t.Fatal("expected OfFunction to be set")
	}
	if result[0].OfFunction.Name != "Read" {
		t.Errorf("expected name Read, got %s", result[0].OfFunction.Name)
	}
	params, ok := result[0].OfFunction.Parameters["type"]
	if !ok || params != "object" {
		t.Error("expected parameters.type = object")
	}
}

func TestExtractFunctionCalls(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		calls := extractFunctionCalls(nil)
		if len(calls) != 0 {
			t.Errorf("expected 0 calls, got %d", len(calls))
		}
	})

	t.Run("no function calls", func(t *testing.T) {
		resp := &responses.Response{
			Output: []responses.ResponseOutputItemUnion{
				{Type: "message", ID: "msg_1"},
			},
		}
		calls := extractFunctionCalls(resp)
		if len(calls) != 0 {
			t.Errorf("expected 0 calls, got %d", len(calls))
		}
	})

	t.Run("with function calls", func(t *testing.T) {
		resp := &responses.Response{
			Output: []responses.ResponseOutputItemUnion{
				{
					Type:      "function_call",
					ID:        "fc_1",
					CallID:    "call_abc",
					Name:      "Read",
					Arguments: `{"path":"main.go"}`,
				},
				{Type: "message", ID: "msg_1"},
				{
					Type:      "function_call",
					ID:        "fc_2",
					CallID:    "call_def",
					Name:      "Bash",
					Arguments: `{"command":"go build"}`,
				},
			},
		}
		calls := extractFunctionCalls(resp)
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
		if calls[0].Name != "Read" || calls[0].CallID != "call_abc" {
			t.Errorf("unexpected first call: %+v", calls[0])
		}
		if calls[1].Name != "Bash" || calls[1].CallID != "call_def" {
			t.Errorf("unexpected second call: %+v", calls[1])
		}
	})
}

func TestResolveResponsesAPIKey(t *testing.T) {
	// Save and restore globals.
	origKey := runAPIKey
	t.Cleanup(func() { runAPIKey = origKey })

	t.Run("from flag", func(t *testing.T) {
		runAPIKey = "flag-key"
		key, err := resolveResponsesAPIKey("")
		if err != nil {
			t.Fatal(err)
		}
		if key != "flag-key" {
			t.Errorf("expected flag-key, got %s", key)
		}
	})

	t.Run("from config", func(t *testing.T) {
		runAPIKey = ""
		key, err := resolveResponsesAPIKey("config-key")
		if err != nil {
			t.Fatal(err)
		}
		if key != "config-key" {
			t.Errorf("expected config-key, got %s", key)
		}
	})

	t.Run("from env", func(t *testing.T) {
		runAPIKey = ""
		t.Setenv("OPENAI_API_KEY", "env-key")
		key, err := resolveResponsesAPIKey("")
		if err != nil {
			t.Fatal(err)
		}
		if key != "env-key" {
			t.Errorf("expected env-key, got %s", key)
		}
	})

	t.Run("missing", func(t *testing.T) {
		runAPIKey = ""
		t.Setenv("OPENAI_API_KEY", "")
		_, err := resolveResponsesAPIKey("")
		if err == nil {
			t.Fatal("expected error for missing key")
		}
	})
}
