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

func TestLoopDetector(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(ld *LoopDetector)
		wantStuck bool
	}{
		{
			name:      "not stuck initially",
			setup:     func(ld *LoopDetector) {},
			wantStuck: false,
		},
		{
			name: "stuck after identical repeats",
			setup: func(ld *LoopDetector) {
				calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"file.go"}`)}
				results := map[string]string{"1": "file content"}
				for i := 0; i < loopMaxRepeats; i++ {
					ld.Record(calls, results)
				}
			},
			wantStuck: true,
		},
		{
			name: "not stuck with one fewer than max repeats",
			setup: func(ld *LoopDetector) {
				calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"file.go"}`)}
				results := map[string]string{"1": "file content"}
				for i := 0; i < loopMaxRepeats-1; i++ {
					ld.Record(calls, results)
				}
			},
			wantStuck: false,
		},
		{
			name: "different calls not stuck",
			setup: func(ld *LoopDetector) {
				for i := 0; i < loopMaxRepeats+2; i++ {
					calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"file`+string(rune('0'+i))+`.go"}`)}
					ld.Record(calls, map[string]string{"1": "content"})
				}
			},
			wantStuck: false,
		},
		{
			name: "same call different results not stuck",
			setup: func(ld *LoopDetector) {
				calls := []llms.ToolCall{makeToolCall("1", "Grep", `{"pattern":"foo"}`)}
				for i := 0; i < loopMaxRepeats+2; i++ {
					results := map[string]string{"1": "result " + string(rune('a'+i))}
					ld.Record(calls, results)
				}
			},
			wantStuck: false,
		},
		{
			name: "empty calls ignored",
			setup: func(ld *LoopDetector) {
				for i := 0; i < loopMaxRepeats+5; i++ {
					ld.Record(nil, nil)
				}
			},
			wantStuck: false,
		},
		{
			name: "window sliding pushes old repeats out",
			setup: func(ld *LoopDetector) {
				// Fill with unique steps to push old ones out of the window.
				for i := 0; i < loopWindowSize; i++ {
					calls := []llms.ToolCall{makeToolCall("1", "Read", `{"path":"unique`+string(rune('a'+i))+`.go"}`)}
					ld.Record(calls, map[string]string{"1": "content"})
				}
				// Add repeated steps below threshold.
				repeated := []llms.ToolCall{makeToolCall("1", "Write", `{"path":"out.go","content":"x"}`)}
				for i := 0; i < loopMaxRepeats-1; i++ {
					ld.Record(repeated, map[string]string{"1": "ok"})
				}
			},
			wantStuck: false,
		},
		{
			name: "multiple tool calls per step",
			setup: func(ld *LoopDetector) {
				calls := []llms.ToolCall{
					makeToolCall("1", "Read", `{"path":"a.go"}`),
					makeToolCall("2", "Read", `{"path":"b.go"}`),
				}
				results := map[string]string{"1": "content a", "2": "content b"}
				for i := 0; i < loopMaxRepeats; i++ {
					ld.Record(calls, results)
				}
			},
			wantStuck: true,
		},
		{
			name: "nil FunctionCall handled gracefully",
			setup: func(ld *LoopDetector) {
				calls := []llms.ToolCall{
					{ID: "1", FunctionCall: nil},
					makeToolCall("2", "Read", `{"path":"a.go"}`),
				}
				results := map[string]string{"2": "content"}
				ld.Record(calls, results)
			},
			wantStuck: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ld := &LoopDetector{}
			tt.setup(ld)
			if got := ld.Stuck(); got != tt.wantStuck {
				t.Errorf("Stuck() = %v, want %v", got, tt.wantStuck)
			}
		})
	}
}

func TestStepSignature(t *testing.T) {
	tests := []struct {
		name      string
		calls1    []llms.ToolCall
		results1  map[string]string
		calls2    []llms.ToolCall
		results2  map[string]string
		wantSame  bool
		wantEmpty bool
	}{
		{
			name:     "deterministic for same input",
			calls1:   []llms.ToolCall{makeToolCall("abc", "Bash", `{"command":"ls"}`)},
			results1: map[string]string{"abc": "file.go\n"},
			calls2:   []llms.ToolCall{makeToolCall("abc", "Bash", `{"command":"ls"}`)},
			results2: map[string]string{"abc": "file.go\n"},
			wantSame: true,
		},
		{
			name:     "different results produce different signatures",
			calls1:   []llms.ToolCall{makeToolCall("1", "Bash", `{"command":"ls"}`)},
			results1: map[string]string{"1": "result-a"},
			calls2:   []llms.ToolCall{makeToolCall("1", "Bash", `{"command":"ls"}`)},
			results2: map[string]string{"1": "result-b"},
			wantSame: false,
		},
		{
			name:      "empty calls produce empty signature",
			calls1:    nil,
			results1:  nil,
			calls2:    nil,
			results2:  nil,
			wantSame:  true,
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig1 := stepSignature(tt.calls1, tt.results1)
			sig2 := stepSignature(tt.calls2, tt.results2)

			if tt.wantEmpty && sig1 != "" {
				t.Errorf("expected empty signature, got %q", sig1)
			}
			if !tt.wantEmpty && sig1 == "" {
				t.Error("expected non-empty signature")
			}
			if tt.wantSame && sig1 != sig2 {
				t.Errorf("signatures should match: %q != %q", sig1, sig2)
			}
			if !tt.wantSame && sig1 == sig2 {
				t.Error("signatures should differ")
			}
		})
	}
}
