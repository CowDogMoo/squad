package session

import (
	"context"
	"os"
	"testing"
)

// TestAppendContextWithoutContext checks that an event appended with no
// context still lands, carrying only the session's root trace id.
func TestAppendContextWithoutContext(t *testing.T) {
	l, _ := newTestLogger(t)
	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	var noCtx context.Context
	if err := l.AppendContext(noCtx, EventIteration, nil); err != nil {
		t.Fatal(err)
	}
	evs := readTraceEvents(t, l)
	if len(evs) != 1 || evs[0].TraceID != rootTrace || evs[0].SpanID != "" {
		t.Errorf("events = %+v, want one event with the root trace and no span", evs)
	}
}

// TestMetaWriteFailureKeepsRunGoing checks that when meta.json cannot be
// rewritten, SetTrace and SetClaudeSessionID leave the file as it was instead
// of failing the run, and events still record the run's trace.
func TestMetaWriteFailureKeepsRunGoing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	l, _ := newTestLogger(t)
	before := readMeta(t, l)
	if err := os.Chmod(l.Dir(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(l.Dir(), 0o700) })

	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	l.SetClaudeSessionID("cc-1")

	after := readMeta(t, l)
	if after.TraceID != before.TraceID || after.ClaudeSessionID != before.ClaudeSessionID {
		t.Errorf("meta.json changed despite the failed write: %+v", after)
	}
	if err := l.Append(EventRunEnd, nil); err != nil {
		t.Fatal(err)
	}
	evs := readTraceEvents(t, l)
	if len(evs) != 1 || evs[0].TraceID != rootTrace {
		t.Errorf("events = %+v, want run_end carrying the run's trace", evs)
	}
}
