package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestToolLoopRecordsActivity runs a two-turn readonly loop and checks the
// session events and the span tree: each turn span parents its model call
// and its tool spans, and the refused Write is recorded as a denial.
func TestToolLoopRecordsActivity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	logger, err := session.New(t.TempDir(), "", "probe", "ollama", "qwen", "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	ctx := session.WithLogger(InitReadOnlyMode(context.Background()), logger)

	llm := &scriptedLLM{responses: []*llms.ContentResponse{
		{Choices: []*llms.ContentChoice{{
			Content:          "Checking the notes first.",
			ReasoningContent: "the notes may hold the answer",
			StopReason:       "stop",
			GenerationInfo:   map[string]any{"PromptTokens": 100, "CompletionTokens": 9},
			ToolCalls: []llms.ToolCall{
				{ID: "c1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: `{"command":"cat notes.txt"}`}},
				{ID: "c2", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Write", Arguments: `{"path":"out.txt","content":"x"}`}},
			},
		}}},
		{Choices: []*llms.ContentChoice{{Content: "The notes say alpha.", StopReason: "stop"}}},
	}}
	out, err := RunWithTools(ctx, llm, "sys", "user", t.TempDir(), RunWithToolsConfig{
		Provider:      "ollama",
		Model:         "qwen",
		MaxIterations: 5,
		Executor:      &mockExecutor{responses: map[string]string{"cat notes.txt": "alpha\n"}},
	})
	if err != nil || out != "The notes say alpha." {
		t.Fatalf("RunWithTools = %q, %v", out, err)
	}

	var turns []sdktrace.ReadOnlySpan
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range rec.Ended() {
		if s.Name() == "chat qwen" {
			turns = append(turns, s)
		}
		byName[s.Name()] = s
	}
	if len(turns) != 2 {
		t.Fatalf("turn spans = %d, want 2", len(turns))
	}
	first := turns[0]
	for _, name := range []string{"tool.Bash", "tool.Write"} {
		s, ok := byName[name]
		if !ok {
			t.Fatalf("no %s span", name)
		}
		if s.Parent().SpanID() != first.SpanContext().SpanID() {
			t.Errorf("%s is not a child of the first turn", name)
		}
	}
	generates := 0
	for _, s := range rec.Ended() {
		if s.Name() == "llm.generate" && s.Parent().SpanID() == first.SpanContext().SpanID() {
			generates++
		}
	}
	if generates != 1 {
		t.Errorf("llm.generate spans under the first turn = %d, want 1", generates)
	}
	if loop := byName["tool.loop"]; first.Parent().SpanID() != loop.SpanContext().SpanID() {
		t.Error("turn span is not a child of tool.loop")
	}
	for _, kv := range byName["tool.Write"].Attributes() {
		if kv.Key == "squad.tool.denied" && !kv.Value.AsBool() {
			t.Error("Write span not marked denied")
		}
	}

	f, err := os.Open(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var types []string
	var response map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev struct {
			Type    string         `json:"type"`
			TraceID string         `json:"trace_id"`
			SpanID  string         `json:"span_id"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.TraceID == "" || ev.SpanID == "" {
			t.Errorf("%s event lacks trace/span ids", ev.Type)
		}
		if ev.Type == session.EventResponse && response == nil {
			response = ev.Payload
		}
		types = append(types, ev.Type)
	}
	want := "iteration,response,tool_call,tool_result,tool_call,tool_denied,iteration,response"
	if got := strings.Join(types, ","); got != want {
		t.Errorf("events = %s, want %s", got, want)
	}
	if response["text"] != "Checking the notes first." || response["thinking"] != "the notes may hold the answer" ||
		response["finish_reason"] != "stop" || response["input_tokens"].(float64) != 100 || response["tool_calls"].(float64) != 2 {
		t.Errorf("first response payload = %v", response)
	}
}

// TestTaskChildEventsAreTagged runs a parent loop whose first turn dispatches
// a blocking Task child with its own tool loop, and checks that the child's
// events in the shared events.jsonl name the child agent and the Task call,
// while the parent's events name neither.
func TestTaskChildEventsAreTagged(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger, err := session.New(t.TempDir(), "", "probe", "ollama", "qwen", "go")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	ctx := session.WithLogger(InitReadOnlyMode(context.Background()), logger)
	dir := t.TempDir()
	exec := &mockExecutor{responses: map[string]string{"cat notes.txt": "alpha\n"}}
	child := &scriptedLLM{responses: []*llms.ContentResponse{
		toolCallResp("child-c1", "Bash", `{"command":"cat notes.txt"}`),
		{Choices: []*llms.ContentChoice{{Content: "child done", StopReason: "stop"}}},
	}}
	taskCfg := &TaskConfig{
		WorkingDir: dir,
		CallModel: func(ctx context.Context, _, _, prompt, workDir, _ string) (string, *metrics.Metrics, error) {
			out, err := RunWithTools(ctx, child, "sys", prompt, workDir, RunWithToolsConfig{
				Provider: "ollama", Model: "qwen", MaxIterations: 3, Executor: exec,
			})
			return out, nil, err
		},
	}
	parent := &scriptedLLM{responses: []*llms.ContentResponse{
		toolCallResp("task-1", "Task", `{"agent":"scout","prompt":"read the notes"}`),
		{Choices: []*llms.ContentChoice{{Content: "parent done", StopReason: "stop"}}},
	}}
	out, err := RunWithTools(ctx, parent, "sys", "go", dir, RunWithToolsConfig{
		Provider: "ollama", Model: "qwen", MaxIterations: 3, Executor: exec, TaskCfg: taskCfg,
	})
	if err != nil || out != "parent done" {
		t.Fatalf("RunWithTools = %q, %v", out, err)
	}

	b, err := os.ReadFile(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var ev struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		agent, _ := ev.Payload["agent"].(string)
		parentCall, _ := ev.Payload["parent_tool_use_id"].(string)
		got = append(got, ev.Type+"["+agent+"/"+parentCall+"]")
	}
	want := []string{
		"iteration[/]", "response[/]", "tool_call[/]",
		"iteration[scout/task-1]", "response[scout/task-1]", "tool_call[scout/task-1]", "tool_result[scout/task-1]",
		"iteration[scout/task-1]", "response[scout/task-1]",
		"tool_result[/]", "iteration[/]", "response[/]",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("events =\n%s\nwant\n%s", strings.Join(got, " "), strings.Join(want, " "))
	}
}

func TestTurnResult(t *testing.T) {
	res := turnResult([]*llms.ContentChoice{
		{GenerationInfo: map[string]any{"InputTokens": 5, "CacheReadInputTokens": 2, "OutputTokens": 3, "finish_reason": "tool_use"}},
		nil,
		{ReasoningContent: "a"},
		{ReasoningContent: "b"},
	}, "text", 1)
	if res.FinishReason != "tool_use" || res.Thinking != "a\nb" || res.Usage.InputTokens != 5 || res.Usage.CacheReadTokens != 2 || res.Usage.OutputTokens != 3 {
		t.Errorf("turnResult = %+v", res)
	}
	if empty := turnResult(nil, "", 0); empty.FinishReason != "" || empty.Usage.InputTokens != 0 {
		t.Errorf("turnResult(nil) = %+v", empty)
	}
}
