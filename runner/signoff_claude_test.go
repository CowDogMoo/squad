package runner

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/internal/fakeclaude"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

// TestHelperFakeClaude is not a test: it is the body of the fake `claude`
// binary the shim installed by fakeclaude.Install re-execs into.
func TestHelperFakeClaude(t *testing.T) {
	if os.Getenv("FAKE_CLAUDE") != "1" {
		t.Skip("helper process for the fake claude shim")
	}
	fakeclaude.Main()
}

// signOffChunkReader yields one chunk per Read call, simulating a human
// typing lines at the sign-off prompt (mirrors the tools package tests).
type signOffChunkReader struct{ chunks []string }

func (c *signOffChunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	if n < len(c.chunks[0]) {
		c.chunks[0] = c.chunks[0][n:]
	} else {
		c.chunks = c.chunks[1:]
	}
	return n, nil
}

func scriptedSignOff(out *bytes.Buffer, answers ...string) *tools.SignOffRuntime {
	return &tools.SignOffRuntime{
		In:    &signOffChunkReader{chunks: answers},
		Out:   out,
		IsTTY: func() bool { return true },
	}
}

// liveGateCtx builds the run context the live path sees: edits tracking, the
// sign-off gate, and a real session logger whose events.jsonl the tests
// assert against.
func liveGateCtx(t *testing.T, repo string, rt *tools.SignOffRuntime) (context.Context, *session.Logger) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger, err := session.New(repo, "", "agent", "claude-code", "", "task")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	ctx := tools.WithSignOffRuntime(tools.InitEdits(context.Background()), rt)
	return session.WithLogger(ctx, logger), logger
}

func readSessionEvents(t *testing.T, logger *session.Logger) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(logger.Dir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
	}
	return string(raw)
}

// TestInvokeAgenticCLI_LiveGateFullLoop drives the full deny-steer →
// feedback → approve loop through invokeAgenticCLI against the fake claude
// binary, mirroring TestRunWithTools_SignOffGateIntegration for the native
// path.
func TestInvokeAgenticCLI_LiveGateFullLoop(t *testing.T) {
	repo := initTestGitRepo(t)
	fakeclaude.Install(t, map[string]string{
		"FAKE_CLAUDE_ASK_BEFORE_TEXT": "1", // first Write attempt arrives with no plan prose
		"FAKE_CLAUDE_WRITE_FILE":      "hello.txt",
		"FAKE_CLAUDE_MAX_ASKS":        "4",
	})
	var tty bytes.Buffer
	rt := scriptedSignOff(&tty, "add tests please\n", "\n", "yes\n")
	ctx, logger := liveGateCtx(t, repo, rt)

	bundle := &agent.Bundle{WorkDir: repo, User: "create hello.txt"}
	m := metrics.New("claude-code", "")
	resp, m, err := invokeAgenticCLI(ctx, &RunOptions{Interactive: true}, "claude-code", "", "sys", bundle, m)
	if err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	if resp != "fake done" {
		t.Errorf("response = %q, want %q", resp, "fake done")
	}

	// Ask 1: no plan text yet → denied without prompting the reviewer.
	// Ask 2: plan text present → reviewer replies with feedback.
	// Ask 3: revised plan → reviewer approves → the Write lands.
	if rt.Approved() != true {
		t.Error("gate should be approved after the yes answer")
	}
	if _, err := os.Stat(filepath.Join(repo, "hello.txt")); err != nil {
		t.Errorf("approved Write did not land: %v", err)
	}
	if !tools.EditsApplied(ctx) {
		t.Error("EditsApplied = false, want true after the tree changed")
	}
	if m.Iterations() != 3 {
		t.Errorf("iterations = %d, want 3 (deny-steer, feedback, approve)", m.Iterations())
	}

	prompt := tty.String()
	if !strings.Contains(prompt, "plan attempt 2") {
		t.Errorf("TTY never saw the model's plan prose:\n%s", prompt)
	}
	if !strings.Contains(prompt, "pending tool call: Write") {
		t.Errorf("TTY never saw the pending tool call:\n%s", prompt)
	}
	// The feedback round-trips: the model's revised plan (shown at the
	// second review) embeds the reviewer's words.
	if !strings.Contains(prompt, "add tests please") {
		t.Errorf("revised plan never surfaced the feedback:\n%s", prompt)
	}

	events := readSessionEvents(t, logger)
	for _, want := range []string{
		`"reason":"no-plan"`, `"reason":"feedback"`, `"reason":"plan-approved"`,
		session.EventPermissionDecision,
		`"resolution":"feedback"`, `"resolution":"approved"`,
	} {
		if !strings.Contains(events, want) {
			t.Errorf("events.jsonl missing %s", want)
		}
	}
}

// TestInvokeAgenticCLI_LiveGateRejection pins the reject path: deny with the
// rejection message, interrupt the CLI, keep the gate locked, and end with
// the CLI's summary — no edits.
func TestInvokeAgenticCLI_LiveGateRejection(t *testing.T) {
	repo := initTestGitRepo(t)
	fakeclaude.Install(t, map[string]string{"FAKE_CLAUDE_WRITE_FILE": "hello.txt"})
	var tty bytes.Buffer
	rt := scriptedSignOff(&tty, "no\n")
	ctx, logger := liveGateCtx(t, repo, rt)

	bundle := &agent.Bundle{WorkDir: repo, User: "create hello.txt"}
	m := metrics.New("claude-code", "")
	_, m, err := invokeAgenticCLI(ctx, &RunOptions{Interactive: true}, "claude-code", "", "sys", bundle, m)
	if err == nil || !strings.Contains(err.Error(), "plan rejected by the user") {
		t.Fatalf("err = %v, want the rejection error", err)
	}
	if m.Iterations() == 0 {
		t.Error("iterations = 0, want the interrupted turns still recorded")
	}
	if rt.Approved() {
		t.Error("gate must stay locked after rejection")
	}
	if _, err := os.Stat(filepath.Join(repo, "hello.txt")); !os.IsNotExist(err) {
		t.Errorf("rejected run must not edit files (stat err = %v)", err)
	}
	events := readSessionEvents(t, logger)
	if !strings.Contains(events, `"reason":"plan-rejected"`) || !strings.Contains(events, `"resolution":"rejected"`) {
		t.Errorf("events.jsonl missing the rejection audit trail:\n%s", events)
	}
}

// TestInvokeModel_InteractiveClaudeCodeGoesLive verifies the InvokeModel
// routing: a gate-armed claude-code run reaches RunLive (and succeeds via
// the fake binary) instead of being rejected.
func TestInvokeModel_InteractiveClaudeCodeGoesLive(t *testing.T) {
	fakeclaude.Install(t, nil)
	var tty bytes.Buffer
	rt := scriptedSignOff(&tty, "yes\n")
	ctx := tools.WithSignOffRuntime(context.Background(), rt)

	opts := &RunOptions{Provider: "claude-code"}
	bundle := &agent.Bundle{System: "sys", User: "user", WorkDir: t.TempDir()}
	resp, m, err := InvokeModel(ctx, opts, bundle)
	if err != nil {
		t.Fatalf("InvokeModel: %v", err)
	}
	if resp != "fake done" {
		t.Errorf("response = %q, want %q", resp, "fake done")
	}
	if m.Iterations() != 1 {
		t.Errorf("iterations = %d, want 1", m.Iterations())
	}
}

// TestApplyProviderConstraints_InteractiveAgenticCLI pins the narrowed
// fail-fast: agy + --interactive is still rejected before the session opens,
// while claude-code + --interactive proceeds to the live path.
func TestApplyProviderConstraints_InteractiveAgenticCLI(t *testing.T) {
	t.Parallel()
	cmd := &cobra.Command{}
	bundle := &agent.Bundle{}

	err := applyProviderConstraints(cmd, &RunOptions{Interactive: true, Provider: "agy"}, bundle, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not supported with agentic CLI provider") {
		t.Errorf("agy: err = %v, want the fail-fast rejection", err)
	}
	if err := applyProviderConstraints(cmd, &RunOptions{Interactive: true, Provider: "claude-code"}, bundle, t.TempDir()); err != nil {
		t.Errorf("claude-code: err = %v, want nil (live path supported)", err)
	}
}

// TestSignOffPromptBlockCLI pins the live-path system prompt variant: it
// must not reference the ProposePlan tool (which doesn't exist in the CLI
// session) and must cover claude's NotebookEdit.
func TestSignOffPromptBlockCLI(t *testing.T) {
	t.Parallel()
	if strings.Contains(signOffPromptBlockCLI, "ProposePlan") {
		t.Error("CLI prompt block must not mention ProposePlan")
	}
	if !strings.Contains(signOffPromptBlockCLI, "NotebookEdit") {
		t.Error("CLI prompt block should list NotebookEdit as locked")
	}
	if !strings.Contains(signOffPromptBlock, "ProposePlan") {
		t.Error("native prompt block must keep the ProposePlan instructions")
	}
}
