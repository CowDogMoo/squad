package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/session"
)

// confirmEventPayload mirrors the structured payload written for each
// confirm_resolved event so tests can decode and assert against it.
type confirmEventPayload struct {
	Summary    string   `json:"summary"`
	Options    []string `json:"options"`
	Resolution string   `json:"resolution"`
	Via        string   `json:"via"`
	Error      string   `json:"error,omitempty"`
}

// newSessionForTest creates a fresh on-disk session under tempdir so the
// test can read back events.jsonl. The session is closed via t.Cleanup.
func newSessionForTest(t *testing.T) *session.Logger {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	logger, err := session.New(wd, "", "test", "openai", "gpt-x", "audit")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return logger
}

// readConfirmEvents flushes the logger and returns every confirm_resolved
// payload from events.jsonl in order.
func readConfirmEvents(t *testing.T, logger *session.Logger) []confirmEventPayload {
	t.Helper()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []confirmEventPayload
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Type    string              `json:"type"`
			Payload confirmEventPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		if ev.Type == session.EventConfirmResolved {
			out = append(out, ev.Payload)
		}
	}
	return out
}

func TestConfirm_SessionEvent_TTYYes(t *testing.T) {
	logger := newSessionForTest(t)
	ctx := session.WithLogger(context.Background(), logger)

	rt := &ConfirmRuntime{
		In:    strings.NewReader("yes\n"),
		Out:   &nopWriter{},
		IsTTY: func() bool { return true },
	}
	args, _ := json.Marshal(confirmArgs{Summary: "Approve checkout?"})
	out, err := confirmTool(rt)(ctx, args)
	if err != nil || out != "yes" {
		t.Fatalf("tty yes: out=%q err=%v", out, err)
	}

	events := readConfirmEvents(t, logger)
	if len(events) != 1 || events[0].Via != "tty" || events[0].Resolution != "yes" {
		t.Errorf("event = %+v", events)
	}
}

func TestConfirm_SessionEvent_AutoYes(t *testing.T) {
	logger := newSessionForTest(t)
	ctx := session.WithLogger(context.Background(), logger)

	rt := &ConfirmRuntime{AutoConfirm: AutoConfirmYes}
	args, _ := json.Marshal(confirmArgs{Summary: "Send email?", Options: []string{"send", "skip"}})
	out, err := confirmTool(rt)(ctx, args)
	if err != nil || out != "send" {
		t.Fatalf("auto yes: out=%q err=%v", out, err)
	}

	events := readConfirmEvents(t, logger)
	if len(events) != 1 || events[0].Via != "auto-confirm=yes" || events[0].Resolution != "send" {
		t.Errorf("event = %+v", events)
	}
}

func TestConfirm_SessionEvent_AbortRecordsError(t *testing.T) {
	logger := newSessionForTest(t)
	ctx := session.WithLogger(context.Background(), logger)

	rt := &ConfirmRuntime{} // unset → abort
	args, _ := json.Marshal(confirmArgs{Summary: "Delete prod?"})
	if _, err := confirmTool(rt)(ctx, args); err == nil {
		t.Fatal("expected abort error")
	}

	events := readConfirmEvents(t, logger)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	if !strings.HasPrefix(events[0].Via, "auto-confirm=abort") {
		t.Errorf("via = %q", events[0].Via)
	}
	if events[0].Error == "" {
		t.Errorf("error field should be populated on abort: %+v", events[0])
	}
}

// nopWriter discards writes — the prompt body isn't relevant to event tests.
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
