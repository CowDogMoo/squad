package runner

import (
	"testing"
	"time"

	"github.com/cowdogmoo/squad/agenticcli"
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
