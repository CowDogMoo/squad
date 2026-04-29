package responses

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/session"
	oairesponses "github.com/openai/openai-go/v3/responses"
)

func TestFormatLargeResultPlaceholder(t *testing.T) {
	out := formatLargeResultPlaceholder("abcd1234", "Read", 12345)
	for _, want := range []string{"abcd1234", "Read", "12345", "get_tool_result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("placeholder %q missing %q", out, want)
		}
	}
}

func TestLogEventNilLoggerNoop(t *testing.T) {
	// Must not panic and must not error on nil logger.
	logEvent(nil, session.EventToolCall, map[string]any{"name": "Read"})
}

func TestLogEventWritesToSession(t *testing.T) {
	wd := t.TempDir()
	l, err := session.New(wd, "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	logEvent(l, session.EventToolCall, map[string]any{"name": "Read"})

	data, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if !strings.Contains(string(data), `"type":"tool_call"`) {
		t.Fatalf("expected tool_call event in events.jsonl: %s", data)
	}
}

func TestRecordResponseNilSafe(t *testing.T) {
	// nil logger and nil resp should both no-op.
	recordResponse(nil, nil, "label")
	recordResponse(nil, &oairesponses.Response{ID: "x"}, "label")

	wd := t.TempDir()
	l, err := session.New(wd, "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	recordResponse(l, nil, "label")
}

func TestRecordResponseSetsLastIDAndAppends(t *testing.T) {
	wd := t.TempDir()
	l, err := session.New(wd, "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	resp := &oairesponses.Response{ID: "resp_xyz"}
	recordResponse(l, resp, "initial")

	if got := l.LastResponseID(); got != "resp_xyz" {
		t.Fatalf("LastResponseID = %q, want resp_xyz", got)
	}

	data, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var saw bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev session.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == session.EventResponse && strings.Contains(string(ev.Payload), `"resp_xyz"`) {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("expected response event referencing resp_xyz")
	}
}

func TestLogAPIErrorNilNoop(t *testing.T) {
	logAPIError(context.Background(), nil, "label") // must not panic
}

func TestLogAPIErrorClassifies(t *testing.T) {
	// Known status codes hit dedicated branches; default falls through.
	cases := []int{429, 500, 503, 401, 418}
	for _, code := range cases {
		err := &oairesponses.Error{StatusCode: code, Message: "x", Code: "y"}
		logAPIError(context.Background(), err, "label")
	}
}

func TestLogAPIErrorContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	logAPIError(ctx, errors.New("transport"), "label") // hits ctx.Err() branch
}

func TestLogAPIErrorPlainError(t *testing.T) {
	logAPIError(context.Background(), errors.New("boom"), "label") // hits final branch
}

func TestExtractFunctionCallsHandlesNilAndEmpty(t *testing.T) {
	if calls := ExtractFunctionCalls(nil); calls != nil {
		t.Fatalf("nil resp should return nil calls")
	}
	if calls := ExtractFunctionCalls(&oairesponses.Response{}); calls != nil {
		t.Fatalf("empty resp should return nil calls")
	}
}
