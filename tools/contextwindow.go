package tools

import (
	"context"
	"slices"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// ContextBudgeter is implemented by a model whose provider cuts a prompt that
// outgrows a fixed context window, as Ollama does at num_ctx. What such a
// provider cuts first is the system prompt and agent instructions, so the
// tool loop compacts its conversation to the model's budget before every
// request. Models that do not implement it are compacted by the token
// thresholds alone.
type ContextBudgeter interface {
	// ContextBudget returns how many prompt tokens the messages of a request
	// made with options may hold, once room is set aside for the reply, the
	// tool definitions, and estimate error. estimate returns how many tokens
	// messages hold, and adds up over them: the estimate of a conversation
	// is the sum of the estimates of its messages.
	ContextBudget(options ...llms.CallOption) (budget int, estimate func([]llms.MessageContent) int)
}

const (
	// minKeptLatestOutput is the least of a latest-turn tool output that
	// fitContextWindow keeps.
	minKeptLatestOutput = 2048

	// latestOutputCutMargin is how much longer than minKeptLatestOutput a
	// latest-turn tool output must be to be shortened, and how much is taken
	// off the size aimed for. It covers the notice a shortened output
	// carries, so every cut leaves the output smaller.
	latestOutputCutMargin = 512
)

// fitContextWindow returns messages compacted so that the request about to be
// made with callOpts fits the model's context window. It returns messages
// unchanged for a model that is not a [ContextBudgeter], and when they fit.
//
// It changes tool outputs only, in two steps, and stops as soon as the
// conversation fits:
//
//  1. Older tool outputs are replaced by the compaction stub, oldest first
//     and only as many as the budget needs, so every recent output that fits
//     stays whole.
//  2. The latest turn's tool outputs are shortened, largest first, keeping
//     the start and end of each.
//
// The system prompt, the agent instructions, every user prompt (a resumed
// run's new one included), and the model's own messages are never changed.
// When those alone outgrow the budget the request is sent over budget, with
// a warning. The caller's messages are never modified. After any change the
// read cache's compaction epoch moves on, so a file whose Read output was
// compacted is returned again on the next Read.
func fitContextWindow(ctx context.Context, llm llms.Model, messages []llms.MessageContent, callOpts []llms.CallOption) []llms.MessageContent {
	budgeter, ok := llm.(ContextBudgeter)
	if !ok {
		return messages
	}
	budget, estimate := budgeter.ContextBudget(callOpts...)
	before := estimate(messages)
	if before <= budget {
		return messages
	}

	out := slices.Clone(messages)
	total := before
	latest := lastAIMessage(out)

	replaced := 0
	for i := 0; i < latest && total > budget; i++ {
		if out[i].Role != llms.ChatMessageTypeTool {
			continue
		}
		parts, n := stubToolOutputs(out[i].Parts)
		if n == 0 {
			continue
		}
		old := estimate(out[i : i+1])
		out[i].Parts = parts
		total += estimate(out[i:i+1]) - old
		replaced += n
	}

	shortened := 0
	for total > budget {
		i, j := largestToolOutput(out, latest+1)
		if i < 0 {
			break
		}
		resp := out[i].Parts[j].(llms.ToolCallResponse)
		old := estimate(out[i : i+1])
		// Aim for the share of the message that the budget has room for.
		keep := minKeptLatestOutput
		if spare := old - (total - budget); spare > 0 {
			keep = max(keep, len(resp.Content)*spare/old-latestOutputCutMargin)
		}
		resp.Content = TruncateToolOutputHeadTail(resp.Content, keep)
		parts := slices.Clone(out[i].Parts)
		parts[j] = resp
		out[i].Parts = parts
		total += estimate(out[i:i+1]) - old
		shortened++
	}

	if replaced+shortened > 0 {
		GetReadCache(ctx).BumpCompactionEpoch()
		logging.InfoContext(ctx, "context window: request of ~%d tokens exceeds the ~%d-token prompt budget; compacted to ~%d tokens (replaced %d older tool outputs, shortened %d latest tool outputs)",
			before, budget, total, replaced, shortened)
	}
	if total > budget {
		logging.WarnContext(ctx, "context window: the prompts, the model's own messages, and the latest turn still need ~%d tokens, over the ~%d-token prompt budget; raise the model's context window (--num-ctx for Ollama)",
			total, budget)
	}
	return out
}

// lastAIMessage returns the index of the last assistant message, or -1.
func lastAIMessage(messages []llms.MessageContent) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llms.ChatMessageTypeAI {
			return i
		}
	}
	return -1
}

// largestToolOutput returns the message and part index of the largest tool
// output at or after from that is long enough to shorten, or -1, -1.
func largestToolOutput(messages []llms.MessageContent, from int) (int, int) {
	msg, part, size := -1, -1, minKeptLatestOutput+latestOutputCutMargin
	for i := from; i < len(messages); i++ {
		if messages[i].Role != llms.ChatMessageTypeTool {
			continue
		}
		for j, p := range messages[i].Parts {
			if resp, ok := p.(llms.ToolCallResponse); ok && len(resp.Content) > size {
				msg, part, size = i, j, len(resp.Content)
			}
		}
	}
	return msg, part
}
