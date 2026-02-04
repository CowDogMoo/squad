package responses

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/cowdogmoo/squad/tools"
	openai "github.com/openai/openai-go/v3"
	oairesponses "github.com/openai/openai-go/v3/responses"
	"github.com/tmc/langchaingo/llms"
)

func TestIsReasoningModel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"gpt-5", "gpt-5", true},
		{"gpt-5.2-codex", "gpt-5.2-codex", true},
		{"gpt-5-mini", "gpt-5-mini", true},
		{"gpt-4o", "gpt-4o", false},
		{"claude-3", "claude-3", false},
		{"empty", "", false},
		{"case insensitive", "GPT-5", true},
		{"whitespace", " gpt-5 ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsReasoningModel(tt.model); got != tt.want {
				t.Fatalf("IsReasoningModel(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestUseResponsesAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		model    string
		want     bool
	}{
		{"openai-responses with any model", "openai-responses", "gpt-4o", true},
		{"openai with gpt-5", "openai", "gpt-5", true},
		{"openai with gpt-4", "openai", "gpt-4o", false},
		{"empty provider with gpt-5", "", "gpt-5", false},
		{"ollama with gpt-5", "ollama", "gpt-5", false},
		{"anthropic", "anthropic", "claude-3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := UseResponsesAPI(tt.provider, tt.model); got != tt.want {
				t.Fatalf("UseResponsesAPI(%q, %q) = %v, want %v", tt.provider, tt.model, got, tt.want)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   []llms.Tool
		wantLen int
	}{
		{"nil input", nil, 0},
		{"empty input", []llms.Tool{}, 0},
		{
			"valid tool",
			[]llms.Tool{
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc", Parameters: map[string]any{"type": "object"}}},
			},
			1,
		},
		{
			"tool with nil function",
			[]llms.Tool{
				{},
				{Function: &llms.FunctionDefinition{Name: "Echo", Description: "desc", Parameters: "bad"}},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ConvertTools(tt.input)
			if len(got) != tt.wantLen {
				t.Fatalf("ConvertTools() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestExtractFunctionCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		resp    *oairesponses.Response
		wantLen int
	}{
		{"nil response", nil, 0},
		{"empty output", &oairesponses.Response{Output: nil}, 0},
		{
			"mixed output items",
			&oairesponses.Response{
				Output: []oairesponses.ResponseOutputItemUnion{
					{Type: "message", ID: "msg"},
					{Type: "function_call", ID: "1", CallID: "call-1", Name: "Echo", Arguments: "{}"},
					{Type: "message", ID: "msg2"},
					{Type: "function_call", ID: "2", CallID: "call-2", Name: "Read", Arguments: `{"path":"x"}`},
				},
			},
			2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractFunctionCalls(tt.resp)
			if len(got) != tt.wantLen {
				t.Fatalf("ExtractFunctionCalls() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCheckRepeat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		iterations int
		wantLast   bool
	}{
		{"below threshold", 5, false},
		{"at threshold minus one", 9, false},
		{"at threshold", 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			var repeat tools.RepeatTracker
			var exceeded bool
			for i := 0; i < tt.iterations; i++ {
				exceeded = checkRepeat(ctx, &repeat, []FunctionCall{{Name: "Tool", Arguments: "{}"}})
			}
			if exceeded != tt.wantLast {
				t.Fatalf("checkRepeat after %d iterations = %v, want %v", tt.iterations, exceeded, tt.wantLast)
			}
		})
	}
}

func TestConfigApplyOptionals(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		wantTemp      bool
		wantMaxTokens bool
	}{
		{
			"non-reasoning with temperature",
			Config{Model: "gpt-4o", Temperature: 0.7},
			true,
			false,
		},
		{
			"reasoning model skips temperature",
			Config{Model: "gpt-5-turbo", Temperature: 0.5},
			false,
			true,
		},
		{
			"explicit max tokens",
			Config{Model: "gpt-5", MaxTokens: 2048},
			false,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var params oairesponses.ResponseNewParams
			tt.config.applyOptionals(&params)
			var emptyTemp oairesponses.ResponseNewParams
			hasTemp := !reflect.DeepEqual(params.Temperature, emptyTemp.Temperature)
			if hasTemp != tt.wantTemp {
				t.Fatalf("temperature set = %v, want %v", hasTemp, tt.wantTemp)
			}
			hasMax := !reflect.DeepEqual(params.MaxOutputTokens, emptyTemp.MaxOutputTokens)
			if hasMax != tt.wantMaxTokens {
				t.Fatalf("max tokens set = %v, want %v", hasMax, tt.wantMaxTokens)
			}
		})
	}
}

func TestExecuteAndBuildOutputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handlers := map[string]tools.Handler{
		"Success": {
			Call: func(context.Context, []byte) (string, error) { return "ok", nil },
		},
		"Fail": {
			Call: func(context.Context, []byte) (string, error) { return "", errors.New("boom") },
		},
	}
	tests := []struct {
		name       string
		calls      []FunctionCall
		wantOutput string
	}{
		{
			"missing tool",
			[]FunctionCall{{Name: "Missing", CallID: "call-miss", Arguments: "{}"}},
			"unknown tool: Missing",
		},
		{
			"success",
			[]FunctionCall{{Name: "Success", CallID: "call-ok", Arguments: "{}"}},
			"ok",
		},
		{
			"failure",
			[]FunctionCall{{Name: "Fail", CallID: "call-fail", Arguments: "{}"}},
			"error: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			outputs := executeAndBuildOutputs(ctx, tt.calls, handlers)
			if len(outputs) != 1 {
				t.Fatalf("expected 1 output, got %d", len(outputs))
			}
			got := outputs[0].OfFunctionCallOutput
			if got == nil {
				t.Fatalf("expected function call output")
			}
			if !reflect.DeepEqual(got.Output.OfString, openai.String(tt.wantOutput)) {
				t.Fatalf("output = %v, want %q", got.Output.OfString, tt.wantOutput)
			}
		})
	}
}

func TestLogOutputItems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logOutputItems(ctx, nil, "nil")
	resp := &oairesponses.Response{
		ID:     "resp",
		Status: "completed",
		Usage: oairesponses.ResponseUsage{
			InputTokens:  2,
			OutputTokens: 3,
		},
		Output: []oairesponses.ResponseOutputItemUnion{{Type: "message", ID: "msg"}},
	}
	logOutputItems(ctx, resp, "with-output")
}
