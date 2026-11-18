package ollama

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

const (
	testSystemPrompt = "SYSTEM: you are the webapp-map agent. Never lose these instructions."
	testTask         = "TASK: map every endpoint of the target."
)

// longConversation builds a system prompt, a task, and turns tool-calling
// turns that each return outputBytes of tool output, as llms messages the
// agent loop would send.
func longConversation(turns, outputBytes int) []llms.MessageContent {
	msgs := []llms.MessageContent{
		{Role: llms.ChatMessageTypeSystem, Parts: []llms.ContentPart{llms.TextPart(testSystemPrompt)}},
		{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(testTask)}},
	}
	for i := 0; i < turns; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.ToolCall{
				ID: id, Type: "function",
				FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: fmt.Sprintf(`{"command":"curl -s http://target/page%d"}`, i)},
			}}},
			llms.MessageContent{Role: llms.ChatMessageTypeTool, Parts: []llms.ContentPart{llms.ToolCallResponse{
				ToolCallID: id, Name: "Bash",
				Content: fmt.Sprintf("turn-%d-output:", i) + strings.Repeat("x", outputBytes),
			}}},
		)
	}
	return msgs
}

// TestGenerateContentSendsWholeConversation checks that the client sends the
// conversation it is given, however long: fitting it into num_ctx is the
// agent loop's job, so that the loop's history, its read cache, and the
// request always agree.
func TestGenerateContentSendsWholeConversation(t *testing.T) {
	t.Parallel()
	f, srv := newFakeOllama(t, fakeReply{body: toolOnlyReply})
	msgs := longConversation(400, 3_000)
	if _, err := New(srv.URL, "qwen3", 16384).GenerateContent(context.Background(), msgs, weatherOpts()...); err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	want, err := convertMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	reqs, _ := f.requests()
	sent := reqs[0].Messages
	if len(sent) != len(want) {
		t.Fatalf("request carries %d messages, want all %d", len(sent), len(want))
	}
	for i := range want {
		if sent[i].Role != want[i].Role || sent[i].Content != want[i].Content {
			t.Fatalf("message %d was changed on the way to Ollama", i)
		}
	}
}

// TestContextBudget pins the prompt budget the agent loop compacts to:
// num_ctx less the reply budget, a tenth of the window as headroom, and the
// tool definitions. The reply budget set aside is the num_predict the same
// request sends, so the prompt and the reply always fit the window together.
// 16384 is the least the runner ever passes as max tokens: reserving all of
// it took half of the default window.
func TestContextBudget(t *testing.T) {
	t.Parallel()
	noTools := tokensFor(jsonBytes([]ollamaTool(nil)), defaultBytesPerToken)
	weatherTools := tokensFor(jsonBytes(convertTools(llmsWeatherTools())), defaultBytesPerToken)
	if weatherTools <= noTools {
		t.Fatalf("tool definitions must cost tokens: %d with, %d without", weatherTools, noTools)
	}
	runnerCap := llms.WithMaxTokens(16384)
	tests := []struct {
		name   string
		numCtx int
		think  bool
		opts   []llms.CallOption
		want   int
	}{
		{"the reply gets an eighth of the default window", 32768, false, []llms.CallOption{runnerCap}, 32768 - 4096 - 3276 - noTools},
		{"a quarter with thinking on", 32768, true, []llms.CallOption{runnerCap}, 32768 - 8192 - 3276 - noTools},
		{"an eighth of a larger window", 65536, false, []llms.CallOption{runnerCap}, 65536 - 8192 - 6553 - noTools},
		{"never more than the caller's max tokens", 65536, false, []llms.CallOption{llms.WithMaxTokens(1024)}, 65536 - 1024 - 6553 - noTools},
		{"the share when the caller sets no max tokens", 65536, false, nil, 65536 - 8192 - 6553 - noTools},
		{"tool definitions are counted", 65536, false, weatherOpts(), 65536 - 1024 - 6553 - weatherTools},
		{"a final call keeps them, so its budget matches the loop's", 65536, false, append(weatherOpts(), llms.WithToolChoice("none")), 65536 - 1024 - 6553 - weatherTools},
		{"no num_ctx means no window to keep to", 0, false, weatherOpts(), math.MaxInt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			llm := New("http://x", "qwen3", tt.numCtx, WithThink(tt.think))
			got, _ := llm.ContextBudget(tt.opts...)
			if got != tt.want {
				t.Fatalf("budget = %d, want %d", got, tt.want)
			}
			if tt.numCtx == 0 {
				return
			}
			opts := llms.CallOptions{}
			for _, opt := range tt.opts {
				opt(&opts)
			}
			req := llm.buildChatRequest(nil, opts)
			sent := intOption(req.Options, "num_predict")
			tools := tokensFor(jsonBytes(req.Tools), defaultBytesPerToken)
			if left := tt.numCtx - got - tools - tt.numCtx/headroomDivisor; left != sent {
				t.Fatalf("the budget leaves %d tokens for the reply, but the request asks for num_predict=%d", left, sent)
			}
		})
	}
}

// intOption returns the int request option key, or 0 when it is unset or not
// an int.
func intOption(options map[string]any, key string) int {
	n, _ := options[key].(int)
	return n
}

// llmsWeatherTools returns the tool definitions weatherOpts passes.
func llmsWeatherTools() []llms.Tool {
	opts := llms.CallOptions{}
	for _, opt := range weatherOpts() {
		opt(&opts)
	}
	return opts.Tools
}

// TestContextBudgetEstimate checks the estimate the agent loop plans its
// compaction with: it counts text, tool calls, and tool outputs at the
// client's bytes-per-token ratio, and adds up message by message.
func TestContextBudgetEstimate(t *testing.T) {
	t.Parallel()
	_, estimate := New("http://x", "qwen3", 65536).ContextBudget()

	text := []llms.MessageContent{{Role: llms.ChatMessageTypeHuman, Parts: []llms.ContentPart{llms.TextPart(strings.Repeat("x", 2500))}}}
	if got, want := estimate(text), 1000+messageOverheadTokens; got != want {
		t.Fatalf("2500 bytes of text = %d tokens, want %d", got, want)
	}

	msgs := longConversation(6, 3000)
	sum := 0
	for i := range msgs {
		one := estimate(msgs[i : i+1])
		if one <= 0 {
			t.Fatalf("message %d (%s) is estimated at %d tokens", i, msgs[i].Role, one)
		}
		sum += one
	}
	if got := estimate(msgs); got != sum {
		t.Fatalf("conversation = %d tokens, but its messages add up to %d", got, sum)
	}

	broken := []llms.MessageContent{{Role: llms.ChatMessageTypeAI, Parts: []llms.ContentPart{llms.ToolCall{
		ID: "c", Type: "function", FunctionCall: &llms.FunctionCall{Name: "Bash", Arguments: "not json"},
	}}}}
	if got := estimate(broken); got != 0 {
		t.Fatalf("a conversation the client cannot send = %d tokens, want 0", got)
	}
}

// TestCalibrateOnlyTightens checks the feedback rule: a prompt_eval_count
// higher than the estimate lowers the client's bytes-per-token ratio, and
// nothing raises it again.
func TestCalibrateOnlyTightens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	llm := New("http://x", "qwen3", 65536)
	req := chatRequest{Messages: []ollamaMessage{{Role: "user", Content: strings.Repeat("x", 30_000)}}}
	sent := float64(promptBytes(req))

	llm.calibrate(ctx, req, 5_000)
	if got := llm.currentBytesPerToken(); got != defaultBytesPerToken {
		t.Fatalf("a count below the estimate must not change the ratio, got %v", got)
	}

	llm.calibrate(ctx, req, 15_000)
	if got, want := llm.currentBytesPerToken(), sent/15_000; got != want {
		t.Fatalf("a count above the estimate must lower the ratio to %v, got %v", want, got)
	}

	llm.calibrate(ctx, req, 5_000)
	if got, want := llm.currentBytesPerToken(), sent/15_000; got != want {
		t.Fatalf("the ratio must never rise again, got %v, want %v", got, want)
	}

	short := chatRequest{Messages: []ollamaMessage{{Role: "user", Content: "hi"}}}
	llm.calibrate(ctx, short, calibrationMinTokens-1)
	if got, want := llm.currentBytesPerToken(), sent/15_000; got != want {
		t.Fatalf("a prompt too short to learn from must not change the ratio, got %v, want %v", got, want)
	}

	llm.calibrate(ctx, req, 60_000)
	if got := llm.currentBytesPerToken(); got != minBytesPerToken {
		t.Fatalf("the ratio must stop at %v, got %v", minBytesPerToken, got)
	}
}

// TestCalibrationTightensContextBudget checks that the learned ratio reaches
// what the agent loop sees: once Ollama reports half as many tokens again as
// squad estimated, the same conversation is estimated higher and the same
// tool definitions leave a smaller budget.
func TestCalibrationTightensContextBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	llm := New("http://x", "qwen3", 65536)
	msgs := longConversation(8, 3000)
	chatMsgs, err := convertMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	budgetBefore, estimate := llm.ContextBudget(weatherOpts()...)
	before := estimate(msgs)

	req := llm.buildChatRequest(chatMsgs, llms.CallOptions{MaxTokens: 1024})
	llm.calibrate(ctx, req, before*3/2)

	budgetAfter, estimate := llm.ContextBudget(weatherOpts()...)
	after := estimate(msgs)
	if after < before*14/10 {
		t.Fatalf("estimate after calibration = %d tokens, want about half as many again as %d", after, before)
	}
	if budgetAfter >= budgetBefore {
		t.Fatalf("budget after calibration = %d, want below %d", budgetAfter, budgetBefore)
	}
}

// TestPromptNearNumCtxWarns checks that squad says so when Ollama reports a
// prompt that filled, or nearly filled, the window: that is the sign Ollama
// truncated it.
func TestPromptNearNumCtxWarns(t *testing.T) {
	t.Parallel()
	const numCtx = 8192
	tests := []struct {
		name         string
		promptTokens int
		wantWarning  bool
	}{
		{"full window", numCtx, true},
		{"within a twentieth of the window", numCtx - numCtx/nearFullDivisor, true},
		{"comfortably inside", numCtx / 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reply := fmt.Sprintf(`{"model":"qwen3","message":{"role":"assistant","content":"ok"},"done":true,"done_reason":"stop","prompt_eval_count":%d,"eval_count":5}`, tt.promptTokens)
			_, srv := newFakeOllama(t, fakeReply{body: reply})
			var logs bytes.Buffer
			ctx := logging.WithLogger(context.Background(), &logging.CustomLogger{LogLevel: slog.LevelInfo, ConsoleWriter: &logs})
			if _, err := New(srv.URL, "qwen3", numCtx).GenerateContent(ctx, userMsg("hi")); err != nil {
				t.Fatalf("GenerateContent() error = %v", err)
			}
			warning := fmt.Sprintf("prompt_eval_count=%d is at or near num_ctx=%d", tt.promptTokens, numCtx)
			if got := strings.Contains(logs.String(), warning); got != tt.wantWarning {
				t.Fatalf("warning logged = %v, want %v; log:\n%s", got, tt.wantWarning, logs.String())
			}
		})
	}
}
