package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/activity"
	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type claudeEvent struct {
	Ts      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	TraceID string         `json:"trace_id"`
	SpanID  string         `json:"span_id"`
	Payload map[string]any `json:"payload"`
}

// recordClaude installs a recording tracer provider and a session logger,
// returning a ctx under a root span.
func recordClaude(t *testing.T) (context.Context, *tracetest.SpanRecorder, *session.Logger) {
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
	l, err := session.New(t.TempDir(), "", "probe", "claude-code", "", "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	ctx, root := tp.Tracer("test").Start(session.WithLogger(context.Background(), l), "agent.invoke")
	t.Cleanup(func() { root.End() })
	return ctx, rec, l
}

func claudeEvents(t *testing.T, l *session.Logger) []claudeEvent {
	t.Helper()
	f, err := os.Open(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var out []claudeEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev claudeEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func eventTypes(evs []claudeEvent) string {
	types := make([]string, 0, len(evs))
	for _, ev := range evs {
		types = append(types, ev.Type)
	}
	return strings.Join(types, ",")
}

func ts(sec int) time.Time {
	return time.Date(2026, 11, 18, 15, 0, sec, 0, time.UTC)
}

func TestClaudeActivityRecordsTurnsAndTools(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "")
	a.started = ts(0) // the CLI process started before the first message
	a.SessionStarted("cc-session", "claude-haiku-5-5")
	a.ThinkingTokens(12)
	a.ThinkingTokens(40)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), Text: []string{"I'll look."},
		Usage: agenticcli.MessageUsage{InputTokens: 2, CacheReadInputTokens: 100, CacheCreationInputTokens: 50, OutputTokens: 9}})
	a.ThinkingTokens(44) // still the same message
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(2), ToolUses: []agenticcli.ToolUse{
		{ID: "t1", Name: "Bash", Input: json.RawMessage(`{"command":"cat notes.txt"}`)},
	}})
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(2), ToolUses: []agenticcli.ToolUse{
		{ID: "t2", Name: "Read", Input: json.RawMessage(`{"file_path":"/tmp/x"}`)},
	}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "t1", Time: ts(4), Content: "alpha", PermissionDecision: "accept"}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "t2", Time: ts(5), IsError: true}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "unknown", Time: ts(5)}})
	a.Assistant(agenticcli.AssistantMessage{ID: "m2", Time: ts(7), Text: []string{"Done."}, StopReason: "end_turn"})
	a.Finished(ts(8))
	a.close(ts(9)) // idempotent

	if got := readSessionMeta(t, l).ClaudeSessionID; got != "cc-session" {
		t.Errorf("claude_session_id = %q", got)
	}
	t.Run("events", func(t *testing.T) { checkRecordedTurnEvents(t, claudeEvents(t, l)) })
	t.Run("spans", func(t *testing.T) { checkRecordedTurnSpans(t, rec) })
}

// checkRecordedTurnEvents checks the two turns' events, their timestamps and
// the response and error payloads.
func checkRecordedTurnEvents(t *testing.T, evs []claudeEvent) {
	t.Helper()
	want := "tool_call,tool_call,tool_result,tool_result,iteration,response,iteration,response"
	if got := eventTypes(evs); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	for i, start := range map[int]time.Time{4: ts(0), 5: ts(0), 6: ts(5), 7: ts(5)} {
		if !evs[i].Ts.Equal(start) {
			t.Errorf("%s event ts = %v, want the turn's start %v", evs[i].Type, evs[i].Ts, start)
		}
	}
	resp := evs[5].Payload
	if resp["text"] != "I'll look." || resp["thinking_tokens"].(float64) != 44 || resp["cache_read_tokens"].(float64) != 100 || resp["tool_calls"].(float64) != 2 {
		t.Errorf("first response = %v", resp)
	}
	if evs[3].Payload["is_error"] != true || evs[3].Payload["error"] != "tool returned an error" {
		t.Errorf("error tool_result = %v", evs[3].Payload)
	}
	if evs[7].Payload["finish_reason"] != "end_turn" || evs[7].Payload["text"] != "Done." {
		t.Errorf("final response = %v", evs[7].Payload)
	}
}

// checkRecordedTurnSpans checks the two turn spans' times and that both tool
// spans sit under the first turn with the CLI's timestamps.
func checkRecordedTurnSpans(t *testing.T, rec *tracetest.SpanRecorder) {
	t.Helper()
	var turns []sdktrace.ReadOnlySpan
	tools := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		switch {
		case s.Name() == "chat claude-haiku-5-5":
			turns = append(turns, s)
		case strings.HasPrefix(s.Name(), "tool."):
			tools[s.Name()] = s
		}
	}
	if len(turns) != 2 {
		t.Fatalf("turn spans = %d, want 2", len(turns))
	}
	first := turns[0]
	if !first.StartTime().Equal(ts(0)) || !first.EndTime().Equal(ts(5)) {
		t.Errorf("first turn %v..%v, want start at process start and end at the last tool result", first.StartTime(), first.EndTime())
	}
	for name, s := range tools {
		if s.Parent().SpanID() != first.SpanContext().SpanID() {
			t.Errorf("%s not under the first turn", name)
		}
	}
	if bash := tools["tool.Bash"]; !bash.StartTime().Equal(ts(2)) || !bash.EndTime().Equal(ts(4)) {
		t.Errorf("Bash span %v..%v", bash.StartTime(), bash.EndTime())
	}
	if !turns[1].StartTime().Equal(ts(5)) {
		t.Errorf("second turn starts %v, want when the last tool result arrived", turns[1].StartTime())
	}
}

func readSessionMeta(t *testing.T, l *session.Logger) session.Meta {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m session.Meta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestClaudeActivityDenialsAndSubagents(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "sonnet")
	a.started = ts(0)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), ToolUses: []agenticcli.ToolUse{
		{ID: "task", Name: "Task", Input: json.RawMessage(`{"description":"scan"}`)},
		{ID: "w1", Name: "Write", Input: json.RawMessage(`{"file_path":"a"}`)},
		{ID: "w2", Name: "Edit", Input: json.RawMessage(`{"file_path":"b"}`)},
	}})
	a.ToolDenied("w1", "write your plan first")
	a.ToolDenied("missing", "ignored")
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "w2", Time: ts(2), IsError: true, Content: "Permission denied", PermissionDecision: "reject"}})
	// The subagent's own turn nests under the Task tool span.
	a.Assistant(agenticcli.AssistantMessage{ID: "s1", ParentToolUseID: "task", Time: ts(3), Text: []string{"sub"}, ToolUses: []agenticcli.ToolUse{
		{ID: "g1", Name: "Grep", Input: json.RawMessage(`{"pattern":"TODO"}`)},
		{ID: "w3", Name: "Write", Input: json.RawMessage(`{"file_path":"c"}`)},
	}})
	a.ToolResults([]agenticcli.ToolResult{
		{ToolUseID: "g1", ParentToolUseID: "task", Time: ts(4), Content: "x"},
		{ToolUseID: "w3", ParentToolUseID: "task", Time: ts(4), IsError: true, Content: "Permission denied", PermissionDecision: "reject"},
	})
	a.close(ts(6)) // the Task call never returned

	t.Run("events", func(t *testing.T) { checkSubagentEvents(t, claudeEventsByType(t, l)) })
	t.Run("spans", func(t *testing.T) { checkSubagentSpans(t, rec) })
}

// claudeEventsByType groups the session's events by type, in file order.
func claudeEventsByType(t *testing.T, l *session.Logger) map[string][]claudeEvent {
	t.Helper()
	byType := map[string][]claudeEvent{}
	for _, ev := range claudeEvents(t, l) {
		byType[ev.Type] = append(byType[ev.Type], ev)
	}
	return byType
}

// lastPayloadWhere returns the payload of the last event whose payload key
// equals want, or nil.
func lastPayloadWhere(evs []claudeEvent, key string, want any) map[string]any {
	var out map[string]any
	for _, ev := range evs {
		if ev.Payload[key] == want {
			out = ev.Payload
		}
	}
	return out
}

// checkSubagentEvents checks the denials, the parent_tool_use_id tags, the
// unfinished Task result, and the subagent's tool call and response.
func checkSubagentEvents(t *testing.T, byType map[string][]claudeEvent) {
	t.Helper()
	denials := byType[session.EventToolDenied]
	if len(denials) != 3 || denials[0].Payload["reason"] != activity.DeniedSignOff || denials[1].Payload["reason"] != activity.DeniedByPermission {
		t.Fatalf("denials = %+v", denials)
	}
	// Every event of the subagent's own tool calls names the Task call.
	for _, ev := range append(append([]claudeEvent{}, byType[session.EventToolResult]...), denials...) {
		id := ev.Payload["call_id"]
		want := map[any]any{"g1": "task", "w3": "task"}[id]
		if ev.Payload["parent_tool_use_id"] != want {
			t.Errorf("%s %v parent_tool_use_id = %v, want %v", ev.Type, id, ev.Payload["parent_tool_use_id"], want)
		}
	}
	taskResult := lastPayloadWhere(byType[session.EventToolResult], "name", "Task")
	if taskResult == nil || taskResult["error"] != "no tool result before the run ended" {
		t.Errorf("unfinished Task result = %v", taskResult)
	}
	subCall := lastPayloadWhere(byType[session.EventToolCall], "name", "Grep")
	if subCall == nil || subCall["parent_tool_use_id"] != "task" || subCall["iteration"].(float64) != 1 {
		t.Errorf("subagent tool_call = %v", subCall)
	}
	subResp := lastPayloadWhere(byType[session.EventResponse], "parent_tool_use_id", "task")
	if subResp == nil || subResp["duration_ms"].(float64) != 2000 {
		t.Errorf("subagent response = %v, want duration_ms 2000 (from the Task call at 1s to the message at 3s)", subResp)
	}
}

// childSpanNamed returns the last ended span called name whose parent is
// parent, or nil.
func childSpanNamed(rec *tracetest.SpanRecorder, name string, parent sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	var out sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name && s.Parent().SpanID() == parent.SpanContext().SpanID() {
			out = s
		}
	}
	return out
}

// spanDenied reports the squad.tool.denied attribute of s.
func spanDenied(s sdktrace.ReadOnlySpan) bool {
	denied := false
	for _, kv := range s.Attributes() {
		if kv.Key == "squad.tool.denied" {
			denied = kv.Value.AsBool()
		}
	}
	return denied
}

// checkSubagentSpans checks the subagent's turn nests under the Task span
// from the Task call's start, its Grep nests under that turn, and both
// refused writes are marked denied.
func checkSubagentSpans(t *testing.T, rec *tracetest.SpanRecorder) {
	t.Helper()
	spans := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		spans[s.Name()] = s
	}
	subTurn := childSpanNamed(rec, "chat sonnet", spans["tool.Task"])
	if subTurn == nil {
		t.Fatal("subagent turn is not nested under the Task tool span")
	}
	if !subTurn.StartTime().Equal(ts(1)) {
		t.Errorf("subagent turn starts %v, want when the Task call started (%v)", subTurn.StartTime(), ts(1))
	}
	if spans["tool.Grep"].Parent().SpanID() != subTurn.SpanContext().SpanID() {
		t.Error("subagent Grep is not under the subagent turn")
	}
	for _, name := range []string{"tool.Write", "tool.Edit"} {
		if !spanDenied(spans[name]) {
			t.Errorf("%s not marked denied", name)
		}
	}
}

// TestClaudeActivityTaskChildKeepsParentSession checks a claude-code agent
// run as a Task child does not overwrite the parent run's claude_session_id.
func TestClaudeActivityTaskChildKeepsParentSession(t *testing.T) {
	ctx, _, l := recordClaude(t)
	newClaudeActivity(ctx, "").SessionStarted("parent-session", "haiku")
	newClaudeActivity(tools.WithTaskDepth(ctx, 1), "").SessionStarted("child-session", "haiku")
	if got := readSessionMeta(t, l).ClaudeSessionID; got != "parent-session" {
		t.Errorf("claude_session_id = %q, want the parent's", got)
	}
}

func TestDeniedDecisions(t *testing.T) {
	if denied("accept") || denied("") || !denied("reject") || !denied("deny") {
		t.Error("denied() misclassifies decisions")
	}
}

// TestFailedClaudeRunKeepsContentOffSpans runs a fake claude whose tool call
// fails and whose run then fails, and checks that the assistant text, tool
// input and tool output reach events.jsonl but no span.
func TestFailedClaudeRunKeepsContentOffSpans(t *testing.T) {
	stream := `{"type":"system","subtype":"init","session_id":"cc-fail","model":"claude-haiku-5-5"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"Reading TEXT-fixture"},{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cat INPUT-fixture"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"cat: OUTPUT-fixture: denied"}]}}
`
	tests := []struct{ name, stdout string }{
		{"no result event", stream},
		{"error result", stream + `{"type":"result","is_error":true,"num_turns":1,"result":"Credit balance is too low"}` + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, rec, l := recordClaude(t)
			installFailingClaude(t, tt.stdout)

			_, _, err := InvokeModel(ctx, &RunOptions{Provider: "claude-code"}, &agent.Bundle{System: "sys", User: "go", WorkDir: t.TempDir()})
			if err == nil {
				t.Fatal("InvokeModel succeeded, want the CLI failure")
			}

			leaks := []string{"TEXT-fixture", "INPUT-fixture", "OUTPUT-fixture"}
			checkSpansOmit(t, rec, leaks)
			checkSpansFailed(t, rec, "agent.invoke", "tool.Bash")
			checkEventsHold(t, l, leaks)
		})
	}
}

// installFailingClaude puts a fake claude first on PATH that prints stdout,
// writes an error to stderr and exits 1.
func installFailingClaude(t *testing.T, stdout string) {
	t.Helper()
	binDir := t.TempDir()
	out := filepath.Join(binDir, "stdout.jsonl")
	if err := os.WriteFile(out, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat > /dev/null\ncat \"" + out + "\"\necho 'auth expired' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// exportedText returns every string a span would export: attribute values,
// status description, and event names and attributes, one per line.
func exportedText(s sdktrace.ReadOnlySpan) string {
	var exported []string
	for _, kv := range s.Attributes() {
		exported = append(exported, kv.Value.String())
	}
	exported = append(exported, s.Status().Description)
	for _, ev := range s.Events() {
		exported = append(exported, ev.Name)
		for _, kv := range ev.Attributes {
			exported = append(exported, kv.Value.String())
		}
	}
	return strings.Join(exported, "\n")
}

// checkSpansOmit fails if any ended span exports one of leaks.
func checkSpansOmit(t *testing.T, rec *tracetest.SpanRecorder, leaks []string) {
	t.Helper()
	for _, s := range rec.Ended() {
		text := exportedText(s)
		for _, leak := range leaks {
			if strings.Contains(text, leak) {
				t.Errorf("span %s exports %q:\n%s", s.Name(), leak, text)
			}
		}
	}
}

// checkSpansFailed fails unless each named span ended with an error status.
func checkSpansFailed(t *testing.T, rec *tracetest.SpanRecorder, names ...string) {
	t.Helper()
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		byName[s.Name()] = s
	}
	for _, name := range names {
		if s, ok := byName[name]; !ok || s.Status().Code != codes.Error {
			t.Errorf("%s span missing or not marked failed", name)
		}
	}
}

// checkEventsHold fails unless events.jsonl contains each of want.
func checkEventsHold(t *testing.T, l *session.Logger, want []string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range want {
		if !strings.Contains(string(b), w) {
			t.Errorf("events.jsonl is missing %q", w)
		}
	}
}

// TestInvokeAgenticCLIRecordsStream runs a fake claude that streams a
// session and checks the run's events and result.
func TestInvokeAgenticCLIRecordsStream(t *testing.T) {
	ctx, _, l := recordClaude(t)
	fixture, err := filepath.Abs(filepath.Join("..", "agenticcli", "testdata", "claude_print_stream.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\ncat \"" + fixture + "\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	m := metrics.New("claude-code", "")
	resp, m, err := invokeAgenticCLI(ctx, &RunOptions{}, "claude-code", "", "sys", &agent.Bundle{WorkDir: t.TempDir(), User: "go"}, m)
	if err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	if !strings.HasPrefix(resp, "Notes hold alpha") || m.Iterations() != 3 || m.OutputTokens() != 224 {
		t.Errorf("response = %q, iterations = %d, output = %d", resp, m.Iterations(), m.OutputTokens())
	}
	evs := claudeEvents(t, l)
	want := "tool_call,tool_call,tool_result,tool_result,iteration,response,tool_call,tool_denied,iteration,response,iteration,response"
	if got := eventTypes(evs); got != want {
		t.Errorf("events = %s\nwant     %s", got, want)
	}
	if got := readSessionMeta(t, l).ClaudeSessionID; got != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("claude_session_id = %q", got)
	}
}

// TestClaudeTurnCountsSlowToolUseBlocks checks a message whose second
// tool_use block arrives more than 750 ms after the first records both calls,
// and the stop reason and usage from its last block, on the turn's events and
// span.
func TestClaudeTurnCountsSlowToolUseBlocks(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "haiku")
	a.started = time.Now().Add(-time.Second)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: time.Now(), Text: []string{"Running two checks."}, ToolUses: []agenticcli.ToolUse{
		{ID: "t1", Name: "Bash", Input: json.RawMessage(`{"command":"cat notes.txt"}`)},
	}})
	time.Sleep(800 * time.Millisecond)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: time.Now(), StopReason: "tool_use",
		Usage: agenticcli.MessageUsage{InputTokens: 3, OutputTokens: 40}, ToolUses: []agenticcli.ToolUse{
			{ID: "t2", Name: "Read", Input: json.RawMessage(`{"file_path":"x"}`)},
		}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "t1", Time: time.Now()}, {ToolUseID: "t2", Time: time.Now()}})
	a.Assistant(agenticcli.AssistantMessage{ID: "m2", Time: time.Now(), Text: []string{"Done."}, StopReason: "end_turn"})
	a.close(time.Now())

	t.Run("events", func(t *testing.T) { checkSlowToolUseEvents(t, claudeEvents(t, l), a.started.UTC()) })
	t.Run("turn span", func(t *testing.T) { checkFirstTurnToolCalls(t, rec, "chat haiku", 2) })
}

// checkSlowToolUseEvents checks the first turn counts both slow tool_use
// blocks, that ordering by ts keeps each turn before its tool calls, and that
// the response carries the last block's stop reason and usage.
func checkSlowToolUseEvents(t *testing.T, evs []claudeEvent, started time.Time) {
	t.Helper()
	if got := eventTypes(evs); got != "tool_call,tool_call,tool_result,tool_result,iteration,response,iteration,response" {
		t.Fatalf("events = %s", got)
	}
	if n := evs[4].Payload["tool_calls"].(float64); n != 2 {
		t.Errorf("iteration tool_calls = %v, want 2", n)
	}
	byTime := append([]claudeEvent(nil), evs...)
	sort.SliceStable(byTime, func(i, j int) bool { return byTime[i].Ts.Before(byTime[j].Ts) })
	if got := eventTypes(byTime); got != "iteration,response,tool_call,tool_call,tool_result,tool_result,iteration,response" {
		t.Errorf("events by ts = %s, want each turn before its tool calls and the results before the next turn", got)
	}
	if !evs[4].Ts.Equal(started) {
		t.Errorf("iteration ts = %v, want the turn's start %v", evs[4].Ts, started)
	}
	resp := evs[5].Payload
	if resp["tool_calls"].(float64) != 2 || resp["finish_reason"] != "tool_use" || resp["output_tokens"].(float64) != 40 {
		t.Errorf("response = %v, want 2 tool calls with the last block's stop reason and usage", resp)
	}
}

// checkFirstTurnToolCalls checks the earliest span called name records want
// tool calls in squad.turn.tool_calls.
func checkFirstTurnToolCalls(t *testing.T, rec *tracetest.SpanRecorder, name string, want int64) {
	t.Helper()
	var first sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name && (first == nil || s.StartTime().Before(first.StartTime())) {
			first = s
		}
	}
	if first == nil {
		t.Fatal("no turn span")
	}
	for _, kv := range first.Attributes() {
		if kv.Key == "squad.turn.tool_calls" && kv.Value.AsInt64() != want {
			t.Errorf("squad.turn.tool_calls = %d, want %d", kv.Value.AsInt64(), want)
		}
	}
}

// TestClaudeRunWithoutResultEndsOpenCallsAtExit runs a fake claude that
// starts a tool call and then dies with no result event, and checks the
// unfinished call ends when the run ended, not before it began.
func TestClaudeRunWithoutResultEndsOpenCallsAtExit(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	binDir := t.TempDir()
	out := filepath.Join(binDir, "stdout.jsonl")
	stream := `{"type":"system","subtype":"init","session_id":"cc-killed","model":"claude-haiku-5-5"}
{"type":"assistant","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"sleep 600"}}]}}
`
	if err := os.WriteFile(out, []byte(stream), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat > /dev/null\nsleep 1\ncat \"" + out + "\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, _, err := InvokeModel(ctx, &RunOptions{Provider: "claude-code"}, &agent.Bundle{System: "sys", User: "go", WorkDir: t.TempDir()}); err == nil {
		t.Fatal("InvokeModel succeeded, want the CLI failure")
	}

	var result map[string]any
	for _, ev := range claudeEvents(t, l) {
		if ev.Type == session.EventToolResult {
			result = ev.Payload
		}
	}
	if result == nil || result["error"] != "no tool result before the run ended" || result["duration_ms"].(float64) < 0 {
		t.Errorf("tool_result = %v, want the unfinished call with a non-negative duration", result)
	}
	for _, s := range rec.Ended() {
		if s.EndTime().Before(s.StartTime()) {
			t.Errorf("span %s ends %v, before its start %v", s.Name(), s.EndTime(), s.StartTime())
		}
	}
}

// TestClaudeSubagentBeforeFlushNestsUnderTask checks a subagent whose first
// message arrives before its dispatching message was flushed still nests
// under the Task span and starts with the Task call.
func TestClaudeSubagentBeforeFlushNestsUnderTask(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "sonnet")
	a.started = ts(0)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), ToolUses: []agenticcli.ToolUse{
		{ID: "task", Name: "Task", Input: json.RawMessage(`{"description":"scan"}`)},
	}})
	a.Assistant(agenticcli.AssistantMessage{ID: "s1", ParentToolUseID: "task", Time: ts(2), Text: []string{"sub"}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "task", Time: ts(4), Content: "done"}})
	a.close(ts(5))

	var task, subTurn sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "tool.Task" {
			task = s
		}
	}
	if task == nil {
		t.Fatal("no tool.Task span")
	}
	for _, s := range rec.Ended() {
		if s.Name() == "chat sonnet" && s.Parent().SpanID() == task.SpanContext().SpanID() {
			subTurn = s
		}
	}
	if subTurn == nil {
		t.Fatal("subagent turn is not nested under the Task tool span")
	}
	if !subTurn.StartTime().Equal(ts(1)) {
		t.Errorf("subagent turn starts %v, want when the Task call started (%v)", subTurn.StartTime(), ts(1))
	}
	var subResp map[string]any
	for _, ev := range claudeEvents(t, l) {
		if ev.Type == session.EventResponse && ev.Payload["parent_tool_use_id"] == "task" {
			subResp = ev.Payload
		}
	}
	if subResp == nil || subResp["duration_ms"].(float64) != 1000 {
		t.Errorf("subagent response = %v, want duration_ms 1000", subResp)
	}
}

// TestClaudeSubagentLastTurnRecordedAtTaskResult checks a subagent's last
// turn is recorded and its span ended when the Task call's result arrives,
// not at the end of the run.
func TestClaudeSubagentLastTurnRecordedAtTaskResult(t *testing.T) {
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "sonnet")
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), ToolUses: []agenticcli.ToolUse{
		{ID: "task", Name: "Task", Input: json.RawMessage(`{"description":"scan"}`)},
	}})
	a.Assistant(agenticcli.AssistantMessage{ID: "s1", ParentToolUseID: "task", Time: ts(2), Text: []string{"subagent report"}, StopReason: "end_turn"})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "task", Time: ts(4), Content: "subagent report"}})

	var subResp map[string]any
	for _, ev := range claudeEvents(t, l) {
		if ev.Type == session.EventResponse && ev.Payload["parent_tool_use_id"] == "task" {
			subResp = ev.Payload
		}
	}
	if subResp == nil || subResp["text"] != "subagent report" {
		t.Errorf("subagent response before the run ended = %v, want its report", subResp)
	}
	var subTurn sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "chat sonnet" && s.EndTime().Equal(ts(2)) {
			subTurn = s
		}
	}
	if subTurn == nil {
		t.Error("subagent turn span not ended at its last message when the Task result arrived")
	}
	a.close(ts(9))
}
