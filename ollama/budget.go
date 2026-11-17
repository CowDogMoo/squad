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
	// ContextBudget leaves unused on top of the reply reserve, to absorb
	// estimate error.
	headroomDivisor = 10

	// nearFullDivisor sets how close to num_ctx (within one twentieth) a
	// prompt_eval_count must come for calibrate to warn.
	nearFullDivisor = 20

	// messageOverheadTokens covers the chat template's per-message framing.
	messageOverheadTokens = 4

	// defaultReplyReserve is the room left for the reply when the request
	// sets no num_predict.
	defaultReplyReserve = 4096
)

// ContextBudget returns how many prompt tokens the messages of a request made
// with options may hold, and a function that estimates how many tokens
// messages hold. Ollama cuts a prompt that outgrows num_ctx from the front,
// which loses the system prompt and agent instructions, so the agent loop
// compacts its conversation to this budget before each request. The budget
// is num_ctx less the reply reserve, the tool definitions, and a tenth of the
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
	budget := o.numCtx - replyReserve(req, o.numCtx) - o.numCtx/headroomDivisor - tokensFor(jsonBytes(req.Tools), bytesPerToken)
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

// replyReserve returns the tokens to leave free for the model's reply: the
// request's num_predict, or defaultReplyReserve, capped at half of num_ctx.
func replyReserve(req chatRequest, numCtx int) int {
	reserve := defaultReplyReserve
	if np, ok := req.Options["num_predict"].(int); ok && np > 0 {
		reserve = np
	}
	return min(reserve, numCtx/2)
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
