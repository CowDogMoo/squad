package ollama

import (
	"context"
	"encoding/json"
	"math"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

const (
	// defaultBytesPerToken is the bytes-per-token ratio a client starts
	// with. It is deliberately low. Squad cannot run the model's tokenizer,
	// and undercounting is the costly direction: Ollama silently drops the
	// oldest tokens of an over-long prompt, which starts with the system
	// prompt. calibrate lowers a client's ratio further when Ollama's own
	// count shows its content is denser.
	defaultBytesPerToken = 2.5

	// minBytesPerToken is the lowest ratio calibrate settles on.
	minBytesPerToken = 1.0

	// calibrationMinTokens is the smallest prompt calibrate learns from.
	// In a shorter one the chat template's fixed framing outweighs the
	// content and would skew the ratio.
	calibrationMinTokens = 1024

	// headroomDivisor sets the share of num_ctx (one tenth) that
	// ContextBudget leaves unused on top of the reply budget, to absorb
	// estimate error.
	headroomDivisor = 10

	// nearFullDivisor sets how close to num_ctx (within one twentieth) a
	// prompt_eval_count must come for calibrate to warn.
	nearFullDivisor = 20

	// messageOverheadTokens covers the chat template's per-message framing.
	messageOverheadTokens = 4

	// replyShareDivisor sets the share of num_ctx (one eighth) that
	// replyBudget gives the reply of a client with thinking off. At the
	// default window of 32768 tokens that is 4096, enough for a long tool
	// call such as a whole-file write, and with the headroom it leaves more
	// than three quarters of the window to the prompt.
	replyShareDivisor = 8

	// thinkingReplyShareDivisor sets that share (one quarter) for a client
	// with thinking on. Reasoning tokens count against num_predict, so the
	// reply gets twice the room: as much again for the reasoning as for the
	// answer or tool call that has to follow it. A smaller share lets a
	// model such as qwen3 spend the whole reply reasoning and stop with
	// done_reason=length before it answers.
	thinkingReplyShareDivisor = 4
)

// replyBudget returns how many tokens the model's reply may take. It is the
// single rule for the reply: buildChatRequest sends it as num_predict, and
// ContextBudget sets the same amount aside, so the prompt squad keeps and the
// reply Ollama may write always fit num_ctx together. The budget is a fixed
// share of num_ctx, which keeps most of the window for the prompt however
// large the caller's cap is, and it never exceeds maxTokens when the caller
// set one. A client with no num_ctx has no window to share out and passes
// maxTokens through.
func (o *LLM) replyBudget(maxTokens int) int {
	if o.numCtx <= 0 {
		return maxTokens
	}
	budget := o.numCtx / replyShareDivisor
	if o.think {
		budget = o.numCtx / thinkingReplyShareDivisor
	}
	if maxTokens > 0 {
		budget = min(budget, maxTokens)
	}
	return budget
}

// ContextBudget returns how many prompt tokens the messages of a request made
// with options may hold, and a function that estimates how many tokens
// messages hold. Ollama cuts a prompt that outgrows num_ctx from the front,
// which loses the system prompt and agent instructions, so the agent loop
// compacts its conversation to this budget before each request. The budget
// is num_ctx less the reply budget, the tool definitions, and a tenth of the
// window as headroom for estimate error. Both results use the client's
// current bytes-per-token ratio, which calibrate keeps in step with the
// prompt_eval_count Ollama reports. A client with no num_ctx has no window to
// keep to and reports the largest budget there is. A conversation the client
// cannot convert is estimated as empty; GenerateContent returns the
// conversion error for it.
func (o *LLM) ContextBudget(options ...llms.CallOption) (int, func([]llms.MessageContent) int) {
	opts := llms.CallOptions{}
	for _, opt := range options {
		opt(&opts)
	}
	bytesPerToken := o.currentBytesPerToken()
	estimate := func(messages []llms.MessageContent) int {
		chatMsgs, err := convertMessages(messages)
		if err != nil {
			return 0
		}
		return estimateTokens(chatMsgs, bytesPerToken)
	}
	if o.numCtx <= 0 {
		return math.MaxInt, estimate
	}
	req := o.buildChatRequest(nil, opts)
	budget := o.numCtx - o.replyBudget(opts.MaxTokens) - o.numCtx/headroomDivisor - tokensFor(jsonBytes(req.Tools), bytesPerToken)
	return budget, estimate
}

// currentBytesPerToken returns the client's current bytes-per-token estimate.
func (o *LLM) currentBytesPerToken() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.bytesPerToken
}

// calibrate checks squad's estimate against promptTokens, the
// prompt_eval_count Ollama reported for req. It warns when the prompt filled,
// or nearly filled, num_ctx, which is where Ollama starts dropping the oldest
// tokens. When the prompt held more tokens than the client's bytes-per-token
// ratio predicts, it lowers the ratio so later estimates run higher and the
// agent loop compacts sooner. The ratio is never raised.
func (o *LLM) calibrate(ctx context.Context, req chatRequest, promptTokens int) {
	if o.numCtx <= 0 || promptTokens <= 0 {
		return
	}
	if promptTokens >= o.numCtx-o.numCtx/nearFullDivisor {
		logging.WarnContext(ctx, "ollama: prompt_eval_count=%d is at or near num_ctx=%d, so Ollama may have dropped the oldest part of the prompt; raise --num-ctx",
			promptTokens, o.numCtx)
	}
	if promptTokens < calibrationMinTokens {
		return
	}
	sent := promptBytes(req)
	observed := max(float64(sent)/float64(promptTokens), minBytesPerToken)

	o.mu.Lock()
	previous := o.bytesPerToken
	if observed < previous {
		o.bytesPerToken = observed
	}
	o.mu.Unlock()

	if observed < previous {
		logging.InfoContext(ctx, "ollama: %d prompt bytes were %d tokens; estimating %.2f bytes per token from now on, down from %.2f",
			sent, promptTokens, observed, previous)
	}
}

// estimateTokens estimates the prompt tokens msgs will occupy.
func estimateTokens(msgs []ollamaMessage, bytesPerToken float64) int {
	total := 0
	for _, m := range msgs {
		total += messageTokens(m, bytesPerToken)
	}
	return total
}

// messageTokens estimates the prompt tokens one message will occupy.
func messageTokens(m ollamaMessage, bytesPerToken float64) int {
	return tokensFor(messageBytes(m), bytesPerToken) + messageOverheadTokens
}

// tokensFor estimates the tokens n bytes of prompt occupy, rounded up.
func tokensFor(n int, bytesPerToken float64) int {
	return int(math.Ceil(float64(n) / bytesPerToken))
}

// promptBytes counts the bytes of req that the token estimate is based on:
// every message plus the tool definitions.
func promptBytes(req chatRequest) int {
	n := jsonBytes(req.Tools)
	for _, m := range req.Messages {
		n += messageBytes(m)
	}
	return n
}

// messageBytes counts the bytes of m that reach the prompt.
func messageBytes(m ollamaMessage) int {
	n := len(m.Content) + len(m.Thinking)
	if len(m.ToolCalls) > 0 {
		n += jsonBytes(m.ToolCalls)
	}
	return n
}

// jsonBytes returns the size of v once serialized.
func jsonBytes(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}
