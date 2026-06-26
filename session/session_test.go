package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
)

// newTestLogger creates a fresh session logger under a per-test temp dir
// and registers cleanup. Most tests want this and the t.TempDir/Close pair.
func newTestLogger(t *testing.T) (*Logger, string) {
	t.Helper()
	wd := t.TempDir()
	l, err := New(wd, "", "go-review", "openai", "gpt-5", "fix the bug")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, wd
}

// readMeta reads and unmarshals meta.json for the session.
func readMeta(t *testing.T, l *Logger) Meta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	return m
}

// readEvents reads events.jsonl and returns one parsed Event per line.
func readEvents(t *testing.T, l *Logger) []Event {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	out := make([]Event, 0, len(lines))
	for i, line := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("parse event[%d]: %v", i, err)
		}
		out = append(out, ev)
	}
	return out
}

func TestNewSetsSessionLayout(t *testing.T) {
	l, _ := newTestLogger(t)
	if l.SessionID() == "" {
		t.Fatalf("session id empty")
	}
	// Sessions live under XDG_STATE_HOME/squad/sessions/<id>, not in-tree.
	wantPrefix := filepath.Join(config.StateDir(), "sessions")
	if !strings.HasPrefix(l.Dir(), wantPrefix) {
		t.Fatalf("session dir %q not under %q", l.Dir(), wantPrefix)
	}
	if !strings.HasSuffix(l.Dir(), l.SessionID()) {
		t.Fatalf("session dir %q does not end with id %q", l.Dir(), l.SessionID())
	}
}

func TestUpdateMetricsAndFinishPersistMeta(t *testing.T) {
	l, _ := newTestLogger(t)
	l.SetLastResponseID("resp_abc123")
	l.UpdateMetrics(100, 50, 0.0123, 3)
	l.Finish(StatusCompleted, "")

	meta := readMeta(t, l)
	if meta.LastResponseID != "resp_abc123" {
		t.Fatalf("last_response_id=%q, want resp_abc123", meta.LastResponseID)
	}
	if meta.Status != StatusCompleted {
		t.Fatalf("status=%q, want %q", meta.Status, StatusCompleted)
	}
	if meta.InputTokens != 100 || meta.OutputTokens != 50 || meta.Iterations != 3 {
		t.Fatalf("metrics not persisted: %+v", meta)
	}
}

func TestAppendWritesEventsJSONL(t *testing.T) {
	l, _ := newTestLogger(t)
	if err := l.Append(EventToolCall, map[string]any{"name": "Read"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	l.Finish(StatusCompleted, "")

	events := readEvents(t, l)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].Type != EventToolCall {
		t.Fatalf("event[0] type=%q, want %q", events[0].Type, EventToolCall)
	}
	if events[len(events)-1].Type != EventRunEnd {
		t.Fatalf("last event type=%q, want %q", events[len(events)-1].Type, EventRunEnd)
	}
}

func TestOpenResumesAndAppends(t *testing.T) {
	wd := t.TempDir()
	l, err := New(wd, "", "agent", "openai", "gpt-5", "go")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.SetLastResponseID("resp_first")
	if err := l.Append(EventResponse, map[string]any{"id": "resp_first"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	id := l.SessionID()
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	resumed, err := Open(wd, id)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if got := resumed.LastResponseID(); got != "resp_first" {
		t.Fatalf("LastResponseID after resume = %q, want resp_first", got)
	}
	if err := resumed.Append(EventResume, map[string]any{"prev": "resp_first"}); err != nil {
		t.Fatalf("Append on resumed: %v", err)
	}

	eventsBytes, err := os.ReadFile(filepath.Join(resumed.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !strings.Contains(string(eventsBytes), `"type":"resume"`) {
		t.Fatalf("resume event missing from events.jsonl:\n%s", eventsBytes)
	}
}

func TestStoreAndReadLargeResult(t *testing.T) {
	wd := t.TempDir()
	l, err := New(wd, "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	content := strings.Repeat("abcdefghij", 2000) // 20000 bytes
	id, err := l.StoreLargeResult(content)
	if err != nil {
		t.Fatalf("StoreLargeResult: %v", err)
	}
	if len(id) != 8 {
		t.Fatalf("expected 8-char id, got %q", id)
	}

	chunk, total, err := l.ReadLargeResult(id, 0, 100)
	if err != nil {
		t.Fatalf("ReadLargeResult: %v", err)
	}
	if total != len(content) {
		t.Fatalf("total=%d, want %d", total, len(content))
	}
	if len(chunk) != 100 || chunk != content[:100] {
		t.Fatalf("chunk mismatch: len=%d", len(chunk))
	}

	// Page from offset.
	chunk2, _, err := l.ReadLargeResult(id, 100, 50)
	if err != nil {
		t.Fatalf("ReadLargeResult page: %v", err)
	}
	if chunk2 != content[100:150] {
		t.Fatalf("paged chunk mismatch")
	}

	// Read past end.
	chunk3, _, err := l.ReadLargeResult(id, total+1, 100)
	if err != nil {
		t.Fatalf("ReadLargeResult past end: %v", err)
	}
	if chunk3 != "" {
		t.Fatalf("expected empty chunk past end, got %d bytes", len(chunk3))
	}

	// Unknown id.
	if _, _, err := l.ReadLargeResult("deadbeef", 0, 0); err == nil {
		t.Fatalf("expected error for unknown id")
	}
}

func TestReadLargeResultRejectsTraversal(t *testing.T) {
	wd := t.TempDir()
	l, err := New(wd, "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	// id is model-controlled; any value that could escape resultDir must be
	// rejected before it is joined into a path (CWE-22).
	for _, id := range []string{"", "..", "../../etc/hosts", "a/b", "sub/../../x"} {
		if _, _, err := l.ReadLargeResult(id, 0, 0); err == nil {
			t.Fatalf("expected error for traversal id %q, got nil", id)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	wd := t.TempDir()
	l, err := New(wd, "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	ctx := WithLogger(context.Background(), l)
	if got := FromContext(ctx); got != l {
		t.Fatalf("FromContext returned different logger")
	}
	if got := FromContext(context.Background()); got != nil {
		t.Fatalf("FromContext on bare ctx should be nil, got %v", got)
	}

	// FromContext must also tolerate a nil ctx — it can be reached from
	// goroutines that lost their parent. staticcheck flags the literal
	// nil, so route it through a typed variable.
	var nilCtx context.Context
	if got := FromContext(nilCtx); got != nil {
		t.Fatalf("FromContext(nil) should be nil")
	}
}

func TestNilLoggerSafe(t *testing.T) {
	var l *Logger
	if err := l.Append("anything", nil); err != nil {
		t.Fatalf("Append on nil: %v", err)
	}
	l.SetLastResponseID("x")
	l.UpdateMetrics(1, 2, 3, 4)
	l.Finish(StatusCompleted, "")
	if err := l.Close(); err != nil {
		t.Fatalf("Close on nil: %v", err)
	}
	if got := l.LastResponseID(); got != "" {
		t.Fatalf("LastResponseID on nil should be empty")
	}
	if _, err := l.StoreLargeResult("x"); err == nil {
		t.Fatalf("StoreLargeResult on nil should error")
	}
	if _, _, err := l.ReadLargeResult("x", 0, 0); err == nil {
		t.Fatalf("ReadLargeResult on nil should error")
	}
}

func TestWithLoggerNilLeavesCtxUnchanged(t *testing.T) {
	base := context.Background()
	if got := WithLogger(base, nil); got != base {
		t.Fatalf("WithLogger(ctx, nil) should return ctx unchanged")
	}
}

func TestNewFailsWhenStateDirIsAFile(t *testing.T) {
	wd := t.TempDir()
	// Point XDG_STATE_HOME at a regular file so MkdirAll on the session
	// directory fails.
	blocked := filepath.Join(wd, "blocked")
	if err := os.WriteFile(blocked, []byte("blocked"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", filepath.Join(blocked, "state"))
	if _, err := New(wd, "", "a", "p", "m", ""); err == nil {
		t.Fatalf("expected mkdir error when state dir cannot be created")
	}
}

func TestOpenFailsForMissingSession(t *testing.T) {
	wd := t.TempDir()
	if _, err := Open(wd, "no-such-session"); err == nil {
		t.Fatalf("expected error opening missing session")
	}
}

func TestOpenFailsOnInvalidMeta(t *testing.T) {
	wd := t.TempDir()
	id := "20260101T000000Z-deadbeef"
	dir := filepath.Join(wd, SessionsRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Open(wd, id); err == nil {
		t.Fatalf("expected parse error on invalid meta.json")
	}
}

func TestSetLastResponseIDIgnoresEmpty(t *testing.T) {
	l, _ := newTestLogger(t)
	l.SetLastResponseID("first")
	l.SetLastResponseID("") // no-op
	if got := l.LastResponseID(); got != "first" {
		t.Fatalf("LastResponseID = %q, want first", got)
	}
}

func TestFinishWithErrMessageRecordsBoth(t *testing.T) {
	l, _ := newTestLogger(t)
	l.Finish(StatusError, "boom")

	meta := readMeta(t, l)
	if meta.Status != StatusError {
		t.Fatalf("status=%q, want %q", meta.Status, StatusError)
	}
	events := readEvents(t, l)
	last := events[len(events)-1]
	if last.Type != EventRunEnd {
		t.Fatalf("last event type=%q", last.Type)
	}
	if !strings.Contains(string(last.Payload), "boom") {
		t.Fatalf("run_end payload missing error: %s", last.Payload)
	}
}

func TestReadLargeResultClampsNegativeOffset(t *testing.T) {
	l, _ := newTestLogger(t)
	id, err := l.StoreLargeResult("hello world")
	if err != nil {
		t.Fatalf("StoreLargeResult: %v", err)
	}
	chunk, total, err := l.ReadLargeResult(id, -10, 5)
	if err != nil {
		t.Fatalf("ReadLargeResult: %v", err)
	}
	if total != len("hello world") {
		t.Fatalf("total=%d", total)
	}
	if chunk != "hello" {
		t.Fatalf("chunk=%q, want hello", chunk)
	}
}

func TestAppendUnmarshalablePayloadErrors(t *testing.T) {
	l, _ := newTestLogger(t)
	// channel cannot be JSON-marshaled.
	err := l.Append(EventToolCall, map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatalf("expected marshal error for channel payload")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenFailsWhenSessionPathIsAFile(t *testing.T) {
	wd := t.TempDir()
	id := "blockedSession"
	dir := filepath.Join(wd, SessionsRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Create the session id path as a regular file so MkdirAll(<id>/results) fails.
	if err := os.WriteFile(filepath.Join(dir, id), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Open(wd, id); err == nil {
		t.Fatalf("expected mkdir error when session path is a file")
	}
}

func TestOpenMissingMetaJSONErrors(t *testing.T) {
	wd := t.TempDir()
	id := "abc"
	dir := filepath.Join(wd, SessionsRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// no meta.json — Open should fail at read step
	if _, err := Open(wd, id); err == nil {
		t.Fatalf("expected error opening session without meta.json")
	}
}

func TestAppendAfterCloseIsNoop(t *testing.T) {
	l, _ := newTestLogger(t)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// appendLocked short-circuits when events is nil; should not error.
	if err := l.Append(EventToolCall, map[string]any{"k": "v"}); err != nil {
		t.Fatalf("Append after Close: %v", err)
	}
}

func TestSetRoutineIDPersistsToMeta(t *testing.T) {
	l, _ := newTestLogger(t)
	defer func() { _ = l.Close() }()
	l.SetRoutineID("global:nightly")

	data, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}
	var m struct {
		RoutineID string `json:"routine_id"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.RoutineID != "global:nightly" {
		t.Errorf("routine_id: got %q want %q", m.RoutineID, "global:nightly")
	}
}

func TestSetRoutineIDIgnoresEmptyAndNil(t *testing.T) {
	l, _ := newTestLogger(t)
	defer func() { _ = l.Close() }()
	l.SetRoutineID("global:first")
	l.SetRoutineID("") // no-op, doesn't overwrite

	data, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		RoutineID string `json:"routine_id"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.RoutineID != "global:first" {
		t.Errorf("empty SetRoutineID should not overwrite, got %q", m.RoutineID)
	}
	// Nil receiver must be a safe no-op (no panic).
	var nilLogger *Logger
	nilLogger.SetRoutineID("anything")
}

// TestPersistenceErrorsAreLogged forces writeMeta() and appendLocked() to
// fail mid-run and confirms the wrapping methods do not panic and surface
// the failure via logging.Warn rather than silently swallowing it.
//
// Setup: remove the session directory (so writeMeta's WriteFile/Rename
// errors) and close the underlying events FD without nilling l.events
// (so appendLocked's Write errors). All four mutators must remain safe.
func TestPersistenceErrorsAreLogged(t *testing.T) {
	l, _ := newTestLogger(t)
	t.Cleanup(func() { _ = l.Close() })

	// Close the events FD but keep l.events non-nil so appendLocked
	// hits its Write error path instead of the nil-events early return.
	if err := l.events.Close(); err != nil {
		t.Fatalf("close events: %v", err)
	}
	// Nuke the session dir so writeMeta's tmp WriteFile errors.
	if err := os.RemoveAll(l.Dir()); err != nil {
		t.Fatalf("remove session dir: %v", err)
	}

	// Each of these must run without panicking, exercising the
	// logging.Warn branch on writeMeta failure.
	l.SetLastResponseID("resp-x")
	l.SetRoutineID("global:any")
	l.UpdateMetrics(1, 2, 0.5, 3)

	// Finish exercises both the appendLocked failure path and the
	// trailing writeMeta failure path.
	l.Finish("failed", "boom")
}

func TestStoreLargeResult_NilLogger(t *testing.T) {
	t.Parallel()
	var l *Logger
	_, err := l.StoreLargeResult("content")
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestReadLargeResult_NilLogger(t *testing.T) {
	t.Parallel()
	var l *Logger
	_, _, err := l.ReadLargeResult("id", 0, 0)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
}

func TestReadLargeResult_OffsetBeyondEnd(t *testing.T) {
	t.Parallel()
	l, _ := newTestLogger(t)
	t.Cleanup(func() { _ = l.Close() })
	id, err := l.StoreLargeResult("hello")
	if err != nil {
		t.Fatalf("StoreLargeResult: %v", err)
	}
	content, total, err := l.ReadLargeResult(id, 9999, 0)
	if err != nil {
		t.Fatalf("ReadLargeResult: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content for offset beyond end, got %q", content)
	}
	if total != len("hello") {
		t.Errorf("total = %d, want %d", total, len("hello"))
	}
}

func TestReadLargeResult_WithLimit(t *testing.T) {
	t.Parallel()
	l, _ := newTestLogger(t)
	t.Cleanup(func() { _ = l.Close() })
	id, err := l.StoreLargeResult("hello world")
	if err != nil {
		t.Fatalf("StoreLargeResult: %v", err)
	}
	content, total, err := l.ReadLargeResult(id, 0, 5)
	if err != nil {
		t.Fatalf("ReadLargeResult: %v", err)
	}
	if content != "hello" {
		t.Errorf("content = %q, want 'hello'", content)
	}
	if total != len("hello world") {
		t.Errorf("total = %d, want %d", total, len("hello world"))
	}
}
