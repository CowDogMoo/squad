package tools

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// windowTokens is the tokenizer of the fake models below: one token per four
// bytes of text, tool-call arguments, and tool output, rounded up per message.
func windowTokens(messages []llms.MessageContent) int {
	total := 0
	for _, msg := range messages {
		n := 0
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				n += len(p.Text)
			case llms.ToolCall:
				if p.FunctionCall != nil {
					n += len(p.FunctionCall.Arguments)
				}
			case llms.ToolCallResponse:
				n += len(p.Content)
			}
		}
		total += (n + 3) / 4
	}
	return total
}

// windowLLM is a model with a context window of budget prompt tokens. next
// scripts its replies from the request it is sent.
type windowLLM struct {
	budget   int
	next     func(msgs []llms.MessageContent) *llms.ContentResponse
	captured [][]llms.MessageContent
}

func (w *windowLLM) ContextBudget(...llms.CallOption) (int, func([]llms.MessageContent) int) {
	return w.budget, windowTokens
}

func (w *windowLLM) GenerateContent(_ context.Context, msgs []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	w.captured = append(w.captured, append([]llms.MessageContent(nil), msgs...))
	if w.next == nil {
		return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: "done"}}}, nil
	}
	return w.next(msgs), nil
}

func (w *windowLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func textMessage(role llms.ChatMessageType, text string) llms.MessageContent {
	return llms.MessageContent{Role: role, Parts: []llms.ContentPart{llms.TextPart(text)}}
}

// toolTurn returns one tool-calling turn: the model's call and its output.
func toolTurn(id, output string) []llms.MessageContent {
	return []llms.MessageContent{
		{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.ToolCall{
			ID: id, Type: "function",
			FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: fmt.Sprintf(`{"command":"curl -s http://target/%s"}`, id)},
		}}},
		{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: id, Name: "Bash", Content: output}}},
	}
}

// outputLines returns about size bytes of tool output in 100-byte lines that
// start with label.
func outputLines(label string, size int) string {
	var sb strings.Builder
	for i := 0; sb.Len() < size; i++ {
		line := fmt.Sprintf("%s line %d ", label, i)
		sb.WriteString(line + strings.Repeat("x", 99-len(line)) + "\n")
	}
	return sb.String()
}

// windowConversation returns a system prompt, a task, and turns turns whose
// outputs are outputBytes long.
func windowConversation(turns, outputBytes int) []llms.MessageContent {
	msgs := []llms.MessageContent{
		textMessage(llms.ChatMessageTypeSystem, "SYSTEM PROMPT and AGENT INSTRUCTIONS: map the target."),
		textMessage(llms.ChatMessageTypeHuman, "TASK: map every endpoint."),
	}
	for i := 0; i < turns; i++ {
		msgs = append(msgs, toolTurn(fmt.Sprintf("call-%d", i), outputLines(fmt.Sprintf("turn-%d", i), outputBytes))...)
	}
	return msgs
}

func isStub(msg llms.MessageContent) bool {
	resp, ok := msg.Parts[0].(llms.ToolCallResponse)
	return ok && strings.HasPrefix(resp.Content, "[tool output compacted")
}

func toolOutput(msg llms.MessageContent) string {
	return partOutput(msg.Parts[0])
}

// partOutput returns the content of one tool output part.
func partOutput(part llms.ContentPart) string {
	return part.(llms.ToolCallResponse).Content
}

// TestFitContextWindowKeepsPromptsAndModelMessages checks what compaction to
// the context window may never touch: the system prompt with the agent
// instructions, every user prompt, including the new prompt of a resumed run
// that sits after the replayed turns, and the model's own messages. Only tool
// outputs change, and the caller's conversation is left as it was.
func TestFitContextWindowKeepsPromptsAndModelMessages(t *testing.T) {
	t.Parallel()
	resumed := windowConversation(8, 4000)
	resumed = append(resumed,
		textMessage(llms.ChatMessageTypeAI, "Final answer of the earlier run."),
		textMessage(llms.ChatMessageTypeHuman, "NEW PROMPT: now map the admin area."),
	)
	for i := 0; i < 4; i++ {
		resumed = append(resumed, toolTurn(fmt.Sprintf("new-%d", i), outputLines(fmt.Sprintf("new-%d", i), 4000))...)
	}
	tests := []struct {
		name string
		msgs []llms.MessageContent
	}{
		{"fresh run", windowConversation(12, 4000)},
		{"resumed run", resumed},
		{"resumed run before its first request", resumed[:len(resumed)-8]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			const budget = 4000
			if windowTokens(tt.msgs) <= budget {
				t.Fatal("the conversation must start over budget")
			}
			original := make([]llms.MessageContent, len(tt.msgs))
			for i, m := range tt.msgs {
				original[i] = llms.MessageContent{Role: m.Role, Parts: append([]llms.ContentPart(nil), m.Parts...)}
			}

			out := fitContextWindow(context.Background(), &windowLLM{budget: budget}, tt.msgs, nil)

			if got := windowTokens(out); got > budget {
				t.Fatalf("compacted conversation = %d tokens, over the budget of %d", got, budget)
			}
			if len(out) != len(original) {
				t.Fatalf("compaction changed the number of messages from %d to %d", len(original), len(out))
			}
			stubs := 0
			for i, m := range out {
				if m.Role == llms.ChatMessageTypeTool {
					if isStub(m) {
						stubs++
					}
					continue
				}
				if !reflect.DeepEqual(m, original[i]) {
					t.Fatalf("message %d (%s) was changed: %+v", i, m.Role, m)
				}
			}
			if stubs == 0 {
				t.Fatal("no tool output was compacted")
			}
			if !reflect.DeepEqual(tt.msgs, original) {
				t.Fatal("the caller's conversation was modified")
			}
		})
	}
}

// TestFitContextWindowKeepsEveryOutputThatFits is the regression test for
// over-compaction in a window that holds only a few tool outputs. With 30 KB
// Bash outputs and room for four, the four newest stay whole however long the
// run gets, and only the older ones are replaced.
func TestFitContextWindowKeepsEveryOutputThatFits(t *testing.T) {
	t.Parallel()
	const (
		outputBytes = 30_000
		fits        = 4
	)
	// Room for the instructions, the model's calls, and four whole outputs,
	// but not five.
	budget := fits*outputBytes/4 + 2_000
	for turns := 1; turns <= 24; turns++ {
		t.Run(fmt.Sprintf("%d turns", turns), func(t *testing.T) {
			t.Parallel()
			msgs := windowConversation(turns, outputBytes)
			out := fitContextWindow(context.Background(), &windowLLM{budget: budget}, msgs, nil)
			if got := windowTokens(out); got > budget {
				t.Fatalf("compacted conversation = %d tokens, over the budget of %d", got, budget)
			}
			whole := 0
			for i, m := range out {
				if m.Role != llms.ChatMessageTypeTool {
					continue
				}
				newest := i >= len(out)-2*fits
				switch {
				case toolOutput(m) == toolOutput(msgs[i]):
					whole++
					if !newest {
						t.Fatalf("message %d is whole while newer outputs were replaced", i)
					}
				case !isStub(m):
					t.Fatalf("message %d is neither whole nor the compaction stub: %.80q", i, toolOutput(m))
				case newest:
					t.Fatalf("message %d is one of the %d newest outputs and fits, but was replaced", i, fits)
				}
			}
			if want := min(turns, fits); whole != want {
				t.Fatalf("%d outputs are whole, want %d", whole, want)
			}
		})
	}
}

// TestFitContextWindowShortensLatestOutputs covers a latest turn that does
// not fit even with every older output replaced: its outputs are shortened,
// keeping their start and end, and older outputs are replaced first.
func TestFitContextWindowShortensLatestOutputs(t *testing.T) {
	t.Parallel()
	const budget = 6000
	msgs := windowConversation(3, 4000)
	big := outputLines("big", 200_000)
	small := outputLines("small", 1500)
	msgs = append(msgs,
		llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{
			llms.ToolCall{ID: "a", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"big.log"}`}},
			llms.ToolCall{ID: "b", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: `{"path":"small.log"}`}},
		}},
		llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{
			llms.ToolCallResponse{ToolCallID: "a", Name: "Read", Content: big},
			llms.ToolCallResponse{ToolCallID: "b", Name: "Read", Content: small},
		}},
	)

	out := fitContextWindow(context.Background(), &windowLLM{budget: budget}, msgs, nil)

	if got := windowTokens(out); got > budget {
		t.Fatalf("compacted conversation = %d tokens, over the budget of %d", got, budget)
	}
	for i := 3; i < 8; i += 2 {
		if !isStub(out[i]) {
			t.Fatalf("older output %d must be replaced before the latest turn is shortened", i)
		}
	}
	latest := out[len(out)-1].Parts
	cut := partOutput(latest[0])
	if len(cut) >= len(big) || len(cut) < minKeptLatestOutput {
		t.Fatalf("the oversized output is %d bytes after compaction, want between %d and %d", len(cut), minKeptLatestOutput, len(big))
	}
	if !strings.HasPrefix(cut, "big line 0 ") || !strings.HasSuffix(cut, big[len(big)-100:]) || !strings.Contains(cut, "lines omitted from tool output") {
		t.Fatalf("a shortened output must keep its start and end around a notice, got %.120q ... %.120q", cut, cut[max(0, len(cut)-120):])
	}
	if got := partOutput(latest[1]); got != small {
		t.Fatal("an output that fits must not be shortened")
	}
}

// TestFitContextWindowWarnsWhenPromptsAloneDoNotFit checks the case
// compaction cannot solve: nothing but tool outputs may be changed, so when
// the prompts themselves outgrow the window the conversation is returned with
// them whole and a warning says to raise the window.
func TestFitContextWindowWarnsWhenPromptsAloneDoNotFit(t *testing.T) {
	t.Parallel()
	msgs := windowConversation(2, 4000)
	msgs[0] = textMessage(llms.ChatMessageTypeSystem, strings.Repeat("s", 40_000))
	var logs bytes.Buffer
	ctx := logging.WithLogger(context.Background(), &logging.CustomLogger{LogLevel: slog.LevelInfo, ConsoleWriter: &logs})

	out := fitContextWindow(ctx, &windowLLM{budget: 5000}, msgs, nil)

	if !reflect.DeepEqual(out[0], msgs[0]) || !reflect.DeepEqual(out[1], msgs[1]) {
		t.Fatal("the system prompt and the task must stay whole")
	}
	if !strings.Contains(logs.String(), "raise the model's context window") {
		t.Fatalf("want a warning that the window is too small, got log:\n%s", logs.String())
	}
}

// TestFitContextWindowLeavesOtherModelsAlone checks that a model with no
// context window to report, as every hosted provider's is, gets its
// conversation back untouched however long it is.
func TestFitContextWindowLeavesOtherModelsAlone(t *testing.T) {
	t.Parallel()
	msgs := windowConversation(40, 30_000)
	ctx := InitReadCache(context.Background())
	out := fitContextWindow(ctx, &captureLLM{}, msgs, nil)
	if len(out) != len(msgs) || &out[0] != &msgs[0] {
		t.Fatal("the conversation must be returned as it was given")
	}
	if got := GetReadCache(ctx).CompactionEpoch(); got != 0 {
		t.Fatalf("compaction epoch = %d, want 0", got)
	}
}

// TestFitContextWindowMovesReadCacheEpoch checks that the read cache hears of
// every compaction, and of nothing when the conversation already fits.
func TestFitContextWindowMovesReadCacheEpoch(t *testing.T) {
	t.Parallel()
	ctx := InitReadCache(context.Background())
	rc := GetReadCache(ctx)

	fitContextWindow(ctx, &windowLLM{budget: 1_000_000}, windowConversation(12, 4000), nil)
	if got := rc.CompactionEpoch(); got != 0 {
		t.Fatalf("epoch after a conversation that fits = %d, want 0", got)
	}
	fitContextWindow(ctx, &windowLLM{budget: 4000}, windowConversation(12, 4000), nil)
	if got := rc.CompactionEpoch(); got != 1 {
		t.Fatalf("epoch after older outputs were replaced = %d, want 1", got)
	}
	fitContextWindow(ctx, &windowLLM{budget: 4000}, windowConversation(1, 200_000), nil)
	if got := rc.CompactionEpoch(); got != 2 {
		t.Fatalf("epoch after the latest output was shortened = %d, want 2", got)
	}
}

// windowRead returns a model reply that reads path.
func windowRead(id, path string) *llms.ContentResponse {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{ToolCalls: []llms.ToolCall{{
		ID: id, Type: "function",
		FunctionCall: &llms.FunctionCall{Name: "Read", Arguments: fmt.Sprintf(`{"path":%q}`, path)},
	}}}}}
}

func windowAnswer(text string) *llms.ContentResponse {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: text}}}
}

func lastToolMessage(msgs []llms.MessageContent) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llms.ChatMessageTypeTool {
			return toolOutput(msgs[i])
		}
	}
	return ""
}

func firstToolMessage(msgs []llms.MessageContent) string {
	for _, m := range msgs {
		if m.Role == llms.ChatMessageTypeTool {
			return toolOutput(m)
		}
	}
	return ""
}

// TestToolLoopReadCompactionRereadReturnsFile is the regression test for a
// Read output compacted out of the conversation while the read cache still
// counted it as delivered. Through the real tool loop and Read tool: the
// model reads a.go, keeps reading until the loop has compacted that output to
// fit the window, and reads a.go again. It must get the file, not the
// cache-hit stub or the read-loop notice. One case per compaction step.
func TestToolLoopReadCompactionRereadReturnsFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		budget    int
		fileBytes int
		// compacted reports whether the request no longer carries the first
		// Read of a.go whole.
		compacted func(msgs []llms.MessageContent) bool
	}{
		{
			name:      "older output replaced",
			budget:    6000,
			fileBytes: 3000,
			compacted: func(msgs []llms.MessageContent) bool {
				return strings.HasPrefix(firstToolMessage(msgs), "[tool output compacted")
			},
		},
		{
			name:      "latest output shortened",
			budget:    4000,
			fileBytes: 30_000,
			compacted: func(msgs []llms.MessageContent) bool {
				return strings.Contains(lastToolMessage(msgs), "lines omitted from tool output")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTestFile(t, dir, "a.go", "// A-GO-FILE-MARKER\n"+outputLines("a.go", tt.fileBytes))
			const fillers = 20
			for i := 1; i <= fillers; i++ {
				writeTestFile(t, dir, fmt.Sprintf("filler-%02d.txt", i), outputLines(fmt.Sprintf("filler-%d", i), 8000))
			}

			var (
				calls, read int
				asked       bool
				reread      string
			)
			llm := &windowLLM{budget: tt.budget}
			llm.next = func(msgs []llms.MessageContent) *llms.ContentResponse {
				calls++
				switch {
				case calls == 1:
					return windowRead("first", "a.go")
				case asked:
					if reread == "" {
						reread = lastToolMessage(msgs)
					}
					return windowAnswer("done")
				case tt.compacted(msgs):
					asked = true
					return windowRead("again", "a.go")
				case read < fillers:
					read++
					return windowRead(fmt.Sprintf("filler-%d", read), fmt.Sprintf("filler-%02d.txt", read))
				default:
					return windowAnswer("gave up")
				}
			}

			response, err := RunWithTools(context.Background(), llm, "You fix Go code.", "Fix the bug in a.go.", dir,
				RunWithToolsConfig{MaxIterations: fillers + 10, Executor: &executor.LocalExecutor{WorkingDir: dir}})
			if err != nil {
				t.Fatalf("RunWithTools() error = %v", err)
			}
			if !asked {
				t.Fatalf("the first Read of a.go was never compacted (%d requests, response %q)", calls, response)
			}
			if !strings.Contains(reread, "A-GO-FILE-MARKER") {
				t.Fatalf("reading a.go again after its output was compacted must return the file, got: %.300s", reread)
			}
			for _, stub := range []string{"CACHE HIT", "READ LOOP DETECTED"} {
				if strings.Contains(reread, stub) {
					t.Fatalf("reading a.go again returned a %s answer: %.300s", stub, reread)
				}
			}
			for i, msgs := range llm.captured {
				if got := windowTokens(msgs); got > tt.budget {
					t.Fatalf("request %d = %d tokens, over the budget of %d", i+1, got, tt.budget)
				}
			}
		})
	}
}

// TestToolLoopFitsEveryRequestIntoWindow checks the two requests that no tool
// output precedes: the first request of a resumed run, whose replayed turns
// may already outgrow the window, and the final request made without tools
// once the iteration limit is reached. Both must fit, with the system prompt
// first and the run's own prompt intact.
func TestToolLoopFitsEveryRequestIntoWindow(t *testing.T) {
	t.Parallel()
	const (
		budget     = 5000
		system     = "You map web applications. AGENT INSTRUCTIONS: never lose these."
		newPrompt  = "NEW PROMPT: now map the admin area."
		iterations = 3
	)
	dir := t.TempDir()
	for i := 1; i <= iterations; i++ {
		writeTestFile(t, dir, fmt.Sprintf("page-%d.txt", i), outputLines(fmt.Sprintf("page-%d", i), 8000))
	}
	prior := windowConversation(20, 4000)[1:]
	prior = append(prior, textMessage(llms.ChatMessageTypeAI, "Final answer of the earlier run."))

	reads := 0
	llm := &windowLLM{budget: budget}
	llm.next = func([]llms.MessageContent) *llms.ContentResponse {
		reads++
		if reads > iterations {
			return windowAnswer("done")
		}
		return windowRead(fmt.Sprintf("page-%d", reads), fmt.Sprintf("page-%d.txt", reads))
	}

	if _, err := RunWithTools(context.Background(), llm, system, newPrompt, dir,
		RunWithToolsConfig{MaxIterations: iterations, PriorMessages: prior, Executor: &executor.LocalExecutor{WorkingDir: dir}}); err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}

	if len(llm.captured) != iterations+1 {
		t.Fatalf("requests = %d, want %d iterations and one final request", len(llm.captured), iterations)
	}
	for i, msgs := range llm.captured {
		if got := windowTokens(msgs); got > budget {
			t.Fatalf("request %d = %d tokens, over the budget of %d", i+1, got, budget)
		}
		if !reflect.DeepEqual(msgs[0], textMessage(llms.ChatMessageTypeSystem, system)) {
			t.Fatalf("request %d does not start with the whole system prompt: %+v", i+1, msgs[0])
		}
		kept := false
		for _, m := range msgs {
			if reflect.DeepEqual(m, textMessage(llms.ChatMessageTypeHuman, newPrompt)) {
				kept = true
			}
		}
		if !kept {
			t.Fatalf("request %d lost the resumed run's new prompt", i+1)
		}
	}
	first := llm.captured[0]
	if !reflect.DeepEqual(first[len(first)-1], textMessage(llms.ChatMessageTypeHuman, newPrompt)) {
		t.Fatalf("the first request must end with the new prompt, got %+v", first[len(first)-1])
	}
}
