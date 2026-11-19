package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/activity"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	oairesponses "github.com/openai/openai-go/v3/responses"
)

// httptestNewServerCapturingBody returns a server that records the
// `previous_response_id` field of incoming JSON bodies into outPrev,
// then replies with the supplied payload. Used by tests that need to
// confirm RunWithTools wired resumeResponseID through to the API.
func httptestNewServerCapturingBody(t *testing.T, outPrev *string, payload map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		var parsed struct {
			PreviousResponseID string `json:"previous_response_id"`
		}
		_ = json.Unmarshal(body, &parsed)
		*outPrev = parsed.PreviousResponseID
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func TestFormatLargeResultPlaceholder(t *testing.T) {
	out := formatLargeResultPlaceholder("abcd1234", "Read", 12345)
	for _, want := range []string{"abcd1234", "Read", "12345", "get_tool_result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("placeholder %q missing %q", out, want)
		}
	}
}

func TestRecordResponseNilSafe(t *testing.T) {
	// nil logger and nil resp should both no-op.
	recordResponse(context.Background(), nil, "label")
	recordResponse(context.Background(), &oairesponses.Response{ID: "x"}, "label")

	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	l, err := session.New(wd, "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	recordResponse(session.WithLogger(context.Background(), l), nil, "label")
}

func TestRecordResponseSetsLastIDAndAppends(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	wd := t.TempDir()
	l, err := session.New(wd, "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	resp := &oairesponses.Response{ID: "resp_xyz"}
	recordResponse(session.WithLogger(context.Background(), l), resp, "initial")

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

// TestRunWithToolsForwardsResumeResponseID exercises the
// PreviousResponseID branch when resumeResponseID is non-empty.
func TestRunWithToolsForwardsResumeResponseID(t *testing.T) {
	payload := map[string]any{
		"id": "resp-final", "object": "response", "created_at": 0,
		"model": "gpt-4o", "parallel_tool_calls": false,
		"temperature": 0, "tool_choice": "auto", "tools": []any{},
		"top_p":              1,
		"error":              map[string]any{"code": "server_error", "message": ""},
		"incomplete_details": map[string]any{"reason": ""},
		"instructions":       "system",
		"metadata":           map[string]any{},
		"output": []map[string]any{
			{
				"id": "msg-1", "type": "message", "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": "hi"}},
			},
		},
	}
	var seenPrev string
	server := httptestNewServerCapturingBody(t, &seenPrev, payload)
	defer server.Close()

	td := t.TempDir()
	out, err := RunWithTools(
		context.Background(),
		"key", server.URL, "gpt-4o", "system", "user", td,
		"", "resp_prior", 0.4, 0, 1, 0, nil, nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("RunWithTools: %v", err)
	}
	if seenPrev != "resp_prior" {
		t.Fatalf("previous_response_id = %q, want resp_prior", seenPrev)
	}
	if out != "hi" {
		t.Fatalf("response = %q, want hi", out)
	}
}

func TestCheckRepeat_NotExceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var repeat tools.RepeatTracker
	calls := []FunctionCall{
		{Name: "Read", Arguments: `{"path":"a.go"}`},
		{Name: "Write", Arguments: `{"path":"b.go"}`},
	}
	if checkRepeat(ctx, &repeat, calls) {
		t.Error("expected checkRepeat to return false for non-repeating calls")
	}
}

func TestCheckRepeat_Exceeded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var repeat tools.RepeatTracker
	// Repeat the same call enough times to exceed the threshold.
	calls := []FunctionCall{{Name: "Read", Arguments: `{"path":"a.go"}`}}
	exceeded := false
	for i := 0; i < 20; i++ {
		if checkRepeat(ctx, &repeat, calls) {
			exceeded = true
			break
		}
	}
	if !exceeded {
		t.Error("expected checkRepeat to return true after repeated identical calls")
	}
}

// TestExecuteAndBuildOutputsTagsLoopIteration checks tool events carry the
// Responses loop's own iteration, not a turn index inherited through ctx
// from a parent agent that dispatched this run as a Task child.
func TestExecuteAndBuildOutputsTagsLoopIteration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	l, err := session.New(t.TempDir(), "", "a", "p", "m", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	parentTurn := activity.Recorder{Provider: "ollama", Model: "qwen"}.StartTurn(session.WithLogger(context.Background(), l), 5, time.Time{})
	defer parentTurn.End()
	handlers := map[string]tools.Handler{
		"Echo": {Call: func(context.Context, []byte) (string, error) { return "ok", nil }},
	}
	executeAndBuildOutputs(parentTurn.Context(), []FunctionCall{{Name: "Echo", CallID: "c1", Arguments: "{}"}}, handlers, 2)

	data, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	seen := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != session.EventToolCall && ev.Type != session.EventToolResult {
			continue
		}
		seen++
		if ev.Payload["iteration"] != float64(2) {
			t.Errorf("%s iteration = %v, want the loop's 2", ev.Type, ev.Payload["iteration"])
		}
	}
	if seen != 2 {
		t.Errorf("tool events = %d, want tool_call and tool_result", seen)
	}
}
