package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/watch"
)

// writeWatchSession writes a session dir with meta.json and the given
// events.jsonl lines.
func writeWatchSession(t *testing.T, status string, lines ...string) string {
	t.Helper()
	dir := t.TempDir()
	meta := session.Meta{
		SessionID:       "20261118T000000Z-abcd1234",
		Agent:           "probe",
		Provider:        "claude-code",
		Model:           "haiku",
		Status:          status,
		TraceID:         "0102030405060708090a0b0c0d0e0f10",
		ClaudeSessionID: "cc-1",
	}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

var watchLines = []string{
	`{"ts":"2026-11-18T15:00:00Z","type":"iteration","trace_id":"0102030405060708090a0b0c0d0e0f10","span_id":"1112131415161718","payload":{"index":1,"tool_calls":1}}`,
	`{"ts":"2026-11-18T15:00:01Z","type":"response","payload":{"iteration":1,"input_tokens":5,"output_tokens":2,"text":"Checking notes."}}`,
	`{"ts":"2026-11-18T15:00:02Z","type":"tool_call","payload":{"name":"Bash","args":"{\"command\":\"cat notes.txt\"}"}}`,
	`{"ts":"2026-11-18T15:00:03Z","type":"tool_result","payload":{"name":"Bash","bytes":10,"duration_ms":40,"is_error":false}}`,
	`{"ts":"2026-11-18T15:00:04Z","type":"tool_denied","payload":{"name":"Write","reason":"readonly"}}`,
}

func TestFollowSessionOnce(t *testing.T) {
	dir := writeWatchSession(t, session.StatusRunning, watchLines...)
	var out bytes.Buffer
	if err := followSession(context.Background(), &out, watch.NewTailer(dir), true, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"session 20261118T000000Z-abcd1234 · agent=probe provider=claude-code model=haiku",
		"trace_id 0102030405060708090a0b0c0d0e0f10",
		"claude_session_id cc-1",
		"response (iter 1) · +5 in / +2 out · Checking notes.",
		`Bash  {"command":"cat notes.txt"}`,
		"result · Bash · 10 B · 40ms",
		"denied · Write · readonly",
		"status=running · 1 turns · 1 tool calls (0 failed, 1 denied) · 0 errors",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFollowSessionStopsWhenRunEnds(t *testing.T) {
	dir := writeWatchSession(t, session.StatusCompleted, watchLines...)
	var out bytes.Buffer
	if err := followSession(context.Background(), &out, watch.NewTailer(dir), false, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "status=completed") {
		t.Errorf("output = %s", out.String())
	}
}

func TestFollowSessionFollowsUntilCanceled(t *testing.T) {
	dir := writeWatchSession(t, session.StatusRunning, watchLines[0])
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- followSession(ctx, &out, watch.NewTailer(dir), false, 5*time.Millisecond) }()
	time.Sleep(50 * time.Millisecond)
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(watchLines[2] + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tool_call") {
		t.Errorf("appended event not printed:\n%s", out.String())
	}
}

func TestResolveWatchDir(t *testing.T) {
	dir := writeWatchSession(t, session.StatusCompleted)
	got, err := resolveWatchDir(dir)
	if err != nil || got != dir {
		t.Fatalf("resolveWatchDir(dir) = %q, %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := resolveWatchDir("no-such-session"); err == nil {
		t.Error("want an error for an unknown session id")
	}
}
