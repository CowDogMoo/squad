// Package fakeclaude implements a scripted stand-in for the `claude` CLI's
// stream-json control protocol, used by agenticcli and runner tests. It
// speaks the wire shapes recorded in agenticcli/testdata (init → assistant
// text → can_use_tool → control_response → … → result) without any network
// or credentials.
//
// It is compiled into a test binary and invoked through a tiny shell shim on
// PATH (`exec <test-binary> -test.run '^TestHelperFakeClaude$' --`), the
// standard helper-process pattern. Behavior is configured via FAKE_CLAUDE_*
// environment variables:
//
//	FAKE_CLAUDE_MAX_ASKS          can_use_tool attempts before giving up (default 3)
//	FAKE_CLAUDE_ASK_BEFORE_TEXT   "1": first ask arrives with no prior assistant prose
//	FAKE_CLAUDE_GARBAGE           emit N non-JSON stdout lines before init
//	FAKE_CLAUDE_EXIT_EARLY        "1": die after the first ask without a result event
//	FAKE_CLAUDE_WRITE_FILE        on allow, write this file (content "hi") in the cwd
//	FAKE_CLAUDE_HANG_AFTER_RESULT "1": ignore stdin EOF and sleep after the result
package fakeclaude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Install puts a fake `claude` on PATH for the duration of the test: a shim
// that re-execs the current test binary into Main via a helper test named
// TestHelperFakeClaude, which the calling package must define:
//
//	func TestHelperFakeClaude(t *testing.T) {
//		if os.Getenv("FAKE_CLAUDE") != "1" {
//			t.Skip("helper process for the fake claude shim")
//		}
//		fakeclaude.Main()
//	}
func Install(t *testing.T, env map[string]string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\nexport FAKE_CLAUDE=1\n")
	for k, v := range env {
		fmt.Fprintf(&sb, "export %s=%q\n", k, v)
	}
	fmt.Fprintf(&sb, "exec %q -test.run '^TestHelperFakeClaude$' -- \"$@\"\n", self)
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(sb.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type decision struct {
	allow     bool
	message   string
	interrupt bool
}

// Main runs the fake CLI session and never returns.
func Main() {
	in := bufio.NewReader(os.Stdin)

	for i := range envInt("FAKE_CLAUDE_GARBAGE", 0) {
		fmt.Printf("this is not json (line %d)\n", i+1)
	}

	emit(map[string]any{
		"type": "system", "subtype": "init",
		"session_id": "fake-session", "model": "fake-model",
		"permissionMode": "default", "tools": []string{"Write", "Read", "Bash"},
	})
	// Telemetry noise the parent must ignore.
	emit(map[string]any{"type": "rate_limit_event", "rate_limit_info": map[string]any{"status": "allowed"}})
	emit(map[string]any{"type": "system", "subtype": "thinking_tokens", "estimated_tokens": 12})

	awaitUserMessage(in)

	maxAsks := envInt("FAKE_CLAUDE_MAX_ASKS", 3)
	msgSeq := 0
	if os.Getenv("FAKE_CLAUDE_ASK_BEFORE_TEXT") != "1" {
		msgSeq++
		emitAssistantText(msgSeq, "I plan to create hello.txt containing hi. (plan v1)")
	}
	for ask := 1; ; ask++ {
		if ask > maxAsks {
			emitResult(fmt.Sprintf("gave up after %d denials", maxAsks), maxAsks)
			drainAndExit(in)
		}
		reqID := fmt.Sprintf("req-%d", ask)
		toolUseID := fmt.Sprintf("toolu_fake_%d", ask)
		emit(map[string]any{
			"type": "control_request", "request_id": reqID,
			"request": map[string]any{
				"subtype": "can_use_tool", "tool_name": "Write", "display_name": "Write",
				"input":       map[string]any{"file_path": "hello.txt", "content": "hi"},
				"tool_use_id": toolUseID,
			},
		})
		if os.Getenv("FAKE_CLAUDE_EXIT_EARLY") == "1" {
			fmt.Fprintln(os.Stderr, "boom: fake claude dying before result")
			os.Exit(1)
		}
		d := awaitDecision(in, reqID)
		if d.interrupt {
			emitResult("Run interrupted by user", ask)
			drainAndExit(in)
		}
		if d.allow {
			if name := os.Getenv("FAKE_CLAUDE_WRITE_FILE"); name != "" {
				if err := os.WriteFile(name, []byte("hi"), 0o644); err != nil {
					fmt.Fprintf(os.Stderr, "fake claude: write %s: %v\n", name, err)
					os.Exit(1)
				}
			}
			emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []map[string]any{
				{"tool_use_id": toolUseID, "type": "tool_result", "content": "File created successfully"}}}})
			msgSeq++
			emitAssistantText(msgSeq, "Done. File created.")
			emitResult("fake done", ask)
			if os.Getenv("FAKE_CLAUDE_HANG_AFTER_RESULT") == "1" {
				time.Sleep(30 * time.Second)
				os.Exit(0)
			}
			drainAndExit(in)
		}
		// Denied: echo the deny message back as the next plan so tests can
		// assert the round-trip, mimicking the error tool_result the real
		// CLI feeds the model.
		emit(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []map[string]any{
			{"tool_use_id": toolUseID, "type": "tool_result", "content": d.message, "is_error": true}}}})
		msgSeq++
		emitAssistantText(msgSeq, fmt.Sprintf("plan attempt %d after deny: %s", ask+1, d.message))
	}
}

func emit(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake claude: marshal: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
		os.Exit(1)
	}
}

func emitAssistantText(msgSeq int, text string) {
	emit(map[string]any{"type": "assistant", "message": map[string]any{
		"id": fmt.Sprintf("msg_fake_%d", msgSeq), "role": "assistant",
		"content": []map[string]any{{"type": "text", "text": text}},
	}})
}

func emitResult(text string, turns int) {
	emit(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"num_turns": turns, "result": text, "session_id": "fake-session",
		"usage": map[string]any{
			"input_tokens": 100, "cache_creation_input_tokens": 10,
			"cache_read_input_tokens": 5, "output_tokens": 20,
		},
	})
}

// awaitUserMessage blocks until the initial user event arrives.
func awaitUserMessage(in *bufio.Reader) {
	for {
		ev := readEvent(in)
		if ev["type"] == "user" {
			return
		}
	}
}

// awaitDecision blocks until the control_response for reqID (or a
// parent-initiated interrupt control_request, which is acked first).
func awaitDecision(in *bufio.Reader, reqID string) decision {
	for {
		ev := readEvent(in)
		switch ev["type"] {
		case "control_request":
			req, _ := ev["request"].(map[string]any)
			if req["subtype"] == "interrupt" {
				emit(map[string]any{"type": "control_response", "response": map[string]any{
					"subtype": "success", "request_id": ev["request_id"],
					"response": map[string]any{"still_queued": []any{}},
				}})
				return decision{interrupt: true}
			}
		case "control_response":
			resp, _ := ev["response"].(map[string]any)
			if resp["request_id"] != reqID {
				continue
			}
			inner, _ := resp["response"].(map[string]any)
			msg, _ := inner["message"].(string)
			return decision{allow: inner["behavior"] == "allow", message: msg}
		}
	}
}

// readEvent reads one JSON line from stdin, exiting cleanly on EOF (the
// parent closed stdin) like the real CLI does.
func readEvent(in *bufio.Reader) map[string]any {
	line, err := in.ReadString('\n')
	if len(line) > 0 {
		var ev map[string]any
		if json.Unmarshal([]byte(line), &ev) == nil {
			return ev
		}
	}
	if err != nil {
		os.Exit(0)
	}
	return map[string]any{}
}

// drainAndExit mimics the real CLI post-result: wait for stdin EOF, then exit.
func drainAndExit(in *bufio.Reader) {
	_, _ = io.Copy(io.Discard, in)
	os.Exit(0)
}

func envInt(name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
