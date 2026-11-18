package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

	if got := readMetaClaudeSession(t, l); got != "cc-session" {
		t.Errorf("claude_session_id = %q", got)
	}
	evs := claudeEvents(t, l)
	want := "iteration,response,tool_call,tool_call,tool_result,tool_result,iteration,response"
	if got := eventTypes(evs); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	resp := evs[1].Payload
	if resp["text"] != "I'll look." || resp["thinking_tokens"].(float64) != 44 || resp["cache_read_tokens"].(float64) != 100 || resp["tool_calls"].(float64) != 2 {
		t.Errorf("first response = %v", resp)
	}
	if evs[5].Payload["is_error"] != true || evs[5].Payload["error"] != "tool returned an error" {
		t.Errorf("error tool_result = %v", evs[5].Payload)
	}
	if evs[7].Payload["finish_reason"] != "end_turn" || evs[7].Payload["text"] != "Done." {
		t.Errorf("final response = %v", evs[7].Payload)
	}

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

func readMetaClaudeSession(t *testing.T, l *session.Logger) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(l.Dir(), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m session.Meta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m.ClaudeSessionID
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
	}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "g1", ParentToolUseID: "task", Time: ts(4), Content: "x"}})
	a.close(ts(6)) // the Task call never returned

	byType := map[string][]claudeEvent{}
	for _, ev := range claudeEvents(t, l) {
		byType[ev.Type] = append(byType[ev.Type], ev)
	}
	denials := byType[session.EventToolDenied]
	if len(denials) != 2 || denials[0].Payload["reason"] != activity.DeniedSignOff || denials[1].Payload["reason"] != activity.DeniedByPermission {
		t.Fatalf("denials = %+v", denials)
	}
	var taskResult map[string]any
	for _, ev := range byType[session.EventToolResult] {
		if ev.Payload["name"] == "Task" {
			taskResult = ev.Payload
		}
	}
	if taskResult == nil || taskResult["error"] != "no tool result before the run ended" {
		t.Errorf("unfinished Task result = %v", taskResult)
	}
	var subCall map[string]any
	for _, ev := range byType[session.EventToolCall] {
		if ev.Payload["name"] == "Grep" {
			subCall = ev.Payload
		}
	}
	if subCall == nil || subCall["parent_tool_use_id"] != "task" || subCall["iteration"].(float64) != 1 {
		t.Errorf("subagent tool_call = %v", subCall)
	}
	var subResp map[string]any
	for _, ev := range byType[session.EventResponse] {
		if ev.Payload["parent_tool_use_id"] == "task" {
			subResp = ev.Payload
		}
	}
	if subResp == nil || subResp["duration_ms"].(float64) != 2000 {
		t.Errorf("subagent response = %v, want duration_ms 2000 (from the Task call at 1s to the message at 3s)", subResp)
	}

	spans := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		spans[s.Name()] = s
	}
	task := spans["tool.Task"]
	var subTurn sdktrace.ReadOnlySpan
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
	if spans["tool.Grep"].Parent().SpanID() != subTurn.SpanContext().SpanID() {
		t.Error("subagent Grep is not under the subagent turn")
	}
	for _, name := range []string{"tool.Write", "tool.Edit"} {
		denied := false
		for _, kv := range spans[name].Attributes() {
			if kv.Key == "squad.tool.denied" {
				denied = kv.Value.AsBool()
			}
		}
		if !denied {
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
	if got := readMetaClaudeSession(t, l); got != "parent-session" {
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
			binDir := t.TempDir()
			out := filepath.Join(binDir, "stdout.jsonl")
			if err := os.WriteFile(out, []byte(tt.stdout), 0o600); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\ncat > /dev/null\ncat \"" + out + "\"\necho 'auth expired' >&2\nexit 1\n"
			if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			_, _, err := InvokeModel(ctx, &RunOptions{Provider: "claude-code"}, &agent.Bundle{System: "sys", User: "go", WorkDir: t.TempDir()})
			if err == nil {
				t.Fatal("InvokeModel succeeded, want the CLI failure")
			}

			leaks := []string{"TEXT-fixture", "INPUT-fixture", "OUTPUT-fixture"}
			names := map[string]sdktrace.ReadOnlySpan{}
			for _, s := range rec.Ended() {
				names[s.Name()] = s
				var exported []string
				for _, kv := range s.Attributes() {
					exported = append(exported, kv.Value.Emit())
				}
				exported = append(exported, s.Status().Description)
				for _, ev := range s.Events() {
					exported = append(exported, ev.Name)
					for _, kv := range ev.Attributes {
						exported = append(exported, kv.Value.Emit())
					}
				}
				for _, leak := range leaks {
					if text := strings.Join(exported, "\n"); strings.Contains(text, leak) {
						t.Errorf("span %s exports %q:\n%s", s.Name(), leak, text)
					}
				}
			}
			for _, name := range []string{"agent.invoke", "tool.Bash"} {
				if s, ok := names[name]; !ok || s.Status().Code != codes.Error {
					t.Errorf("%s span missing or not marked failed", name)
				}
			}

			b, err := os.ReadFile(filepath.Join(l.Dir(), "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range leaks {
				if !strings.Contains(string(b), want) {
					t.Errorf("events.jsonl is missing %q", want)
				}
			}
		})
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
	want := "iteration,response,tool_call,tool_call,tool_result,tool_result,iteration,response,tool_call,tool_denied,iteration,response"
	if got := eventTypes(evs); got != want {
		t.Errorf("events = %s\nwant     %s", got, want)
	}
	if got := readMetaClaudeSession(t, l); got != "11111111-2222-4333-8444-555555555555" {
		t.Errorf("claude_session_id = %q", got)
	}
}

// TestClaudeActivityFlushesAfterQuietPeriod checks a tool call reaches
// events.jsonl while the tool is still running, not only once its result
// arrives.
func TestClaudeActivityFlushesAfterQuietPeriod(t *testing.T) {
	prev := claudeFlushDelay
	claudeFlushDelay = 10 * time.Millisecond
	t.Cleanup(func() { claudeFlushDelay = prev })
	ctx, _, l := recordClaude(t)
	a := newClaudeActivity(ctx, "haiku")
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: time.Now(), Text: []string{"Running the scan."}, ToolUses: []agenticcli.ToolUse{
		{ID: "t1", Name: "Bash", Input: json.RawMessage(`{"command":"sleep 600"}`)},
	}})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := eventTypes(claudeEvents(t, l)); got == "iteration,response,tool_call" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("events = %q, want the tool call recorded before its result", eventTypes(claudeEvents(t, l)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A late block of the same message still starts its tool call.
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: time.Now(), ToolUses: []agenticcli.ToolUse{
		{ID: "t2", Name: "Read", Input: json.RawMessage(`{"file_path":"x"}`)},
	}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "t1", Time: time.Now()}, {ToolUseID: "t2", Time: time.Now()}})
	a.close(time.Now())
	if got := eventTypes(claudeEvents(t, l)); got != "iteration,response,tool_call,tool_call,tool_result,tool_result" {
		t.Errorf("events = %s", got)
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
	prev := claudeFlushDelay
	claudeFlushDelay = time.Hour
	t.Cleanup(func() { claudeFlushDelay = prev })
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
