package ollama

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

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

// assertWellFormed checks the invariants every trimmed request must keep:
// instructions first and intact, and no tool result without a preceding
// assistant turn.
func assertWellFormed(t *testing.T, msgs []ollamaMessage) {
	t.Helper()
	if len(msgs) < 2 || msgs[0].Role != "system" || msgs[0].Content != testSystemPrompt {
		t.Fatalf("system prompt must stay first and intact, got %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != testTask {
		t.Fatalf("task must stay second and intact, got %+v", msgs[1])
	}
	for i, m := range msgs {
		if m.Role != "tool" {
			continue
		}
		if i == 0 || (msgs[i-1].Role != "assistant" && msgs[i-1].Role != "tool") {
			t.Fatalf("tool message %d follows %q, not an assistant turn", i, msgs[i-1].Role)
		}
	}
}

// TestContextBalloonIsTrimmed is the regression test for the context
// balloon: the agent loop's history grew far past num_ctx, so Ollama cut the
// prompt from the front and the model lost its instructions. The request
// that reaches Ollama must fit num_ctx minus the reply reserve, with the
// system prompt, the task, and the latest tool output intact.
func TestContextBalloonIsTrimmed(t *testing.T) {
	t.Parallel()
	const numCtx = 16384
	f, srv := newFakeOllama(t, fakeReply{body: toolOnlyReply})
	llm := New(srv.URL, "qwen3", numCtx)
	// ~400K tokens by squad's estimate, in enough turns that replacing old
	// tool outputs alone cannot fit them, so the oldest turns are removed too.
	msgs := longConversation(400, 3_000)
	if _, err := llm.GenerateContent(context.Background(), msgs, weatherOpts()...); err != nil {
		t.Fatalf("GenerateContent() error = %v", err)
	}
	reqs, _ := f.requests()
	sent := reqs[0]
	// weatherOpts asks for 1024 output tokens and thinking is off, so 1024
	// is the reply reserve.
	budget := numCtx - 1024 - jsonTokens(sent.Tools)
	if got := estimateTokens(sent.Messages); got > budget {
		t.Fatalf("request estimate %d tokens exceeds budget %d", got, budget)
	}
	assertWellFormed(t, sent.Messages)
	last := sent.Messages[len(sent.Messages)-1]
	if last.Role != "tool" || !strings.HasPrefix(last.Content, "turn-399-output:") {
		t.Fatalf("latest tool output must be kept, got %q", truncate(last.Content))
	}
	prev := sent.Messages[len(sent.Messages)-2]
	if prev.Role != "assistant" || len(prev.ToolCalls) != 1 {
		t.Fatalf("latest assistant tool call must be kept, got %+v", prev)
	}
	if last.Content != msgs[len(msgs)-1].Parts[0].(llms.ToolCallResponse).Content {
		t.Fatal("latest tool output must not be shortened when dropping old turns is enough")
	}
	if !strings.Contains(sent.Messages[2].Content, "squad removed the") {
		t.Fatalf("a note must replace the removed turns, got %q", truncate(sent.Messages[2].Content))
	}
}

func TestContextUnderBudgetUnchanged(t *testing.T) {
	t.Parallel()
	llm := New("http://x", "qwen3", 65536)
	chatMsgs, err := convertMessages(longConversation(3, 1000))
	if err != nil {
		t.Fatal(err)
	}
	req := llm.buildChatRequest(chatMsgs, llms.CallOptions{MaxTokens: 1024})
	got := llm.fitContext(context.Background(), req)
	if len(got) != len(chatMsgs) {
		t.Fatalf("messages = %d, want %d unchanged", len(got), len(chatMsgs))
	}
	for i := range got {
		if got[i].Content != chatMsgs[i].Content {
			t.Fatalf("message %d changed under budget", i)
		}
	}
}

// TestTrimPrefersOldToolOutputs checks the first, gentlest step: when
// replacing older tool outputs is enough, every turn survives.
func TestTrimPrefersOldToolOutputs(t *testing.T) {
	t.Parallel()
	msgs, err := convertMessages(longConversation(10, 3000))
	if err != nil {
		t.Fatal(err)
	}
	budget := estimateTokens(msgs) - 3000
	out, stats := trimToBudget(msgs, budget, 32768)
	if stats.dropped != 0 || stats.truncated != 0 || stats.compacted == 0 {
		t.Fatalf("want only older outputs replaced, got %s", stats)
	}
	if len(out) != len(msgs) {
		t.Fatalf("messages = %d, want %d", len(out), len(msgs))
	}
	if !strings.Contains(out[3].Content, "older tool output removed") {
		t.Fatalf("oldest tool output must be replaced first, got %q", truncate(out[3].Content))
	}
	if out[len(out)-1].Content != msgs[len(msgs)-1].Content {
		t.Fatal("latest tool output must be untouched")
	}
	if estimateTokens(out) > budget {
		t.Fatalf("estimate %d over budget %d", estimateTokens(out), budget)
	}
	if msgs[3].Content == out[3].Content {
		t.Fatal("trimming must not be a no-op")
	}
	if strings.Contains(msgs[3].Content, "older tool output removed") {
		t.Fatal("trimming must not modify the caller's messages")
	}
}

// TestTrimShortensHugeLatestOutput covers a single tool output bigger than
// the whole window: it is cut in the middle, keeping its start and end.
func TestTrimShortensHugeLatestOutput(t *testing.T) {
	t.Parallel()
	msgs, err := convertMessages(longConversation(1, 200_000))
	if err != nil {
		t.Fatal(err)
	}
	msgs[len(msgs)-1].Content += "END-MARKER"
	out, stats := trimToBudget(msgs, 4000, 8192)
	if stats.truncated == 0 {
		t.Fatalf("want the latest output shortened, got %s", stats)
	}
	if estimateTokens(out) > 4000 {
		t.Fatalf("estimate %d over budget 4000", estimateTokens(out))
	}
	assertWellFormed(t, out)
	last := out[len(out)-1].Content
	if !strings.HasPrefix(last, "turn-0-output:") || !strings.HasSuffix(last, "END-MARKER") || !strings.Contains(last, "bytes removed by squad") {
		t.Fatalf("shortened output must keep its start and end, got %q", truncate(last))
	}
}

// TestTrimNeverDropsInstructions checks that a head bigger than the budget is
// sent as is: squad never drops the system prompt or the task.
func TestTrimNeverDropsInstructions(t *testing.T) {
	t.Parallel()
	msgs := []ollamaMessage{
		{Role: "system", Content: strings.Repeat("s", 30_000)},
		{Role: "user", Content: "task"},
	}
	out, _ := trimToBudget(msgs, 100, 4096)
	if len(out) != 2 || out[0].Content != msgs[0].Content {
		t.Fatal("instructions must never be dropped or cut")
	}
}

// TestTrimIsStableAcrossTurns checks that as the conversation grows one turn
// per request, the trimmed prefix changes only every few requests, so
// Ollama can reuse its prompt cache instead of re-reading the whole prompt.
func TestTrimIsStableAcrossTurns(t *testing.T) {
	t.Parallel()
	full, err := convertMessages(longConversation(48, 3000))
	if err != nil {
		t.Fatal(err)
	}
	const budget = 20_000
	boundaries := map[int]bool{}
	for turns := 40; turns <= 48; turns++ {
		msgs := full[:2+2*turns]
		_, stats := trimToBudget(msgs, budget, 32768)
		if stats.compacted%trimStep != 0 {
			t.Fatalf("turns=%d: replaced %d outputs, want a multiple of %d", turns, stats.compacted, trimStep)
		}
		boundaries[stats.compacted] = true
	}
	if len(boundaries) > 2 {
		t.Fatalf("trim boundary moved %d times over 9 requests; want at most 2 distinct boundaries", len(boundaries))
	}
}

func TestTruncateMiddleKeepsUTF8Valid(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("héllo wörld ✓ ", 2000)
	got := truncateMiddle(s, 1001, 8192)
	if !utf8.ValidString(got) {
		t.Fatal("truncated string must stay valid UTF-8")
	}
	if len(got) >= len(s) {
		t.Fatal("string must shrink")
	}
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}
