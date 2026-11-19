package watch

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cowdogmoo/squad/session"
)

// TestSummarizeAgentActivity covers the per-turn and per-tool events every
// provider now writes.
func TestSummarizeAgentActivity(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name    string
		ev      session.Event
		wantSub []string
		notSub  []string
	}{
		{
			name:    "response with text",
			ev:      event(now, session.EventResponse, map[string]any{"iteration": 2, "input_tokens": 7, "output_tokens": 3, "text": "I'll check\nthe notes."}),
			wantSub: []string{"response (iter 2)", "+7 in", "+3 out", "I'll check the notes."},
		},
		{
			name:    "tool result with size, duration and error",
			ev:      event(now, session.EventToolResult, map[string]any{"name": "Bash", "bytes": 256, "duration_ms": 22, "is_error": true, "error": "command failed:\nexit status 1"}),
			wantSub: []string{"result · Bash", "256 B", "22ms", "error: command failed: exit status 1"},
		},
		{
			name:    "tool result without timing",
			ev:      event(now, session.EventToolResult, map[string]any{"name": "Read"}),
			wantSub: []string{"result · Read"},
			notSub:  []string{" B", "ms", "error"},
		},
		{
			name:    "tool denied",
			ev:      event(now, session.EventToolDenied, map[string]any{"name": "Write", "reason": "readonly"}),
			wantSub: []string{"denied · Write · readonly"},
		},
		{
			name:    "multi-line args collapse",
			ev:      event(now, session.EventToolCall, map[string]any{"name": "Bash", "args": "{\"command\":\"a\n b\"}"}),
			wantSub: []string{`Bash  {"command":"a b"}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(tt.ev)
			for _, s := range tt.wantSub {
				if !strings.Contains(got, s) {
					t.Errorf("summarize = %q, missing %q", got, s)
				}
			}
			for _, s := range tt.notSub {
				if strings.Contains(got, s) {
					t.Errorf("summarize = %q, should not contain %q", got, s)
				}
			}
		})
	}
}

// TestSummarizeCutsModelTextOnRuneBoundaries checks that summaries of
// free-form model text stay valid UTF-8 when the cut lands inside a
// multi-byte character.
func TestSummarizeCutsModelTextOnRuneBoundaries(t *testing.T) {
	now := time.Now()
	splitAt := func(w int) string {
		return strings.Repeat("a", w-2) + "—" + strings.Repeat("b", 20)
	}
	for _, ev := range []session.Event{
		event(now, session.EventResponse, map[string]any{"text": splitAt(100)}),
		event(now, session.EventToolResult, map[string]any{"name": "Bash", "error": splitAt(60)}),
		event(now, session.EventToolCall, map[string]any{"name": "Bash", "args": splitAt(80)}),
	} {
		got := summarize(ev)
		if !utf8.ValidString(got) {
			t.Errorf("summarize(%s) = %q, not valid UTF-8", ev.Type, got)
		}
		if !strings.HasSuffix(got, "a…") {
			t.Errorf("summarize(%s) = %q, want the cut to drop the split rune", ev.Type, got)
		}
	}
}

func TestTailerToolOutcomesAreNotRunErrors(t *testing.T) {
	tl := NewTailer(t.TempDir())
	now := time.Now()
	for _, ev := range []session.Event{
		event(now, session.EventToolResult, map[string]any{"name": "Bash", "is_error": true}),
		event(now, session.EventToolResult, map[string]any{"name": "Read", "is_error": false}),
		event(now, session.EventToolDenied, map[string]any{"name": "Write", "reason": "readonly"}),
		event(now, session.EventError, map[string]any{"phase": "generate", "error": "timeout"}),
	} {
		tl.applyEvent(ev)
	}
	c := tl.State().Counts
	if c.Errors != 1 {
		t.Errorf("counts = %+v", c)
	}
	if tl.State().LastError != "timeout" {
		t.Errorf("LastError = %q", tl.State().LastError)
	}
}
