package agenticcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/internal/fakeclaude"
)

// TestHelperFakeClaude is not a test: it is the body of the fake `claude`
// binary, entered when the shim script installed by installFakeClaude
// re-execs this test binary. See internal/fakeclaude.
func TestHelperFakeClaude(t *testing.T) {
	if os.Getenv("FAKE_CLAUDE") != "1" {
		t.Skip("helper process for the fake claude shim")
	}
	fakeclaude.Main()
}

// installFakeClaude puts a fake `claude` on PATH: a shim that re-execs this
// test binary into fakeclaude.Main with the given FAKE_CLAUDE_* env.
func installFakeClaude(t *testing.T, env map[string]string) {
	t.Helper()
	fakeclaude.Install(t, env)
}

// askRecord captures one CanUseTool invocation.
type askRecord struct {
	tool  string
	input string
	text  string
}

func liveReq(workDir string, cb CanUseTool) LiveRequest {
	return LiveRequest{
		Request: Request{
			Provider:     "claude-code",
			SystemPrompt: "sys",
			UserPrompt:   "create hello.txt",
			WorkDir:      workDir,
		},
		CanUseTool: cb,
	}
}

func TestRunLive_AllowEverything(t *testing.T) {
	dir := t.TempDir()
	installFakeClaude(t, map[string]string{"FAKE_CLAUDE_WRITE_FILE": "hello.txt"})

	var asks []askRecord
	res, err := RunLive(context.Background(), liveReq(dir, func(_ context.Context, tool string, input json.RawMessage, text string) (Decision, error) {
		asks = append(asks, askRecord{tool: tool, input: string(input), text: text})
		return Decision{Allow: true}, nil
	}))
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if res.Response != "fake done" || res.Turns != 1 {
		t.Errorf("result = %+v, want Response=fake done Turns=1", res)
	}
	// Token mapping parity with the single-shot path: input + both cache
	// counters summed.
	if res.InputTokens != 115 || res.OutputTokens != 20 {
		t.Errorf("tokens = %d/%d, want 115/20", res.InputTokens, res.OutputTokens)
	}
	if len(asks) != 1 {
		t.Fatalf("CanUseTool called %d times, want 1", len(asks))
	}
	if asks[0].tool != "Write" || !strings.Contains(asks[0].input, "hello.txt") {
		t.Errorf("ask = %+v, want Write on hello.txt", asks[0])
	}
	if !strings.Contains(asks[0].text, "plan v1") {
		t.Errorf("assistantText = %q, want the accumulated plan text", asks[0].text)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); err != nil {
		t.Errorf("allowed Write did not land: %v", err)
	}
}

func TestRunLive_DenyThenAllow(t *testing.T) {
	dir := t.TempDir()
	installFakeClaude(t, nil)

	var asks []askRecord
	res, err := RunLive(context.Background(), liveReq(dir, func(_ context.Context, tool string, input json.RawMessage, text string) (Decision, error) {
		asks = append(asks, askRecord{tool: tool, input: string(input), text: text})
		if len(asks) == 1 {
			return Decision{Message: "write out your plan first"}, nil
		}
		return Decision{Allow: true}, nil
	}))
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if len(asks) != 2 {
		t.Fatalf("CanUseTool called %d times, want 2", len(asks))
	}
	// The deny message steers the model: the fake echoes it into its next
	// plan text, which must reach the second callback.
	if !strings.Contains(asks[1].text, "write out your plan first") {
		t.Errorf("second assistantText = %q, want deny-message round-trip", asks[1].text)
	}
	if res.Turns != 2 {
		t.Errorf("Turns = %d, want 2", res.Turns)
	}
}

func TestRunLive_AskBeforeText(t *testing.T) {
	installFakeClaude(t, map[string]string{"FAKE_CLAUDE_ASK_BEFORE_TEXT": "1", "FAKE_CLAUDE_MAX_ASKS": "1"})

	var texts []string
	_, err := RunLive(context.Background(), liveReq(t.TempDir(), func(_ context.Context, _ string, _ json.RawMessage, text string) (Decision, error) {
		texts = append(texts, text)
		return Decision{Message: "no plan yet"}, nil
	}))
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if len(texts) != 1 || texts[0] != "" {
		t.Errorf("texts = %q, want one empty assistantText (no prose before first ask)", texts)
	}
}

func TestRunLive_CallbackErrorFailsClosed(t *testing.T) {
	installFakeClaude(t, nil)

	_, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{}, errors.New("tty went away")
	}))
	if err == nil || !strings.Contains(err.Error(), "sign-off callback failed") {
		t.Fatalf("err = %v, want sign-off callback failure", err)
	}
}

func TestRunLive_InterruptDecision(t *testing.T) {
	installFakeClaude(t, nil)

	res, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Message: "plan rejected", Interrupt: true}, nil
	}))
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if res.Response != "Run interrupted by user" {
		t.Errorf("Response = %q, want the interrupted summary", res.Response)
	}
}

func TestRunLive_ProtocolGarbage(t *testing.T) {
	allowAll := func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}
	t.Run("two consecutive garbage lines fail closed", func(t *testing.T) {
		installFakeClaude(t, map[string]string{"FAKE_CLAUDE_GARBAGE": "2"})
		_, err := RunLive(context.Background(), liveReq(t.TempDir(), allowAll))
		if err == nil || !strings.Contains(err.Error(), "protocol failure") {
			t.Fatalf("err = %v, want protocol failure", err)
		}
	})
	t.Run("one garbage line is tolerated", func(t *testing.T) {
		installFakeClaude(t, map[string]string{"FAKE_CLAUDE_GARBAGE": "1"})
		res, err := RunLive(context.Background(), liveReq(t.TempDir(), allowAll))
		if err != nil {
			t.Fatalf("RunLive: %v", err)
		}
		if res.Response != "fake done" {
			t.Errorf("Response = %q, want fake done", res.Response)
		}
	})
}

func TestRunLive_ExitWithoutResult(t *testing.T) {
	installFakeClaude(t, map[string]string{"FAKE_CLAUDE_EXIT_EARLY": "1"})

	_, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}))
	if err == nil || !strings.Contains(err.Error(), "without a result event") {
		t.Fatalf("err = %v, want exit-without-result", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the CLI stderr tail included", err)
	}
}

// TestRunLive_HandshakeFailure pins the fail-closed path for an installed
// CLI that predates the control protocol: it dies before the init event, and
// the error must carry both its stderr and a version hint.
func TestRunLive_HandshakeFailure(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\necho \"error: unknown option '--permission-prompt-tool'\" >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}))
	if err == nil {
		t.Fatal("want handshake failure")
	}
	for _, want := range []string{"unknown option", "check `claude --version`"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

func TestRunLive_WatchdogKillsHungProcess(t *testing.T) {
	old := claudeLiveExitGrace
	claudeLiveExitGrace = 200 * time.Millisecond
	t.Cleanup(func() { claudeLiveExitGrace = old })
	installFakeClaude(t, map[string]string{"FAKE_CLAUDE_HANG_AFTER_RESULT": "1"})

	start := time.Now()
	res, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}))
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if res.Response != "fake done" {
		t.Errorf("Response = %q, want fake done despite the hang", res.Response)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("RunLive took %s, watchdog should have killed the hung process", elapsed)
	}
}

func TestRunLive_ContextCancel(t *testing.T) {
	installFakeClaude(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	_, err := RunLive(ctx, liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		cancel() // cancel mid-session, while the CLI waits for our answer
		<-ctx.Done()
		return Decision{Allow: true}, nil
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRunLive_RejectsNonClaude(t *testing.T) {
	t.Parallel()
	cb := func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}
	if _, err := RunLive(context.Background(), LiveRequest{Request: Request{Provider: "antigravity"}, CanUseTool: cb}); err == nil {
		t.Error("want error for antigravity provider")
	}
	if _, err := RunLive(context.Background(), LiveRequest{Request: Request{Provider: "claude-code"}}); err == nil {
		t.Error("want error for nil CanUseTool")
	}
}

func TestClaudeLiveArgs(t *testing.T) {
	t.Parallel()
	req := Request{Provider: "claude-code", SystemPrompt: "sys", Model: "opus", ReadOnly: true}
	got := claudeLiveArgs(req)
	want := []string{
		"--print", "--input-format", "stream-json", "--output-format", "stream-json",
		"--verbose", "--permission-prompt-tool", "stdio", "--permission-mode", "default",
		"--append-system-prompt", "sys", "--model", "opus",
		"--disallowed-tools", readOnlyDisallowedTools,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("claudeLiveArgs = %q, want %q", got, want)
	}
	for _, arg := range got {
		if arg == "--dangerously-skip-permissions" {
			t.Error("live args must not bypass permission checks")
		}
	}
}

// nopWriteCloser adapts a bytes.Buffer into the session's stdin sink.
type nopWriteCloser struct{ *bytes.Buffer }

func (nopWriteCloser) Close() error { return nil }

// failingWriteCloser injects write/close failures into the stdin sink.
type failingWriteCloser struct{ writeErr, closeErr error }

func (f failingWriteCloser) Write([]byte) (int, error) { return 0, f.writeErr }
func (f failingWriteCloser) Close() error              { return f.closeErr }

func TestRunLive_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := RunLive(context.Background(), liveReq(t.TempDir(), func(context.Context, string, json.RawMessage, string) (Decision, error) {
		return Decision{Allow: true}, nil
	}))
	if err == nil || !strings.Contains(err.Error(), `requires the "claude" CLI on PATH`) {
		t.Fatalf("err = %v, want the install hint", err)
	}
}

func TestLiveSessionProtocolBranches(t *testing.T) {
	t.Parallel()

	t.Run("control_request without request_id counts toward the budget", func(t *testing.T) {
		t.Parallel()
		s := &liveSession{ctx: context.Background()}
		if err := s.handle([]byte(`{"type":"control_request"}`)); err != nil {
			t.Fatalf("first malformed request must not be fatal: %v", err)
		}
		if err := s.handle([]byte(`{"type":"control_request"}`)); err == nil || !strings.Contains(err.Error(), "protocol failure") {
			t.Fatalf("second consecutive malformed request must fail closed, got %v", err)
		}
	})

	t.Run("unknown control subtype gets an error response", func(t *testing.T) {
		t.Parallel()
		var sent bytes.Buffer
		s := &liveSession{ctx: context.Background(), stdin: nopWriteCloser{&sent}}
		err := s.handle([]byte(`{"type":"control_request","request_id":"r9","request":{"subtype":"hook_callback"}}`))
		if err != nil {
			t.Fatalf("one unknown subtype must not be fatal: %v", err)
		}
		if got := sent.String(); !strings.Contains(got, `"subtype":"error"`) || !strings.Contains(got, `"request_id":"r9"`) {
			t.Errorf("expected an error control_response for r9, sent: %s", got)
		}
	})

	t.Run("errOrEOF keeps non-EOF errors verbatim", func(t *testing.T) {
		t.Parallel()
		underlying := errors.New("read: connection reset")
		if got := errOrEOF(underlying); got != underlying {
			t.Errorf("errOrEOF rewrote a real error: %v", got)
		}
		if got := errOrEOF(io.EOF); !strings.Contains(got.Error(), "closed stdout") {
			t.Errorf("errOrEOF(EOF) = %v, want the readable rewrite", got)
		}
	})

	t.Run("send fails after stdin is closed", func(t *testing.T) {
		t.Parallel()
		s := &liveSession{ctx: context.Background(), stdin: nopWriteCloser{&bytes.Buffer{}}}
		s.closeStdin()
		if err := s.send([]byte("{}")); err == nil || !strings.Contains(err.Error(), "already closed") {
			t.Fatalf("err = %v, want stdin-closed", err)
		}
	})

	t.Run("send surfaces write errors", func(t *testing.T) {
		t.Parallel()
		s := &liveSession{ctx: context.Background(), stdin: failingWriteCloser{writeErr: errors.New("pipe broke")}}
		if err := s.send([]byte("{}")); err == nil || !strings.Contains(err.Error(), "pipe broke") {
			t.Fatalf("err = %v, want the write error", err)
		}
	})

	t.Run("closeStdin tolerates close errors and is idempotent", func(t *testing.T) {
		t.Parallel()
		s := &liveSession{ctx: context.Background(), stdin: failingWriteCloser{closeErr: errors.New("bad fd")}}
		s.closeStdin()
		s.closeStdin()
		if !s.stdinClosed {
			t.Fatal("stdin must be marked closed despite the close error")
		}
	})

	t.Run("accumulate tolerates nil and empty blocks", func(t *testing.T) {
		t.Parallel()
		s := &liveSession{ctx: context.Background()}
		s.accumulate(nil)
		s.accumulate(&streamedMsg{ID: "m1", Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{{Type: "text", Text: ""}, {Type: "tool_use"}}})
		if got := s.latestAssistantText(); got != "" {
			t.Errorf("latestAssistantText = %q, want empty", got)
		}
	})
}

// TestGoldenSessionReplay replays the recorded CLI stdout fixture through the
// event loop and verifies the control responses squad produces are
// semantically identical to what the recorded parent sent, pinning the wire
// protocol against claude CLI 2.1.220.
func TestGoldenSessionReplay(t *testing.T) {
	t.Parallel()
	sessionLines := fixtureLines(t, "claude_live_session.jsonl")
	parentLines := fixtureLines(t, "claude_live_parent.jsonl")

	var sent bytes.Buffer
	calls := 0
	s := &liveSession{
		ctx:   context.Background(),
		stdin: nopWriteCloser{&sent},
		canUseTool: func(_ context.Context, tool string, input json.RawMessage, _ string) (Decision, error) {
			calls++
			if tool != "Write" {
				t.Errorf("call %d: tool = %q, want Write", calls, tool)
			}
			if calls == 1 {
				// Mirror the recorded probe: deny first, allow second.
				return Decision{Message: "PROBE-DENY: retry the exact same Write once more."}, nil
			}
			return Decision{Allow: true, UpdatedInput: input}, nil
		},
	}

	for _, line := range sessionLines {
		if err := s.handle([]byte(line)); err != nil {
			t.Fatalf("handle(%.60s): %v", line, err)
		}
	}

	if calls != 2 {
		t.Fatalf("CanUseTool called %d times, want 2", calls)
	}
	if s.sessionID != "4ee01345-bb91-416a-ae01-53637c9732c2" {
		t.Errorf("sessionID = %q, want the recorded one", s.sessionID)
	}
	if !s.resultSeen || s.resErr != nil {
		t.Fatalf("result: seen=%t err=%v", s.resultSeen, s.resErr)
	}
	wantResp := "Done. File created at `hello.txt` with content \"hi\"."
	if s.res.Response != wantResp || s.res.Turns != 3 {
		t.Errorf("res = %+v, want Response=%q Turns=3", s.res, wantResp)
	}
	if s.res.InputTokens != 26+18216+80481 || s.res.OutputTokens != 530 {
		t.Errorf("tokens = %d/%d, want 98723/530", s.res.InputTokens, s.res.OutputTokens)
	}

	// The recorded parent stream is: initial user message, deny, allow,
	// interrupt probe. The replayed loop covers the two control responses.
	sentLines := strings.Split(strings.TrimSpace(sent.String()), "\n")
	if len(sentLines) != 2 {
		t.Fatalf("sent %d control responses, want 2:\n%s", len(sentLines), sent.String())
	}
	assertSameJSON(t, "deny response", parentLines[1], sentLines[0])
	assertSameJSON(t, "allow response", parentLines[2], sentLines[1])
}

func fixtureLines(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func assertSameJSON(t *testing.T, label, want, got string) {
	t.Helper()
	var w, g any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want fixture: %v", label, err)
	}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("%s: bad got line: %v", label, err)
	}
	if !reflect.DeepEqual(w, g) {
		t.Errorf("%s mismatch:\nwant %s\ngot  %s", label, want, got)
	}
}

// TestUserMessageEvent pins the initial-message framing against the recorded
// parent fixture.
func TestUserMessageEvent(t *testing.T) {
	t.Parallel()
	parentLines := fixtureLines(t, "claude_live_parent.jsonl")
	var recorded struct {
		Message struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(parentLines[0]), &recorded); err != nil {
		t.Fatal(err)
	}
	assertSameJSON(t, "user message", parentLines[0], string(userMessageEvent(recorded.Message.Content[0].Text)))
}

func TestControlEventEncoders(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(`{"file_path":"a.txt"}`)

	deny := string(controlResponseEvent("r1", input, Decision{}))
	if !strings.Contains(deny, `"behavior":"deny"`) || !strings.Contains(deny, "denied by squad sign-off gate") {
		t.Errorf("default deny = %s", deny)
	}
	allow := string(controlResponseEvent("r2", input, Decision{Allow: true}))
	if !strings.Contains(allow, `"behavior":"allow"`) || !strings.Contains(allow, `"updatedInput":{"file_path":"a.txt"}`) {
		t.Errorf("allow must carry the original input: %s", allow)
	}
	rewritten := string(controlResponseEvent("r3", input, Decision{Allow: true, UpdatedInput: json.RawMessage(`{"file_path":"b.txt"}`)}))
	if !strings.Contains(rewritten, `"updatedInput":{"file_path":"b.txt"}`) {
		t.Errorf("allow must prefer the rewrite: %s", rewritten)
	}
	interrupt := string(interruptEvent("i1"))
	if !strings.Contains(interrupt, `"subtype":"interrupt"`) || !strings.Contains(interrupt, `"request_id":"i1"`) {
		t.Errorf("interrupt = %s", interrupt)
	}
}
