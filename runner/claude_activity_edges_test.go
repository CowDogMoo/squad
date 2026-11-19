package runner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TestClaudeActivityRecordsThinkingText checks that the text of a thinking
// block reaches the turn's response event, and that empty thinking blocks
// (signature-only, as hosted models send them) add nothing.
func TestClaudeActivityRecordsThinkingText(t *testing.T) {
	ctx, _, l := recordClaude(t)
	a := newClaudeActivity(ctx, "haiku")
	a.started = ts(0)
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), Thinking: []string{"", "compare the two files"}})
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(2), Text: []string{"They match."}})
	a.Finished(ts(3))

	resp := claudeEventsByType(t, l)["response"]
	if len(resp) != 1 {
		t.Fatalf("response events = %d, want 1", len(resp))
	}
	if resp[0].Payload["thinking"] != "compare the two files" || resp[0].Payload["text"] != "They match." {
		t.Errorf("response payload = %v, want the thinking text and the reply", resp[0].Payload)
	}
}

// TestClaudeActivityUntimedMessageEndsAtClose checks that a turn built from a
// message with no timestamp is ended at the run's close time rather than at
// the zero time.
func TestClaudeActivityUntimedMessageEndsAtClose(t *testing.T) {
	ctx, rec, _ := recordClaude(t)
	a := newClaudeActivity(ctx, "haiku")
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Text: []string{"no timestamp"}})
	closeAt := time.Now().Add(time.Second)
	a.close(closeAt)

	var found bool
	for _, s := range rec.Ended() {
		if s.Name() != "chat haiku" {
			continue
		}
		found = true
		if !s.EndTime().Equal(closeAt) {
			t.Errorf("turn ended %v, want the close time %v", s.EndTime(), closeAt)
		}
	}
	if !found {
		t.Fatal("no turn span was ended")
	}
}

// TestClaudeToolResultErrorStatus checks that a claude-code tool_result
// error that names a path reaches events.jsonl in full and never reaches the
// tool.Read span, which gets the fixed status even with the commands opt-in
// on.
func TestClaudeToolResultErrorStatus(t *testing.T) {
	const content = `path "/tmp/webapp-map/candidates.jsonl" is outside working directory; token=hunter2-fixture`
	for _, env := range []string{"", "1"} {
		t.Run("opt-in="+env, func(t *testing.T) {
			t.Setenv(telemetry.ExportCommandsEnv, env)
			read, l := runClaudeFailedRead(t, content)
			if got := read.Status(); got.Code != codes.Error || got.Description != "tool returned an error" {
				t.Errorf("tool.Read status = %+v, want the fixed error status", got)
			}
			if text := exportedText(read); strings.Contains(text, "/tmp/webapp-map") || strings.Contains(text, "hunter2") {
				t.Errorf("tool.Read span exports the tool result:\n%s", text)
			}
			checkEventsHold(t, l, []string{`/tmp/webapp-map/candidates.jsonl\" is outside working directory; token=hunter2-fixture`})
		})
	}
}

// runClaudeFailedRead feeds a claude-code Read call whose tool_result is an
// error holding content, and returns the tool.Read span and the session.
func runClaudeFailedRead(t *testing.T, content string) (sdktrace.ReadOnlySpan, *session.Logger) {
	t.Helper()
	ctx, rec, l := recordClaude(t)
	a := newClaudeActivity(ctx, "haiku")
	a.Assistant(agenticcli.AssistantMessage{ID: "m1", Time: ts(1), ToolUses: []agenticcli.ToolUse{
		{ID: "r1", Name: "Read", Input: json.RawMessage(`{"file_path":"/tmp/webapp-map/candidates.jsonl"}`)},
	}})
	a.ToolResults([]agenticcli.ToolResult{{ToolUseID: "r1", Time: ts(2), IsError: true, Content: content}})
	a.Finished(ts(3))
	for _, s := range rec.Ended() {
		if s.Name() == "tool.Read" {
			return s, l
		}
	}
	t.Fatal("no tool.Read span")
	return nil, nil
}
