package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
)

// TestToolLoopEditDeadlineGraceThenStop drives a run that only reads: the
// edit deadline opens a grace period, the model is told reads are disabled,
// and once the grace iterations are spent the loop stops.
func TestToolLoopEditDeadlineGraceThenStop(t *testing.T) {
	var responses []*llms.ContentResponse
	for i := 0; i < 8; i++ {
		responses = append(responses, toolCallResp(fmt.Sprintf("c%d", i), "Read", fmt.Sprintf(`{"path":"file-%d.txt"}`, i)))
	}
	llm := &scriptedLLM{responses: responses}
	_, _, err, done := toolLoop(context.Background(), llm, buildInitialMessages("sys", "user", false),
		map[string]Handler{"Read": {Call: func(context.Context, []byte) (string, error) { return "contents", nil }}},
		8, RunWithToolsConfig{EditDeadline: 1}, nil)
	if done || !errors.Is(err, ErrEditDeadlineReached) {
		t.Fatalf("toolLoop = done %v, err %v; want ErrEditDeadlineReached", done, err)
	}
	if !sawMessage(llm.msgs, "[EDIT DEADLINE REACHED]") {
		t.Error("the model was never told the edit deadline was reached")
	}
	if llm.calls >= len(responses) {
		t.Errorf("model was called %d times; the loop should stop before the iteration cap", llm.calls)
	}
}

// sawMessage reports whether any request contained a text part with substr.
func sawMessage(requests [][]llms.MessageContent, substr string) bool {
	for _, msgs := range requests {
		for _, m := range msgs {
			for _, p := range m.Parts {
				if tp, ok := p.(llms.TextContent); ok && strings.Contains(tp.Text, substr) {
					return true
				}
			}
		}
	}
	return false
}

// TestFinishToolLoopEmptyFinalResponse checks that a final call returning no
// choices falls back to the last partial text and records no turn result.
func TestFinishToolLoopEmptyFinalResponse(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	l, err := session.New(t.TempDir(), "", "probe", "ollama", "qwen", "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	ctx := session.WithLogger(context.Background(), l)

	out, err := finishToolLoop(ctx, &stubLLM{resp: &llms.ContentResponse{}}, nil, "partial answer", 1, nil, nil, 0)
	if err != nil || out != "partial answer" {
		t.Fatalf("finishToolLoop = %q, %v; want the partial answer", out, err)
	}
	data, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"type":"response"`) || strings.Contains(string(data), `"type":"iteration"`) {
		t.Errorf("events = %s, want no turn result for an empty final response", data)
	}
}
