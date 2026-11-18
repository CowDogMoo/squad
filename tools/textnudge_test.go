package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/tmc/langchaingo/llms"
)

func textResp(content string) *llms.ContentResponse {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{Content: content}}}
}

// runNudgeLoop drives toolLoop with scripted replies and returns the final
// content, the scripted model, and whether the loop finished on its own.
func runNudgeLoop(t *testing.T, textNudges, maxIter int, responses ...*llms.ContentResponse) (string, *scriptedLLM, bool) {
	t.Helper()
	dir := t.TempDir()
	handlers, _ := BuildHandlers(dir, nil, &executor.LocalExecutor{WorkingDir: dir})
	llm := &scriptedLLM{responses: responses}
	content, _, err, done := toolLoop(InitReadOnlyMode(context.Background()), llm, buildInitialMessages("sys", "user", false), handlers, maxIter, RunWithToolsConfig{TextNudges: textNudges}, nil)
	if err != nil {
		t.Fatalf("toolLoop error = %v", err)
	}
	return content, llm, done
}

// messageText joins the text and tool output in msg.
func messageText(msg llms.MessageContent) string {
	var sb strings.Builder
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llms.TextContent:
			sb.WriteString(p.Text)
		case llms.ToolCallResponse:
			sb.WriteString(p.Content)
		}
	}
	return sb.String()
}

// nudgesSeen counts the nudges in the last request the model was sent.
func nudgesSeen(llm *scriptedLLM) int {
	if len(llm.msgs) == 0 {
		return 0
	}
	n := 0
	for _, msg := range llm.msgs[len(llm.msgs)-1] {
		if messageText(msg) == textNudgeMessage {
			n++
		}
	}
	return n
}

func TestToolLoopTextNudges(t *testing.T) {
	t.Parallel()
	probe := toolCallResp("c1", "Bash", `{"command":"true"}`)
	tests := []struct {
		name        string
		textNudges  int
		maxIter     int
		responses   []*llms.ContentResponse
		wantContent string
		wantCalls   int
		wantNudges  int
	}{
		{
			name:        "off by default: a text reply ends the loop",
			textNudges:  0,
			maxIter:     10,
			responses:   []*llms.ContentResponse{textResp("Let me check the key."), probe},
			wantContent: "Let me check the key.",
			wantCalls:   1,
		},
		{
			name:        "a nudged agent goes back to its tools",
			textNudges:  1,
			maxIter:     10,
			responses:   []*llms.ContentResponse{textResp("Let me check the key."), probe, textResp("report")},
			wantContent: "report",
			wantCalls:   3,
			wantNudges:  1,
		},
		{
			name:        "a text reply to a nudge is final",
			textNudges:  3,
			maxIter:     10,
			responses:   []*llms.ContentResponse{textResp("draft"), textResp("final report"), probe},
			wantContent: "final report",
			wantCalls:   2,
			wantNudges:  1,
		},
		{
			name:       "no more nudges than allowed",
			textNudges: 2,
			maxIter:    10,
			responses: []*llms.ContentResponse{
				textResp("one"), probe, textResp("two"), probe, textResp("three"), probe,
			},
			wantContent: "three",
			wantCalls:   5,
			wantNudges:  2,
		},
		{
			name:        "an empty reply is not nudged",
			textNudges:  1,
			maxIter:     10,
			responses:   []*llms.ContentResponse{textResp("  \n"), probe},
			wantContent: "  \n",
			wantCalls:   1,
		},
		{
			name:        "the last iteration is not nudged",
			textNudges:  1,
			maxIter:     1,
			responses:   []*llms.ContentResponse{textResp("Let me check the key."), probe},
			wantContent: "Let me check the key.",
			wantCalls:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, llm, done := runNudgeLoop(t, tt.textNudges, tt.maxIter, tt.responses...)
			if !done {
				t.Fatal("loop must finish on the model's final reply")
			}
			if content != tt.wantContent {
				t.Fatalf("content = %q, want %q", content, tt.wantContent)
			}
			if llm.calls != tt.wantCalls {
				t.Fatalf("model calls = %d, want %d", llm.calls, tt.wantCalls)
			}
			if got := nudgesSeen(llm); got != tt.wantNudges {
				t.Fatalf("nudges in the last request = %d, want %d", got, tt.wantNudges)
			}
		})
	}
}

// TestToolLoopTextNudgeKeepsTheReply checks the model sees its own text
// before the nudge, so the conversation reads as a reply and a follow-up.
func TestToolLoopTextNudgeKeepsTheReply(t *testing.T) {
	t.Parallel()
	_, llm, _ := runNudgeLoop(t, 1, 10, textResp("Let me check the key."), textResp("done"))
	last := llm.msgs[len(llm.msgs)-1]
	if len(last) < 2 {
		t.Fatalf("want the reply and the nudge at the end, got %d messages", len(last))
	}
	reply, nudge := last[len(last)-2], last[len(last)-1]
	if reply.Role != llms.ChatMessageTypeAI || !strings.Contains(messageText(reply), "check the key") {
		t.Fatalf("second-to-last message = %+v, want the model's reply", reply)
	}
	if nudge.Role != llms.ChatMessageTypeHuman || messageText(nudge) != textNudgeMessage {
		t.Fatalf("last message = %+v, want the nudge", nudge)
	}
}
