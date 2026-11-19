package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
)

type loggedEvent struct {
	Type    string         `json:"type"`
	TraceID string         `json:"trace_id"`
	Payload map[string]any `json:"payload"`
}

// newEventLogger opens a session logger in a temp state dir and returns a ctx
// carrying it.
func newEventLogger(t *testing.T) (context.Context, *session.Logger) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	l, err := session.New(t.TempDir(), "", "agent", "openai", "gpt-4o", "go")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return session.WithLogger(context.Background(), l), l
}

func loggedEvents(t *testing.T, l *session.Logger) []loggedEvent {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	var out []loggedEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev loggedEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad event line %q: %v", line, err)
		}
		out = append(out, ev)
	}
	return out
}

func eventsOfType(evs []loggedEvent, typ string) []loggedEvent {
	var out []loggedEvent
	for _, ev := range evs {
		if ev.Type == typ {
			out = append(out, ev)
		}
	}
	return out
}

// responsePayload is a minimal Responses API body with the given output items.
func responsePayload(id string, output []map[string]any) map[string]any {
	return map[string]any{
		"id":                  id,
		"object":              "response",
		"created_at":          0,
		"model":               "gpt-4o",
		"status":              "completed",
		"parallel_tool_calls": false,
		"temperature":         0,
		"tool_choice":         "auto",
		"tools":               []any{},
		"top_p":               1,
		"instructions":        "system",
		"metadata":            map[string]any{},
		"output":              output,
	}
}

// TestRunWithToolsFollowUpFailureRecordsError checks that a failed follow-up
// request ends the run with an error event naming the iteration, and that
// the initial response's assistant text is recorded on its response event.
func TestRunWithToolsFollowUpFailureRecordsError(t *testing.T) {
	ctx, l := newEventLogger(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) > 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"bad follow-up","type":"invalid_request_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responsePayload("resp-1", []map[string]any{
			{"id": "msg-1", "type": "message", "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": "Checking the notes first."}}},
			{"id": "fc-1", "type": "function_call", "call_id": "call-1", "name": "Missing", "arguments": "{}"},
		}))
	}))
	defer server.Close()

	dir := t.TempDir()
	_, err := RunWithTools(ctx, "key", server.URL, "gpt-4o", "system", "user", dir, "", "", 0.4, 0, 3, 0,
		nil, nil, nil, &executor.LocalExecutor{WorkingDir: dir}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "follow-up failed at iteration 1") {
		t.Fatalf("RunWithTools error = %v, want a follow-up failure at iteration 1", err)
	}

	evs := loggedEvents(t, l)
	errs := eventsOfType(evs, session.EventError)
	if len(errs) != 1 || errs[0].Payload["phase"] != "follow-up" || errs[0].Payload["iteration"] != float64(1) {
		t.Fatalf("error events = %+v, want one follow-up error at iteration 1", errs)
	}
	if !strings.Contains(errs[0].Payload["error"].(string), "400") {
		t.Errorf("error payload = %v, want the API status", errs[0].Payload["error"])
	}
	resp := eventsOfType(evs, session.EventResponse)
	if len(resp) != 1 || resp[0].Payload["text"] != "Checking the notes first." {
		t.Errorf("response events = %+v, want the initial response's text", resp)
	}
}

// TestRunResponsesToolSpillsLargeResult checks that an oversized tool result
// is stored in the session, replaced by a get_tool_result placeholder, and
// recorded with its full and sent sizes.
func TestRunResponsesToolSpillsLargeResult(t *testing.T) {
	ctx, l := newEventLogger(t)
	big := strings.Repeat("x", session.LargeResultThreshold+100)
	handlers := map[string]tools.Handler{
		"Dump": {Call: func(context.Context, []byte) (string, error) { return big, nil }},
	}
	outputs := executeAndBuildOutputs(ctx, []FunctionCall{{Name: "Dump", CallID: "c1", Arguments: "{}"}}, handlers, 1)
	if len(outputs) != 1 || outputs[0].OfFunctionCallOutput == nil {
		t.Fatalf("outputs = %+v", outputs)
	}
	sent := outputs[0].OfFunctionCallOutput.Output.OfString.Value
	if !strings.Contains(sent, "get_tool_result") || strings.Contains(sent, big) {
		t.Fatalf("output sent to the model = %q, want a placeholder", sent)
	}

	evs := loggedEvents(t, l)
	large := eventsOfType(evs, session.EventLargeResult)
	if len(large) != 1 || large[0].Payload["bytes"] != float64(len(big)) {
		t.Fatalf("large_result events = %+v", large)
	}
	stored, total, err := l.ReadLargeResult(large[0].Payload["result_id"].(string), 0, 0)
	if err != nil || total != len(big) || stored != big {
		t.Errorf("stored result = %d bytes (err %v), want the full output", total, err)
	}
	res := eventsOfType(evs, session.EventToolResult)
	if len(res) != 1 || res[0].Payload["bytes"] != float64(len(big)) || res[0].Payload["sent_bytes"] != float64(len(sent)) {
		t.Errorf("tool_result = %+v, want full bytes and the placeholder's sent_bytes", res)
	}
}
