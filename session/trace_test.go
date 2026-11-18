package session

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// spanContext returns ctx carrying a fixed, valid span context.
func spanContext(t *testing.T, traceHex, spanHex string) context.Context {
	t.Helper()
	tid, err := trace.TraceIDFromHex(traceHex)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := trace.SpanIDFromHex(spanHex)
	if err != nil {
		t.Fatal(err)
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	return trace.ContextWithSpanContext(context.Background(), sc)
}

func readTraceEvents(t *testing.T, l *Logger) []Event {
	t.Helper()
	f, err := os.Open(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		out = append(out, ev)
	}
	return out
}

const (
	rootTrace = "0102030405060708090a0b0c0d0e0f10"
	rootSpan  = "1112131415161718"
	childSpan = "2122232425262728"
)

func TestSetTraceRecordsRootOnce(t *testing.T) {
	l, _ := newTestLogger(t)
	l.SetTrace(context.Background()) // no span: ignored
	if got := readMeta(t, l); got.TraceID != "" {
		t.Fatalf("trace_id = %q before any span", got.TraceID)
	}
	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	l.SetTrace(spanContext(t, "ffffffffffffffffffffffffffffffff", childSpan))
	m := readMeta(t, l)
	if m.TraceID != rootTrace || m.SpanID != rootSpan {
		t.Errorf("meta trace = %s/%s, want the first root %s/%s", m.TraceID, m.SpanID, rootTrace, rootSpan)
	}
}

func TestAppendContextStampsSpan(t *testing.T) {
	l, _ := newTestLogger(t)
	if err := l.Append(EventRunStart, nil); err != nil {
		t.Fatal(err)
	}
	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	if err := l.AppendContext(spanContext(t, rootTrace, childSpan), EventToolCall, map[string]any{"name": "Bash"}); err != nil {
		t.Fatal(err)
	}
	if err := l.AppendContext(context.Background(), EventIteration, nil); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(EventRunEnd, nil); err != nil {
		t.Fatal(err)
	}
	evs := readTraceEvents(t, l)
	if len(evs) != 4 {
		t.Fatalf("events = %d, want 4", len(evs))
	}
	if evs[0].TraceID != "" || evs[0].SpanID != "" {
		t.Errorf("event before SetTrace = %s/%s, want empty", evs[0].TraceID, evs[0].SpanID)
	}
	if evs[1].TraceID != rootTrace || evs[1].SpanID != childSpan {
		t.Errorf("AppendContext event = %s/%s", evs[1].TraceID, evs[1].SpanID)
	}
	for _, ev := range evs[2:] {
		if ev.TraceID != rootTrace || ev.SpanID != "" {
			t.Errorf("%s event = %s/%s, want root trace and no span", ev.Type, ev.TraceID, ev.SpanID)
		}
	}
}

func TestAppendContextNilLogger(t *testing.T) {
	var l *Logger
	if err := l.AppendContext(context.Background(), EventToolCall, nil); err != nil {
		t.Fatal(err)
	}
	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	l.SetClaudeSessionID("x")
}

func TestSetClaudeSessionID(t *testing.T) {
	l, _ := newTestLogger(t)
	l.SetClaudeSessionID("")
	l.SetClaudeSessionID("abc")
	l.SetClaudeSessionID("abc")
	if got := readMeta(t, l).ClaudeSessionID; got != "abc" {
		t.Errorf("claude_session_id = %q", got)
	}
}

func TestOpenKeepsTrace(t *testing.T) {
	l, wd := newTestLogger(t)
	l.SetTrace(spanContext(t, rootTrace, rootSpan))
	id := l.SessionID()
	_ = l.Close()
	reopened, err := Open(wd, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopened.SetTrace(spanContext(t, "ffffffffffffffffffffffffffffffff", childSpan))
	if got := readMeta(t, reopened); got.TraceID != rootTrace {
		t.Errorf("resumed trace_id = %q, want the original %q", got.TraceID, rootTrace)
	}
}
