package activity

import (
	"testing"
	"time"
)

// TestRecordAtAndEndAtDefaultToNow checks that a zero finish time measures the
// turn up to now and a zero end time closes the span now.
func TestRecordAtAndEndAtDefaultToNow(t *testing.T) {
	ctx, rec, l := setup(t)
	start := time.Now().Add(-2 * time.Second)
	turn := Recorder{Provider: "claude-code", Model: "haiku"}.StartTurn(ctx, 1, start)
	before := time.Now()
	turn.RecordAt(TurnResult{Text: "done"}, time.Time{})
	turn.EndAt(time.Time{})

	resp := eventsByType(t, l)["response"]
	if len(resp) != 1 {
		t.Fatalf("response events = %d, want 1", len(resp))
	}
	if d := durationMS(resp[0].Payload); d < 2000 {
		t.Errorf("duration_ms = %v, want measured to now (>= 2000)", d)
	}
	span := spanNamed(t, rec, "chat haiku")
	if span.EndTime().Before(before) {
		t.Errorf("span ended %v, want now (after %v)", span.EndTime(), before)
	}
}

// durationMS reads a response event's duration_ms, which JSON decodes as a
// float64.
func durationMS(p map[string]any) float64 { return p["duration_ms"].(float64) }

// TestAppendUnencodablePayload checks that an event whose payload cannot be
// encoded is dropped without writing a broken line or panicking.
func TestAppendUnencodablePayload(t *testing.T) {
	ctx, _, l := setup(t)
	Append(ctx, "custom", map[string]any{"bad": make(chan int)})
	Append(ctx, "custom", map[string]any{"ok": true})
	evs := eventsByType(t, l)["custom"]
	if len(evs) != 1 || evs[0].Payload["ok"] != true {
		t.Errorf("custom events = %+v, want only the encodable one", evs)
	}
}
