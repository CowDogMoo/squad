package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/cowdogmoo/squad/logging"
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

	// headroomDivisor sets the share of num_ctx (one tenth) that fitContext
	// leaves unused on top of the reply reserve, to absorb estimate error.
	headroomDivisor = 10

	// nearFullDivisor sets how close to num_ctx (within one twentieth) a
	// prompt_eval_count must come for calibrate to warn.
	nearFullDivisor = 20

	// messageOverheadTokens covers the chat template's per-message framing.
	messageOverheadTokens = 4

	// defaultReplyReserve is the room left for the reply when the request
	// sets no num_predict.
	defaultReplyReserve = 4096

	// minKeptToolBytes is the least of a recent tool output that trimming
	// keeps.
	minKeptToolBytes = 2048

	// trimStep rounds how many older outputs are replaced, and how many old
	// turns are removed, up to a multiple of this. The trimmed prefix then
	// holds steady for several turns instead of shifting every request,
	// which keeps Ollama's prompt cache useful.
	trimStep = 8
)

// trimStats records what fitContext changed, for the log line.
type trimStats struct {
	compacted int // older tool outputs replaced by a placeholder
	dropped   int // oldest messages removed
	truncated int // recent tool outputs cut down
}

func (s trimStats) String() string {
	return fmt.Sprintf("replaced %d older tool outputs, removed %d oldest messages, shortened %d recent tool outputs",
		s.compacted, s.dropped, s.truncated)
}

// fitContext returns the request's messages trimmed so the prompt fits in
// num_ctx with room left for the reply and headroom for estimate error.
// Ollama truncates an over-long prompt from the front, which loses the system
// prompt and agent instructions, so squad trims first and always keeps
// everything before the first assistant turn (the system prompt, agent
// instructions, and task). The caller's messages are never modified.
func (o *LLM) fitContext(ctx context.Context, req chatRequest) []ollamaMessage {
	if o.numCtx <= 0 {
		return req.Messages
	}
	bytesPerToken := o.currentBytesPerToken()
	reserve := replyReserve(req, o.numCtx)
	headroom := o.numCtx / headroomDivisor
	toolTokens := tokensFor(jsonBytes(req.Tools), bytesPerToken)
	budget := o.numCtx - reserve - headroom - toolTokens
	before := estimateTokens(req.Messages, bytesPerToken)
	if before <= budget {
		return req.Messages
	}

	out, stats := trimToBudget(req.Messages, budget, o.numCtx, bytesPerToken)
	after := estimateTokens(out, bytesPerToken)
	logging.InfoContext(ctx, "ollama: request of ~%d tokens exceeds the ~%d-token prompt budget (num_ctx=%d, %d reserved for the reply, %d kept as headroom, ~%d for tool definitions); trimmed to ~%d tokens: %s",
		before, budget, o.numCtx, reserve, headroom, toolTokens, after, stats)
	if after > budget {
		logging.WarnContext(ctx, "ollama: the system prompt, agent instructions, and latest turn still need ~%d tokens, over the ~%d-token budget; raise --num-ctx", after, budget)
	}
	return out
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
// ratio predicts, it lowers the ratio so later requests are trimmed harder.
// The ratio is never raised.
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

// trimToBudget shrinks msgs until their estimate fits budget, in three steps:
//
//  1. Replace older tool outputs with a short placeholder, oldest first.
//  2. Remove the oldest whole turns (an assistant message with its tool
//     results), leaving one note in their place.
//  3. Shorten the latest turn's tool outputs, largest first.
//
// Steps 1 and 2 work in multiples of trimStep. The head (everything before the first assistant message) is never
// touched, and neither is the latest assistant message.
func trimToBudget(msgs []ollamaMessage, budget, numCtx int, bytesPerToken float64) ([]ollamaMessage, trimStats) {
	var stats trimStats
	out := slices.Clone(msgs)
	head := slices.IndexFunc(out, func(m ollamaMessage) bool { return m.Role == "assistant" })
	if head < 0 {
		return out, stats
	}
	tailStart := lastAssistant(out)
	total := estimateTokens(out, bytesPerToken)

	for i := head; i < tailStart && (total > budget || stats.compacted%trimStep != 0); i++ {
		if out[i].Role != "tool" {
			continue
		}
		placeholder := fmt.Sprintf("[older tool output removed by squad to fit num_ctx=%d; it was %d bytes]", numCtx, len(out[i].Content))
		if len(out[i].Content) <= len(placeholder) {
			continue
		}
		old := messageTokens(out[i], bytesPerToken)
		out[i].Content = placeholder
		total += messageTokens(out[i], bytesPerToken) - old
		stats.compacted++
	}

	if total > budget && head < tailStart {
		// Count the note up front (len(out) bounds the number it reports)
		// so adding it cannot push the result back over budget.
		noteCost := messageTokens(droppedNote(len(out), numCtx), bytesPerToken)
		dropEnd, turns := head, 0
		for dropEnd < tailStart && (total+noteCost > budget || turns%trimStep != 0) {
			turns++
			next := dropEnd + 1
			for next < tailStart && out[next].Role != "assistant" {
				next++
			}
			for _, m := range out[dropEnd:next] {
				total -= messageTokens(m, bytesPerToken)
			}
			dropEnd = next
		}
		stats.dropped = dropEnd - head
		note := droppedNote(stats.dropped, numCtx)
		total += messageTokens(note, bytesPerToken)
		out = slices.Concat(out[:head], []ollamaMessage{note}, out[dropEnd:])
		tailStart = head + 1 + (tailStart - dropEnd)
	}

	for total > budget {
		idx := largestToolOutput(out, tailStart)
		if idx < 0 {
			break
		}
		content := out[idx].Content
		excess := int(float64(total-budget) * bytesPerToken)
		keep := max(minKeptToolBytes, len(content)-excess-256)
		old := messageTokens(out[idx], bytesPerToken)
		out[idx].Content = truncateMiddle(content, keep, numCtx)
		total += messageTokens(out[idx], bytesPerToken) - old
		stats.truncated++
	}
	return out, stats
}

// droppedNote is the message that stands in for removed turns.
func droppedNote(dropped, numCtx int) ollamaMessage {
	return ollamaMessage{
		Role: "user",
		Content: fmt.Sprintf("[squad removed the %d oldest messages of this conversation to fit the model's context window (num_ctx=%d). "+
			"Your instructions above still apply; continue the task.]", dropped, numCtx),
	}
}

// lastAssistant returns the index of the last assistant message, or -1.
func lastAssistant(msgs []ollamaMessage) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return i
		}
	}
	return -1
}

// largestToolOutput returns the index of the largest tool message at or
// after from that is long enough to shorten, or -1. The margin over
// minKeptToolBytes guarantees each cut shrinks the message despite the
// marker it adds.
func largestToolOutput(msgs []ollamaMessage, from int) int {
	idx, size := -1, minKeptToolBytes+512
	for i := from; i < len(msgs); i++ {
		if msgs[i].Role == "tool" && len(msgs[i].Content) > size {
			idx, size = i, len(msgs[i].Content)
		}
	}
	return idx
}

// truncateMiddle keeps about keep bytes of s, three quarters from the start
// and one quarter from the end, with a marker where the middle was cut.
func truncateMiddle(s string, keep, numCtx int) string {
	if len(s) <= keep {
		return s
	}
	headLen := runeStart(s, keep*3/4)
	tailLen := len(s) - runeStart(s, len(s)-keep/4)
	removed := len(s) - headLen - tailLen
	var b strings.Builder
	b.WriteString(s[:headLen])
	fmt.Fprintf(&b, "\n[... %d bytes removed by squad to fit num_ctx=%d ...]\n", removed, numCtx)
	b.WriteString(s[len(s)-tailLen:])
	return b.String()
}

// runeStart moves i back to the start of the UTF-8 sequence containing it.
func runeStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
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
