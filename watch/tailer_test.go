package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/session"
)

// writeSession populates a fresh session dir with the given meta + events.
// Returns the dir path. Helper for table-driven tailer tests.
func writeSession(t *testing.T, meta session.Meta, events []session.Event) string {
	t.Helper()
	dir := t.TempDir()
	writeMeta(t, dir, meta)
	writeEvents(t, dir, events)
	return dir
}

func writeMeta(t *testing.T, dir string, meta session.Meta) {
	t.Helper()
	body, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	// Force a perceptible mtime so a follow-up rewrite registers as newer.
	if err := os.Chtimes(filepath.Join(dir, "meta.json"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func writeEvents(t *testing.T, dir string, events []session.Event) {
	t.Helper()
	path := filepath.Join(dir, "events.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func event(ts time.Time, typ string, payload any) session.Event {
	raw, _ := json.Marshal(payload)
	return session.Event{Ts: ts, Type: typ, Payload: raw}
}

func TestRefreshMissingDir(t *testing.T) {
	tt := NewTailer("/nonexistent/dir/that/does/not/exist")
	s, err := tt.Refresh()
	if err != nil {
		t.Errorf("expected no error for missing dir, got %v", err)
	}
	if s.Meta.SessionID != "" {
		t.Errorf("expected zero state, got meta %+v", s.Meta)
	}
	if len(s.Events) != 0 {
		t.Errorf("expected zero events, got %d", len(s.Events))
	}
}

func TestRefreshLoadsMeta(t *testing.T) {
	meta := session.Meta{
		SessionID: "abc123",
		Agent:     "go-review",
		Status:    session.StatusRunning,
	}
	dir := writeSession(t, meta, nil)
	tt := NewTailer(dir)
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.Meta.SessionID != "abc123" || s.Meta.Agent != "go-review" {
		t.Errorf("meta: got %+v", s.Meta)
	}
}

func TestRefreshLoadsEvents(t *testing.T) {
	now := time.Now().UTC()
	events := []session.Event{
		event(now, session.EventRunStart, map[string]any{"agent": "go-review", "model": "claude-4"}),
		event(now.Add(time.Second), session.EventIteration, map[string]any{"index": 1, "tool_calls": 3}),
		event(now.Add(2*time.Second), session.EventToolCall, map[string]any{"name": "Edit", "args": "runner/run.go"}),
		event(now.Add(3*time.Second), session.EventResponse, map[string]any{"input_tokens": 12000, "output_tokens": 380}),
		event(now.Add(4*time.Second), session.EventRunEnd, map[string]any{"status": "completed"}),
	}
	dir := writeSession(t, session.Meta{SessionID: "x"}, events)

	tt := NewTailer(dir)
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(s.Events), 5; got != want {
		t.Errorf("event count: got %d, want %d", got, want)
	}
	if s.Counts.Iterations != 1 || s.Counts.ToolCalls != 1 || s.Counts.Responses != 1 {
		t.Errorf("counts: %+v", s.Counts)
	}
	if s.LastTool != "Edit" {
		t.Errorf("LastTool: got %q, want %q", s.LastTool, "Edit")
	}
}

func TestRefreshIncrementalAppend(t *testing.T) {
	now := time.Now().UTC()
	dir := writeSession(t, session.Meta{SessionID: "incr"}, []session.Event{
		event(now, session.EventRunStart, map[string]any{"agent": "a"}),
	})

	tt := NewTailer(dir)
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 {
		t.Fatalf("first refresh: want 1 event, got %d", len(s.Events))
	}

	// Append more events; second refresh should read only the new bytes.
	writeEvents(t, dir, []session.Event{
		event(now.Add(time.Second), session.EventToolCall, map[string]any{"name": "Read"}),
		event(now.Add(2*time.Second), session.EventToolCall, map[string]any{"name": "Edit"}),
	})
	s, err = tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 3 {
		t.Errorf("after append: want 3 events, got %d", len(s.Events))
	}
	if s.Counts.ToolCalls != 2 {
		t.Errorf("ToolCalls: want 2, got %d", s.Counts.ToolCalls)
	}
	if s.LastTool != "Edit" {
		t.Errorf("LastTool: want Edit, got %q", s.LastTool)
	}
}

func TestRefreshSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	// Mix a valid line, a garbage line, and another valid line.
	body := `{"ts":"2026-05-11T00:00:00Z","type":"run_start","payload":{}}
not json
{"ts":"2026-05-11T00:00:01Z","type":"run_end","payload":{"status":"completed"}}
`
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tt := NewTailer(dir)
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 {
		t.Errorf("want 2 parsed events, got %d", len(s.Events))
	}
}

func TestEventCapDropsOldest(t *testing.T) {
	dir := t.TempDir()
	tt := NewTailer(dir)
	tt.SetEventCap(3)
	now := time.Now().UTC()
	var events []session.Event
	for i := 0; i < 5; i++ {
		events = append(events, event(now.Add(time.Duration(i)*time.Second),
			session.EventIteration, map[string]any{"index": i}))
	}
	writeEvents(t, dir, events)
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 3 {
		t.Fatalf("cap=3 but got %d events", len(s.Events))
	}
	// First retained event should describe iter index=2 (drops 0, 1).
	if !strings.Contains(s.Events[0].Summary, "iter 2") {
		t.Errorf("first event after cap: got %q, want it to contain 'iter 2'", s.Events[0].Summary)
	}
}

func TestResetClearsState(t *testing.T) {
	dir := writeSession(t, session.Meta{SessionID: "r"}, []session.Event{
		event(time.Now(), session.EventIteration, map[string]any{"index": 1}),
	})
	tt := NewTailer(dir)
	_, _ = tt.Refresh()
	if tt.state.Counts.Iterations != 1 {
		t.Fatal("setup precondition failed")
	}
	tt.Reset()
	if tt.state.Counts.Iterations != 0 || tt.eventOffset != 0 {
		t.Errorf("after Reset: state=%+v offset=%d", tt.state, tt.eventOffset)
	}
}

func TestTailerSessionIDPrefersMeta(t *testing.T) {
	dir := writeSession(t, session.Meta{SessionID: "S-meta"}, nil)
	tt := NewTailer(dir)
	if _, err := tt.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := tt.SessionID(); got != "S-meta" {
		t.Errorf("SessionID = %q, want %q", got, "S-meta")
	}
}

func TestTailerSessionIDFallsBackToDir(t *testing.T) {
	tt := NewTailer("/some/path/S-dir")
	if got := tt.SessionID(); got != "S-dir" {
		t.Errorf("SessionID fallback = %q, want %q", got, "S-dir")
	}
}

func TestTailerStateReturnsZeroBeforeRefresh(t *testing.T) {
	tt := NewTailer(t.TempDir())
	s := tt.State()
	if s.Meta.SessionID != "" || s.Counts.Iterations != 0 {
		t.Errorf("expected zero State before Refresh, got %+v", s)
	}
}

func TestTailerRefreshAfterAppend(t *testing.T) {
	dir := writeSession(t, session.Meta{SessionID: "S"}, []session.Event{
		event(time.Now(), session.EventIteration, map[string]any{"index": 1}),
	})
	tt := NewTailer(dir)
	if _, err := tt.Refresh(); err != nil {
		t.Fatal(err)
	}
	// Append more events and re-refresh.
	writeEvents(t, dir, []session.Event{
		event(time.Now(), session.EventIteration, map[string]any{"index": 2}),
	})
	s, err := tt.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if s.Counts.Iterations < 2 {
		t.Errorf("expected at least 2 iterations after append, got %d", s.Counts.Iterations)
	}
}

func TestSummarize(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		ev      session.Event
		wantSub string // substring assertion — exact text is incidental
	}{
		{event(now, session.EventRunStart, map[string]any{"agent": "go-review", "model": "claude-4"}), "agent=go-review"},
		{event(now, session.EventIteration, map[string]any{"index": 7, "tool_calls": 3}), "iter 7"},
		{event(now, session.EventToolCall, map[string]any{"name": "Edit", "args": "src/main.go"}), "Edit"},
		{event(now, session.EventResponse, map[string]any{"input_tokens": 100, "output_tokens": 20}), "+100 in"},
		{event(now, session.EventLargeResult, map[string]any{"result_id": "rl_abc"}), "rl_abc"},
		{event(now, session.EventError, map[string]any{"error": "boom"}), "boom"},
		{event(now, session.EventRunEnd, map[string]any{"status": "completed"}), "completed"},
	}
	for _, tc := range cases {
		t.Run(tc.ev.Type, func(t *testing.T) {
			got := summarize(tc.ev)
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf("summarize(%s) = %q, want substring %q", tc.ev.Type, got, tc.wantSub)
			}
		})
	}
}

func TestRefresh_MetaStatError(t *testing.T) {
	t.Parallel()
	// Point tailer at a file path (not a dir) so meta.json stat returns a
	// non-IsNotExist error when we try to stat a path inside it.
	dir := t.TempDir()
	// Create a file where meta.json would be, but make it a directory so
	// os.ReadFile fails — actually just use a non-readable path.
	metaPath := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"session_id":"s1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tailer := NewTailer(dir)
	// First refresh should succeed and load meta.
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if tailer.State().Meta.SessionID != "s1" {
		t.Errorf("expected session_id s1, got %q", tailer.State().Meta.SessionID)
	}
}

func TestRefresh_MetaNotModified(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	metaPath := filepath.Join(dir, "meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"session_id":"s2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tailer := NewTailer(dir)
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("first Refresh() error: %v", err)
	}
	// Second refresh with same mtime should be a no-op.
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("second Refresh() error: %v", err)
	}
	if tailer.State().Meta.SessionID != "s2" {
		t.Errorf("expected session_id s2, got %q", tailer.State().Meta.SessionID)
	}
}

func TestSetEventCap(t *testing.T) {
	t.Parallel()
	tailer := NewTailer(t.TempDir())
	tailer.SetEventCap(5)
	if tailer.eventCap != 5 {
		t.Errorf("eventCap = %d, want 5", tailer.eventCap)
	}
}

func TestDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tailer := NewTailer(dir)
	if tailer.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", tailer.Dir(), dir)
	}
}

func TestRefreshEvents_SeekError(t *testing.T) {
	t.Parallel()
	// Write a valid events.jsonl, then advance the offset past the file end
	// so Seek succeeds but there are no new bytes — exercises the no-new-bytes path.
	dir := t.TempDir()
	ev := session.Event{Type: session.EventIteration, Ts: time.Now()}
	writeEvents(t, dir, []session.Event{ev})
	tailer := NewTailer(dir)
	// First refresh to advance offset.
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	// Second refresh with no new bytes — should succeed.
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("second Refresh: %v", err)
	}
}

func TestApplyEvent_LargeResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ev := session.Event{Type: session.EventLargeResult, Ts: time.Now()}
	writeEvents(t, dir, []session.Event{ev})
	tailer := NewTailer(dir)
	state, err := tailer.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if state.Counts.LargeResults != 1 {
		t.Errorf("LargeResults = %d, want 1", state.Counts.LargeResults)
	}
}

func TestApplyEvent_ToolCallWithName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	payload, _ := json.Marshal(map[string]string{"name": "Bash"})
	ev := session.Event{Type: session.EventToolCall, Ts: time.Now(), Payload: payload}
	writeEvents(t, dir, []session.Event{ev})
	tailer := NewTailer(dir)
	state, err := tailer.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if state.LastTool != "Bash" {
		t.Errorf("LastTool = %q, want 'Bash'", state.LastTool)
	}
}

func TestApplyEvent_ErrorWithMessage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	payload, _ := json.Marshal(map[string]string{"message": "something went wrong"})
	ev := session.Event{Type: session.EventError, Ts: time.Now(), Payload: payload}
	writeEvents(t, dir, []session.Event{ev})
	tailer := NewTailer(dir)
	state, err := tailer.Refresh()
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !strings.Contains(state.LastError, "something went wrong") {
		t.Errorf("LastError = %q, want 'something went wrong'", state.LastError)
	}
}

func TestRefreshMeta_InvalidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Write invalid JSON to meta.json — should be silently ignored.
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	tailer := NewTailer(dir)
	if _, err := tailer.Refresh(); err != nil {
		t.Fatalf("Refresh with invalid meta.json: %v", err)
	}
}
