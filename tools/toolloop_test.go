package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tmc/langchaingo/llms"
)

type fakeLLM struct {
	responses []*llms.ContentResponse
	calls     int
}

type stubLLM struct {
	resp *llms.ContentResponse
	err  error
}

func (f *fakeLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	if f.calls >= len(f.responses) {
		return nil, errors.New("no response")
	}
	resp := f.responses[f.calls]
	f.calls++
	return resp, nil
}

func (f *fakeLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func (s *stubLLM) GenerateContent(_ context.Context, _ []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	return s.resp, s.err
}

func (s *stubLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func TestExecuteToolCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		toolCall    llms.ToolCall
		handlers    map[string]Handler
		wantContent string
		wantName    string
	}{
		{
			"nil FunctionCall",
			llms.ToolCall{ID: "1"},
			map[string]Handler{},
			"tool call missing function definition",
			"",
		},
		{
			"unknown tool name",
			llms.ToolCall{ID: "2", FunctionCall: &llms.FunctionCall{Name: "Missing"}},
			map[string]Handler{},
			"unknown tool: Missing",
			"",
		},
		{
			"known tool success",
			llms.ToolCall{ID: "3", FunctionCall: &llms.FunctionCall{Name: "Echo", Arguments: "{}"}},
			map[string]Handler{
				"Echo": {Call: func(context.Context, []byte) (string, error) { return "ok", nil }},
			},
			"ok",
			"Echo",
		},
		{
			"known tool error",
			llms.ToolCall{ID: "4", FunctionCall: &llms.FunctionCall{Name: "Fail", Arguments: "{}"}},
			map[string]Handler{
				"Fail": {Call: func(context.Context, []byte) (string, error) { return "", errors.New("boom") }},
			},
			"error: boom",
			"Fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resp := executeToolCall(context.Background(), tt.toolCall, tt.handlers)
			if resp.Content != tt.wantContent {
				t.Fatalf("Content = %q, want %q", resp.Content, tt.wantContent)
			}
			if resp.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", resp.Name, tt.wantName)
			}
		})
	}
}

func TestRepeatTrackerUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		calls     [][]llms.ToolCall
		wantCount int
		wantName  string
	}{
		{
			"single call",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
			},
			1,
			"A",
		},
		{
			"repeated identical call",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
			},
			3,
			"A",
		},
		{
			"different call resets",
			[][]llms.ToolCall{
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "A", Arguments: "{}"}}},
				{{FunctionCall: &llms.FunctionCall{Name: "B", Arguments: "{}"}}},
			},
			1,
			"B",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := &RepeatTracker{}
			for _, c := range tt.calls {
				tracker.Update(c)
			}
			if tracker.Count != tt.wantCount {
				t.Fatalf("Count = %d, want %d", tracker.Count, tt.wantCount)
			}
			if tracker.LastName != tt.wantName {
				t.Fatalf("LastName = %q, want %q", tracker.LastName, tt.wantName)
			}
		})
	}
}

func TestRepeatTrackerExceeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		toolName string
		count    int
		want     bool
	}{
		{"normal tool at limit", "Other", maxSameToolRepeat, true},
		{"normal tool below limit", "Other", maxSameToolRepeat - 1, false},
		{"mutating tool at limit", "Edit", maxMutatingToolRepeat, true},
		{"mutating tool below limit", "Edit", maxMutatingToolRepeat - 1, false},
		{"high repeat tool at limit", "Read", MaxToolIterations, true},
		{"high repeat tool below limit", "Read", MaxToolIterations - 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := &RepeatTracker{LastName: tt.toolName, Count: tt.count}
			if got := tracker.Exceeded(); got != tt.want {
				t.Fatalf("Exceeded() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunWithToolsLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	llm := &fakeLLM{responses: []*llms.ContentResponse{
		{
			Choices: []*llms.ContentChoice{{
				ToolCalls: []llms.ToolCall{{
					ID:   "1",
					Type: "function",
					FunctionCall: &llms.FunctionCall{
						Name:      "Read",
						Arguments: `{"path":"note.txt"}`,
					},
				}},
			}},
		},
		{
			Choices: []*llms.ContentChoice{{Content: "done"}},
		},
	}}

	out, err := RunWithTools(context.Background(), llm, "", "user", dir, 2, nil)
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if out != "done" {
		t.Fatalf("output = %q, want %q", out, "done")
	}
}

func TestFinishToolLoopFallback(t *testing.T) {
	t.Parallel()
	llm := &fakeLLM{responses: []*llms.ContentResponse{{Choices: []*llms.ContentChoice{{Content: ""}}}}}
	out, err := finishToolLoop(context.Background(), llm, nil, "partial", 1, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "partial" {
		t.Fatalf("output = %q, want %q", out, "partial")
	}
}

func TestRunWithToolsErrors(t *testing.T) {
	tests := []struct {
		name string
		llm  llms.Model
	}{
		{
			name: "generate error",
			llm:  &stubLLM{err: errors.New("boom")},
		},
		{
			name: "nil response",
			llm:  &stubLLM{resp: nil},
		},
		{
			name: "empty choices",
			llm:  &stubLLM{resp: &llms.ContentResponse{Choices: nil}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RunWithTools(context.Background(), tt.llm, "", "user", t.TempDir(), 1, nil)
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestFinishToolLoopFinalContent(t *testing.T) {
	llm := &stubLLM{
		resp: &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "final"}}},
	}
	out, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil)
	if err != nil {
		t.Fatalf("finishToolLoop() error = %v", err)
	}
	if out != "final" {
		t.Fatalf("output = %q, want %q", out, "final")
	}
}

func TestFinishToolLoopErrorNoContent(t *testing.T) {
	llm := &stubLLM{err: errors.New("boom")}
	_, err := finishToolLoop(context.Background(), llm, nil, "", 1, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}
