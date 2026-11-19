package tools

import (
	"context"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// blockedCall is the call that tripped the loop detector after a repeat
// nudge. Running it again cannot change its result, so while the model keeps
// sending exactly that call it is answered with repeatNudgeMessage instead of
// being run. Otherwise a nudged model that resends the call gets the same
// output back as before, and the nudge costs it nothing: qwen3.8:27b sent one
// identical curl nine times across two nudges and the loop detector then
// ended its run 16 iterations early.
type blockedCall struct {
	key  string // callsKey of the looping call; empty when nothing is blocked
	hits int    // times the model has resent it since it was blocked
}

// callsKey identifies a batch of tool calls by name and arguments, so a batch
// resent unchanged has the same key. It is empty when any call has no
// function call.
func callsKey(calls []llms.ToolCall) string {
	var sb strings.Builder
	for _, call := range calls {
		if call.FunctionCall == nil {
			return ""
		}
		sb.WriteString(call.FunctionCall.Name)
		sb.WriteString(":")
		sb.WriteString(call.FunctionCall.Arguments)
		sb.WriteString("\n")
	}
	return sb.String()
}

// answerBlocked answers calls that resend the blocked call without running
// them, and reports whether it did. A different call lifts the block. A model
// that resends the blocked call loopMaxRepeats times is stuck the same way
// the loop detector means, and the loop stops with ErrLoopDetected.
func (st *loopState) answerBlocked(ctx context.Context, messages []llms.MessageContent, content string, calls []llms.ToolCall) ([]llms.MessageContent, stepResult, bool) {
	if st.blocked.key == "" {
		return messages, stepResult{}, false
	}
	if callsKey(calls) != st.blocked.key {
		st.blocked = blockedCall{}
		return messages, stepResult{}, false
	}
	st.blocked.hits++
	if st.blocked.hits >= loopMaxRepeats {
		logging.InfoContext(ctx, "loop detected: model resent a call it was told will not run %d times, stopping", st.blocked.hits)
		return messages, stepResult{kind: stepStop, err: ErrLoopDetected}, true
	}
	logging.InfoContext(ctx, "model resent the looping call; answering without running it (%d of %d)", st.blocked.hits, loopMaxRepeats-1)
	messages = appendToolCallMessage(messages, content, calls, ctx)
	messages = appendToolResults(messages, calls, repeatNudgeMessage, st.cfg.SplitToolResults)
	return messages, stepResult{kind: stepNext}, true
}
