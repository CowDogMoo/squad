package activity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// setup installs a recording tracer provider and a session logger on ctx.
func setup(t *testing.T) (context.Context, *tracetest.SpanRecorder, *session.Logger) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	l, err := session.New(t.TempDir(), "", "probe", "ollama", "qwen", "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return session.WithLogger(context.Background(), l), rec, l
}

type event struct {
	Type    string         `json:"type"`
	TraceID string         `json:"trace_id"`
	SpanID  string         `json:"span_id"`
	Payload map[string]any `json:"payload"`
}

func events(t *testing.T, l *session.Logger) []event {
	t.Helper()
	f, err := os.Open(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func attrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := map[attribute.Key]attribute.Value{}
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func spanNamed(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("no ended span named %q", name)
	return nil
}

const secretArg = "pass" + "w0rd-fixture"

func TestTurnParentsToolsAndWritesEvents(t *testing.T) {
	t.Setenv(telemetry.ExportCommandsEnv, "")
	t.Setenv(telemetry.ExportTextEnv, "")
	ctx, rec, l := setup(t)
	r := Recorder{Provider: "ollama", Model: "qwen"}
	ctx = WithRecorder(ctx, r)
	if RecorderFrom(ctx) != r {
		t.Fatal("RecorderFrom lost the recorder")
	}

	turn := RecorderFrom(ctx).StartTurn(ctx, 1, time.Time{})
	if TurnIndex(turn.Context()) != 1 || turn.Index() != 1 {
		t.Fatalf("turn index = %d", TurnIndex(turn.Context()))
	}
	text := "I'll log in with the admin password " + secretArg
	turn.Record(TurnResult{
		Text:         text,
		Thinking:     "consider the login form",
		FinishReason: "stop",
		Usage:        Usage{InputTokens: 100, CacheReadTokens: 10, OutputTokens: 7},
		ToolCalls:    2,
	})
	args := `{"command":"curl -u admin:` + secretArg + ` http://lab.test"}`
	_, ok := StartTool(turn.Context(), ToolCall{CallID: "c1", Name: "Bash", Args: args, Summary: "curl -u admin:" + secretArg + " http://lab.test"})
	ok.Finish(Result{OutputBytes: 12})
	_, bad := StartTool(turn.Context(), ToolCall{CallID: "c2", Name: "Write", Args: `{"path":"x"}`})
	bad.Deny(DeniedReadOnly, "error: Write is not permitted in readonly mode")
	bad.Finish(Result{OutputBytes: 1}) // ignored: already ended
	turn.End()

	turnSpan := spanNamed(t, rec, "chat qwen")
	ta := attrs(turnSpan)
	if ta["gen_ai.provider.name"].AsString() != "ollama" || ta["gen_ai.system"].AsString() != "ollama" || ta["gen_ai.operation.name"].AsString() != "chat" {
		t.Errorf("turn provider attrs = %v", ta)
	}
	if ta["gen_ai.usage.input_tokens"].AsInt64() != 100 || ta["gen_ai.usage.output_tokens"].AsInt64() != 7 || ta["squad.turn.thinking_bytes"].AsInt64() != int64(len("consider the login form")) {
		t.Errorf("turn usage attrs = %v", ta)
	}
	if got := ta["gen_ai.response.finish_reasons"].AsStringSlice(); len(got) != 1 || got[0] != "stop" {
		t.Errorf("finish reasons = %v", got)
	}
	if _, has := ta["squad.turn.text"]; has {
		t.Error("assistant text exported without the opt-in")
	}

	var tools []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if strings.HasPrefix(s.Name(), "tool.") {
			tools = append(tools, s)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("tool spans = %d, want 2", len(tools))
	}
	for _, s := range tools {
		if s.Parent().SpanID() != turnSpan.SpanContext().SpanID() {
			t.Errorf("%s parent = %s, want the turn span", s.Name(), s.Parent().SpanID())
		}
		for _, kv := range s.Attributes() {
			if strings.Contains(kv.Value.String(), secretArg) {
				t.Errorf("%s attribute %s leaks the argument: %s", s.Name(), kv.Key, kv.Value.String())
			}
		}
	}
	bash := attrs(spanNamed(t, rec, "tool.Bash"))
	if bash["squad.tool.denied"].AsBool() || bash["squad.tool.output_bytes"].AsInt64() != 12 || bash["squad.tool.args_bytes"].AsInt64() != int64(len(args)) {
		t.Errorf("Bash span attrs = %v", bash)
	}
	write := attrs(spanNamed(t, rec, "tool.Write"))
	if !write["squad.tool.denied"].AsBool() || write["squad.tool.denied_reason"].AsString() != DeniedReadOnly {
		t.Errorf("Write span attrs = %v", write)
	}

	evs := events(t, l)
	var types []string
	for _, ev := range evs {
		types = append(types, ev.Type)
		if ev.TraceID != turnSpan.SpanContext().TraceID().String() || ev.SpanID == "" {
			t.Errorf("%s event trace/span = %s/%s", ev.Type, ev.TraceID, ev.SpanID)
		}
	}
	want := []string{"iteration", "response", "tool_call", "tool_result", "tool_call", "tool_denied"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	if evs[0].SpanID != turnSpan.SpanContext().SpanID().String() {
		t.Error("iteration event must carry the turn span id")
	}
	resp := evs[1].Payload
	if resp["text"] != text || resp["finish_reason"] != "stop" || resp["thinking"] != "consider the login form" || resp["cache_read_tokens"].(float64) != 10 {
		t.Errorf("response payload = %v", resp)
	}
	if evs[2].Payload["args"] != args || evs[2].Payload["iteration"].(float64) != 1 {
		t.Errorf("tool_call payload = %v", evs[2].Payload)
	}
	if evs[3].Payload["is_error"] != false || evs[3].Payload["bytes"].(float64) != 12 {
		t.Errorf("tool_result payload = %v", evs[3].Payload)
	}
	if evs[5].Payload["reason"] != DeniedReadOnly || !strings.Contains(evs[5].Payload["message"].(string), "readonly") {
		t.Errorf("tool_denied payload = %v", evs[5].Payload)
	}
}

func TestOptInExportsRedactedContent(t *testing.T) {
	t.Setenv(telemetry.ExportCommandsEnv, "1")
	t.Setenv(telemetry.ExportTextEnv, "1")
	ctx, rec, _ := setup(t)
	turn := Recorder{Provider: "claude-code", Model: "haiku"}.StartTurn(ctx, 1, time.Time{})
	turn.Record(TurnResult{Text: "trying password=" + secretArg})
	_, tool := StartTool(turn.Context(), ToolCall{CallID: "c", Name: "Bash", Args: "{}", Summary: "mysql --password=" + secretArg})
	tool.Finish(Result{})
	turn.End()

	ta := attrs(spanNamed(t, rec, "chat haiku"))
	if txt := ta["squad.turn.text"].AsString(); txt == "" || strings.Contains(txt, secretArg) {
		t.Errorf("squad.turn.text = %q, want present and redacted", txt)
	}
	ba := attrs(spanNamed(t, rec, "tool.Bash"))
	if a := ba["squad.tool.args"].AsString(); a == "" || strings.Contains(a, secretArg) {
		t.Errorf("squad.tool.args = %q, want present and redacted", a)
	}
}

func TestFailuresAndTimestamps(t *testing.T) {
	ctx, rec, l := setup(t)
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	turn := Recorder{Provider: "claude-code"}.StartTurn(ctx, 3, start)
	turn.Fail("generate", errors.New("connection reset"))
	turn.Fail("generate", nil)
	_, tool := StartTool(turn.Context(), ToolCall{CallID: "t", Name: "Read", Args: "{}", Start: start.Add(time.Second), ParentToolUseID: "toolu_task"})
	tool.Finish(Result{OutputBytes: 3, ErrText: "file not found", End: start.Add(3 * time.Second), Extra: map[string]any{"sent_bytes": 3}})
	turn.RecordAt(TurnResult{ThinkingTokens: 55, ParentToolUseID: "toolu_task"}, start.Add(4*time.Second))
	turn.EndAt(start.Add(5 * time.Second))
	Error(turn.Context(), "cli", errors.New("claude exited"))
	Error(turn.Context(), "cli", nil)
	var nilTool *Tool
	nilTool.Finish(Result{})
	nilTool.Deny("x", "y")

	turnSpan := spanNamed(t, rec, "chat")
	if turnSpan.Status().Code != codes.Error || !turnSpan.StartTime().Equal(start) || !turnSpan.EndTime().Equal(start.Add(5*time.Second)) {
		t.Errorf("turn span status=%v start=%v end=%v", turnSpan.Status(), turnSpan.StartTime(), turnSpan.EndTime())
	}
	if attrs(turnSpan)["squad.turn.thinking_tokens"].AsInt64() != 55 {
		t.Error("thinking tokens missing from the turn span")
	}
	read := spanNamed(t, rec, "tool.Read")
	if read.Status().Code != codes.Error || read.EndTime().Sub(read.StartTime()) != 2*time.Second {
		t.Errorf("tool span status=%v duration=%v", read.Status(), read.EndTime().Sub(read.StartTime()))
	}

	byType := map[string][]event{}
	for _, ev := range events(t, l) {
		byType[ev.Type] = append(byType[ev.Type], ev)
	}
	if errs := byType["error"]; len(errs) != 2 || errs[0].Payload["phase"] != "generate" || errs[0].Payload["iteration"].(float64) != 3 || errs[1].Payload["phase"] != "cli" {
		t.Errorf("error events = %+v", errs)
	}
	res := byType["tool_result"][0].Payload
	if res["is_error"] != true || res["error"] != "file not found" || res["duration_ms"].(float64) != 2000 || res["sent_bytes"].(float64) != 3 {
		t.Errorf("tool_result payload = %v", res)
	}
	if byType["tool_call"][0].Payload["parent_tool_use_id"] != "toolu_task" || byType["iteration"][0].Payload["parent_tool_use_id"] != "toolu_task" {
		t.Error("parent_tool_use_id missing")
	}
	if byType["response"][0].Payload["thinking_tokens"].(float64) != 55 || byType["response"][0].Payload["duration_ms"].(float64) != 4000 {
		t.Errorf("response payload = %v", byType["response"][0].Payload)
	}
}

func TestNoSessionLogger(t *testing.T) {
	turn := Recorder{}.StartTurn(context.Background(), 1, time.Time{})
	turn.Record(TurnResult{Text: "hi"})
	_, tool := StartTool(turn.Context(), ToolCall{Name: "Bash"})
	tool.Finish(Result{})
	turn.End()
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	got := truncate("héllo", 2) // cuts inside é
	if got != "h…[truncated]" {
		t.Errorf("truncate on a rune boundary = %q", got)
	}
}
