package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/session"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"under width", "hello", 10, "hello"},
		{"equal width", "hello", 5, "hello"},
		{"over width", "hello world", 5, "hell…"},
		{"zero width returns input", "hello", 0, "hello"},
		{"negative width returns input", "hello", -3, "hello"},
		{"width of one is ellipsis", "hello", 1, "…"},
		{"empty input", "", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.s, tc.w); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
			}
		})
	}
}

func TestNum(t *testing.T) {
	cases := []struct {
		name string
		p    map[string]any
		key  string
		want string
	}{
		{"nil map", nil, "x", "0"},
		{"missing key", map[string]any{"a": 1}, "x", "0"},
		{"float64 integral", map[string]any{"n": float64(42)}, "n", "42"},
		{"float64 fractional", map[string]any{"n": 1.5}, "n", "1.5"},
		{"int", map[string]any{"n": 7}, "n", "7"},
		{"int64", map[string]any{"n": int64(9)}, "n", "9"},
		{"string passthrough", map[string]any{"n": "12k"}, "n", "12k"},
		{"unknown type", map[string]any{"n": true}, "n", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := num(tc.p, tc.key); got != tc.want {
				t.Errorf("num(%v, %q) = %q, want %q", tc.p, tc.key, got, tc.want)
			}
		})
	}
}

func TestStrFallbacks(t *testing.T) {
	if got := str(nil, "k"); got != "" {
		t.Errorf("str(nil) = %q, want empty", got)
	}
	if got := str(map[string]any{"k": 7}, "k"); got != "" {
		t.Errorf("str(non-string) = %q, want empty", got)
	}
	if got := str(map[string]any{"k": "v"}, "x"); got != "" {
		t.Errorf("str(missing) = %q, want empty", got)
	}
}

func TestDecodePayload(t *testing.T) {
	if decodePayload(nil) != nil {
		t.Errorf("decodePayload(nil) should be nil")
	}
	if decodePayload([]byte("not json")) != nil {
		t.Errorf("decodePayload(malformed) should be nil after failed unmarshal")
	}
	p := decodePayload([]byte(`{"a":1}`))
	if p == nil || p["a"] == nil {
		t.Errorf("decodePayload(valid) = %v, want parsed map", p)
	}
}

func TestSummarizeFallback(t *testing.T) {
	// Unknown event types return the raw type string.
	got := summarize(session.Event{Type: "mystery"})
	if got != "mystery" {
		t.Errorf("unknown type fallback: got %q, want %q", got, "mystery")
	}
}

func TestSummarizeAllBranches(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name    string
		ev      session.Event
		wantSub []string
	}{
		{
			name:    "resume",
			ev:      event(now, session.EventResume, map[string]any{"session_id": "old-123"}),
			wantSub: []string{"resumed", "old-123"},
		},
		{
			name:    "prompt with text",
			ev:      event(now, session.EventPrompt, map[string]any{"text": "do thing"}),
			wantSub: []string{"prompt", "do thing"},
		},
		{
			name:    "prompt without text",
			ev:      event(now, session.EventPrompt, map[string]any{}),
			wantSub: []string{"prompt", "(prompt sent)"},
		},
		{
			name:    "tool call without args",
			ev:      event(now, session.EventToolCall, map[string]any{"name": "Read"}),
			wantSub: []string{"Read"},
		},
		{
			name:    "tool result",
			ev:      event(now, session.EventToolResult, map[string]any{"name": "Edit"}),
			wantSub: []string{"result", "Edit"},
		},
		{
			name:    "response with label",
			ev:      event(now, session.EventResponse, map[string]any{"label": "stream", "input_tokens": 10, "output_tokens": 5}),
			wantSub: []string{"(stream)", "+10 in", "+5 out"},
		},
		{
			name:    "error falls back to message field",
			ev:      event(now, session.EventError, map[string]any{"message": "fallback msg"}),
			wantSub: []string{"error", "fallback msg"},
		},
		{
			name:    "run_end with error",
			ev:      event(now, session.EventRunEnd, map[string]any{"status": "failed", "error": "oops"}),
			wantSub: []string{"ended", "failed", "oops"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarize(tc.ev)
			for _, sub := range tc.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("summarize(%s) = %q, missing substring %q", tc.ev.Type, got, sub)
				}
			}
		})
	}
}

func TestApplyEventErrorFallbackToMessage(t *testing.T) {
	dir := t.TempDir()
	tt := NewTailer(dir)
	now := time.Now().UTC()
	writeEvents(t, dir, []session.Event{
		event(now, session.EventError, map[string]any{"message": "fallback"}),
		event(now.Add(time.Second), session.EventLargeResult, map[string]any{"result_id": "lr_x"}),
	})
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.LastError != "fallback" {
		t.Errorf("LastError fallback: got %q, want fallback", s.LastError)
	}
	if s.Counts.LargeResults != 1 {
		t.Errorf("LargeResults count: got %d, want 1", s.Counts.LargeResults)
	}
}

func TestApplyEventRunEndCarriesError(t *testing.T) {
	dir := t.TempDir()
	tt := NewTailer(dir)
	now := time.Now().UTC()
	writeEvents(t, dir, []session.Event{
		event(now, session.EventRunEnd, map[string]any{"status": "failed", "error": "broke"}),
	})
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.LastError != "broke" {
		t.Errorf("LastError after run_end: got %q, want broke", s.LastError)
	}
}
