package ollama

import (
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func TestNewTrimsServerURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		url     string
		wantURL string
	}{
		{"trailing slash", "http://localhost:11434/", "http://localhost:11434"},
		{"trailing /v1/", "http://localhost:11434/v1/", "http://localhost:11434/v1"},
		{"trailing /v1", "http://localhost:11434/v1", "http://localhost:11434"},
		{"bare URL", "http://localhost:11434", "http://localhost:11434"},
		{"empty", "", ""},
		{"double slash", "http://localhost:11434//", "http://localhost:11434/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm := New(tt.url, "mistral", 4096)
			if llm.serverURL != tt.wantURL {
				t.Fatalf("New(%q).serverURL = %q, want %q", tt.url, llm.serverURL, tt.wantURL)
			}
		})
	}
}

func TestLcRoleToOllama(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		role llms.ChatMessageType
		want string
	}{
		{"system", llms.ChatMessageTypeSystem, "system"},
		{"human", llms.ChatMessageTypeHuman, "user"},
		{"AI", llms.ChatMessageTypeAI, "assistant"},
		{"tool", llms.ChatMessageTypeTool, "tool"},
		{"generic", llms.ChatMessageTypeGeneric, "user"},
		{"unknown string", llms.ChatMessageType("unknown"), "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := lcRoleToOllama(tt.role); got != tt.want {
				t.Fatalf("lcRoleToOllama(%q) = %q, want %q", tt.role, got, tt.want)
			}
		})
	}
}

func TestConvertMessages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		messages []llms.MessageContent
		wantLen  int
		wantErr  bool
	}{
		{
			"empty slice",
			nil,
			0,
			false,
		},
		{
			"text only",
			[]llms.MessageContent{
				{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart("system")}},
			},
			1,
			false,
		},
		{
			"with tool call",
			[]llms.MessageContent{
				{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
					llms.TextPart("hello"),
					llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Do", Arguments: `{"x":1}`}},
				}},
			},
			1,
			false,
		},
		{
			"with tool response",
			[]llms.MessageContent{
				{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
					llms.TextPart("hello"),
					llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Do", Arguments: `{"x":1}`}},
				}},
				{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{llms.ToolCallResponse{Content: "tool output"}}},
			},
			3,
			false,
		},
		{
			"invalid JSON args",
			[]llms.MessageContent{
				{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
					llms.ToolCall{FunctionCall: &llms.FunctionCall{Name: "Bad", Arguments: "not-json"}},
				}},
			},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := convertMessages(tt.messages)
			if (err != nil) != tt.wantErr {
				t.Fatalf("convertMessages() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(out) != tt.wantLen {
				t.Fatalf("convertMessages() len = %d, want %d", len(out), tt.wantLen)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tools   []llms.Tool
		wantLen int
	}{
		{"nil slice", nil, 0},
		{"empty slice", []llms.Tool{}, 0},
		{
			"one valid tool",
			[]llms.Tool{
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc", Parameters: map[string]any{"type": "object"}}},
			},
			1,
		},
		{
			"tool with nil function filtered",
			[]llms.Tool{
				{},
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc"}},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := convertTools(tt.tools)
			if len(got) != tt.wantLen {
				t.Fatalf("convertTools() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestConvertResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		resp          chatResponse
		wantContent   string
		wantToolCalls int
		wantTokens    int
	}{
		{
			"with message and tool calls",
			chatResponse{
				Message: &ollamaMessage{
					Content: "hello",
					ToolCalls: []ollamaToolCall{{
						Function: ollamaFunctionCall{Name: "Echo", Arguments: map[string]any{"value": "hi"}},
					}},
				},
				PromptEvalCount: 2,
				EvalCount:       3,
			},
			"hello",
			1,
			5,
		},
		{
			"nil message",
			chatResponse{PromptEvalCount: 1, EvalCount: 2},
			"",
			0,
			3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := convertResponse(tt.resp)
			if content == nil || len(content.Choices) != 1 {
				t.Fatalf("expected 1 choice")
			}
			choice := content.Choices[0]
			if choice.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", choice.Content, tt.wantContent)
			}
			if len(choice.ToolCalls) != tt.wantToolCalls {
				t.Fatalf("ToolCalls len = %d, want %d", len(choice.ToolCalls), tt.wantToolCalls)
			}
			if choice.GenerationInfo["TotalTokens"] != tt.wantTokens {
				t.Fatalf("TotalTokens = %v, want %d", choice.GenerationInfo["TotalTokens"], tt.wantTokens)
			}
		})
	}
}
