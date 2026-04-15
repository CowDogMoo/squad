package tools

import (
	"testing"

	"github.com/tmc/langchaingo/llms"
)

func makeToolCall(id, name, args string) llms.ToolCall {
	return llms.ToolCall{
		ID: id,
		FunctionCall: &llms.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func TestLoopDetector_NotStuckOnVariedCalls(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}
	for i := 0; i < 10; i++ {
		calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"file`+string(rune('a'+i))+`.go"}`)}
		results := map[string]string{"1": "content " + string(rune('a'+i))}
		ld.Record(calls, results)
		if ld.Stuck() {
			t.Fatalf("should not be stuck after %d varied calls", i+1)
		}
	}
}

func TestLoopDetector_StuckOnIdenticalCalls(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}
	calls := []llms.ToolCall{makeToolCall("1", "Grep", `{"pattern":"foo"}`)}
	results := map[string]string{"1": "no matches"}

	for i := 0; i < loopMaxRepeats-1; i++ {
		ld.Record(calls, results)
		if ld.Stuck() {
			t.Fatalf("should not be stuck after only %d repeats", i+1)
		}
	}
	ld.Record(calls, results)
	if !ld.Stuck() {
		t.Fatal("should be stuck after loopMaxRepeats identical calls")
	}
}

func TestLoopDetector_SlidingWindow(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}

	// Fill window with identical calls.
	calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"a.go"}`)}
	results := map[string]string{"1": "content a"}
	for i := 0; i < loopMaxRepeats-1; i++ {
		ld.Record(calls, results)
	}

	// Push the old repeats out of the window with varied calls.
	for i := 0; i < loopWindowSize; i++ {
		varied := []llms.ToolCall{makeToolCall("1", "Bash", `{"command":"echo `+string(rune('a'+i))+`"}`)}
		variedResults := map[string]string{"1": "output " + string(rune('a'+i))}
		ld.Record(varied, variedResults)
	}

	if ld.Stuck() {
		t.Fatal("old repeats should have slid out of the window")
	}
}

func TestLoopDetector_SameCallDifferentResult(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}
	calls := []llms.ToolCall{makeToolCall("1", "Grep", `{"pattern":"foo"}`)}

	for i := 0; i < loopMaxRepeats+2; i++ {
		results := map[string]string{"1": "result " + string(rune('a'+i))}
		ld.Record(calls, results)
	}

	if ld.Stuck() {
		t.Fatal("same call with different results should not trigger loop detection")
	}
}

func TestLoopDetector_EmptyCallsIgnored(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}
	ld.Record(nil, nil)
	ld.Record([]llms.ToolCall{}, nil)
	if ld.Stuck() {
		t.Fatal("empty calls should not trigger loop detection")
	}
}

func TestLoopDetector_MultipleToolCalls(t *testing.T) {
	t.Parallel()
	ld := &LoopDetector{}
	calls := []llms.ToolCall{
		makeToolCall("1", "Read", `{"path":"a.go"}`),
		makeToolCall("2", "Read", `{"path":"b.go"}`),
	}
	results := map[string]string{"1": "content a", "2": "content b"}

	for i := 0; i < loopMaxRepeats; i++ {
		ld.Record(calls, results)
	}
	if !ld.Stuck() {
		t.Fatal("identical multi-tool steps should trigger loop detection")
	}
}
