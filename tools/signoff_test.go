package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/tmc/langchaingo/llms"
)

// signOffInvoke is a thin helper that calls proposePlanTool with the given
// runtime and plan. Returns the tool's response + error.
func signOffInvoke(t *testing.T, rt *SignOffRuntime, plan string) (string, error) {
	t.Helper()
	raw, err := json.Marshal(proposePlanArgs{Plan: plan})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return proposePlanTool(rt)(context.Background(), raw)
}

func ttySignOffRuntime(input string) *SignOffRuntime {
	return &SignOffRuntime{
		In:    strings.NewReader(input),
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
}

func TestProposePlan_RequiresPlan(t *testing.T) {
	if _, err := signOffInvoke(t, ttySignOffRuntime("yes\n"), ""); err == nil {
		t.Fatal("expected error when plan is empty")
	}
}

func TestProposePlan_ApproveUnlocks(t *testing.T) {
	for _, answer := range []string{"yes", "y", "Approve", "APPROVED"} {
		rt := ttySignOffRuntime(answer + "\n")
		out, err := signOffInvoke(t, rt, "add tests to pkg/foo")
		if err != nil {
			t.Fatalf("answer %q: %v", answer, err)
		}
		if !strings.Contains(out, "approved") {
			t.Errorf("answer %q: response should confirm approval: %q", answer, out)
		}
		if !rt.Approved() {
			t.Errorf("answer %q: gate should be unlocked after approval", answer)
		}
	}
}

func TestProposePlan_RejectErrorsAndStaysLocked(t *testing.T) {
	for _, answer := range []string{"no", "n", "reject", "abort", "q"} {
		rt := ttySignOffRuntime(answer + "\n")
		_, err := signOffInvoke(t, rt, "rm -rf everything")
		if err == nil {
			t.Fatalf("answer %q: expected rejection error", answer)
		}
		if !strings.Contains(err.Error(), "rejected") {
			t.Errorf("answer %q: error should mention rejection: %v", answer, err)
		}
		if rt.Approved() {
			t.Errorf("answer %q: gate must stay locked after rejection", answer)
		}
	}
}

func TestProposePlan_FeedbackReturnsTextAndStaysLocked(t *testing.T) {
	rt := ttySignOffRuntime("also update the README while you're in there\n")
	out, err := signOffInvoke(t, rt, "fix the parser bug")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "also update the README") {
		t.Errorf("feedback text should be relayed to the model: %q", out)
	}
	if !strings.Contains(out, "ProposePlan again") {
		t.Errorf("response should instruct a re-proposal: %q", out)
	}
	if rt.Approved() {
		t.Error("gate must stay locked while feedback is pending")
	}
}

func TestProposePlan_PastedMultiLineFeedbackKeptIntact(t *testing.T) {
	// A paste arrives fully buffered, so its interior blank line must not
	// terminate the feedback early.
	paste := "the plan misses two things:\n\n- handle symlinks\n- skip vendored dirs\n"
	rt := ttySignOffRuntime(paste)
	out, err := signOffInvoke(t, rt, "walk the tree")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"misses two things", "- handle symlinks", "- skip vendored dirs"} {
		if !strings.Contains(out, want) {
			t.Errorf("pasted feedback line %q lost: %q", want, out)
		}
	}
	if rt.Approved() {
		t.Error("gate must stay locked while feedback is pending")
	}
}

// chunkReader yields one chunk per Read call, simulating a human typing
// line-by-line (nothing buffered between lines).
type chunkReader struct{ chunks []string }

func (c *chunkReader) Read(p []byte) (int, error) {
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

func TestProposePlan_TypedFeedbackEndsOnEmptyLine(t *testing.T) {
	rt := &SignOffRuntime{
		In:    &chunkReader{chunks: []string{"rename the helper\n", "and add a test\n", "\n"}},
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	out, err := signOffInvoke(t, rt, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rename the helper\nand add a test") {
		t.Errorf("typed multi-line feedback should join lines: %q", out)
	}
}

func TestProposePlan_ReaderPersistsAcrossRounds(t *testing.T) {
	// Round 1: feedback terminated by an empty line at rest. Round 2: the
	// SAME runtime must cleanly read the approval — nothing from round 1
	// may leak, and the persistent reader must not have swallowed it.
	rt := &SignOffRuntime{
		In:    &chunkReader{chunks: []string{"tweak the naming\n", "\n", "yes\n"}},
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	out, err := signOffInvoke(t, rt, "plan v1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tweak the naming") {
		t.Fatalf("round 1 should return feedback: %q", out)
	}
	if rt.Approved() {
		t.Fatal("gate must stay locked after feedback")
	}
	if _, err := signOffInvoke(t, rt, "plan v2"); err != nil {
		t.Fatal(err)
	}
	if !rt.Approved() {
		t.Error("round 2 approval should unlock the gate")
	}
}

func TestProposePlan_FeedbackEndsOnEOF(t *testing.T) {
	rt := ttySignOffRuntime("just the one note") // no trailing newline, then EOF
	out, err := signOffInvoke(t, rt, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "just the one note") {
		t.Errorf("feedback before EOF should be kept: %q", out)
	}
}

func TestProposePlan_RendersPlanAndPrompt(t *testing.T) {
	out := &bytes.Buffer{}
	rt := &SignOffRuntime{
		In:    strings.NewReader("yes\n"),
		Out:   out,
		IsTTY: func() bool { return true },
	}
	if _, err := signOffInvoke(t, rt, "the plan body"); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "the plan body") {
		t.Errorf("plan not printed: %q", rendered)
	}
	if !strings.Contains(rendered, "Approve this plan?") {
		t.Errorf("review prompt not printed: %q", rendered)
	}
}

func TestProposePlan_BlankLineReprompts(t *testing.T) {
	rt := ttySignOffRuntime("\n\nyes\n")
	if _, err := signOffInvoke(t, rt, "plan"); err != nil {
		t.Fatalf("blank lines should re-prompt, not fail: %v", err)
	}
	if !rt.Approved() {
		t.Error("gate should unlock after the eventual approval")
	}
}

func TestProposePlan_EOFErrors(t *testing.T) {
	rt := ttySignOffRuntime("") // immediate EOF, no bytes
	if _, err := signOffInvoke(t, rt, "plan"); err == nil {
		t.Fatal("expected read error on EOF")
	}
}

func TestProposePlan_NonTTYErrors(t *testing.T) {
	cases := map[string]*SignOffRuntime{
		"nil runtime": nil,
		"nil IsTTY":   {In: strings.NewReader("yes\n"), Out: &bytes.Buffer{}},
		"false IsTTY": {In: strings.NewReader("yes\n"), Out: &bytes.Buffer{}, IsTTY: func() bool { return false }},
	}
	for name, rt := range cases {
		if _, err := signOffInvoke(t, rt, "plan"); err == nil {
			t.Errorf("%s: expected error — sign-off has no non-TTY policy", name)
		}
	}
}

func TestProposePlan_NilStdinErrors(t *testing.T) {
	rt := &SignOffRuntime{Out: &bytes.Buffer{}, IsTTY: func() bool { return true }}
	if _, err := signOffInvoke(t, rt, "plan"); err == nil {
		t.Fatal("expected error for nil stdin")
	}
}

func TestProposePlan_WriteErrorSurfaces(t *testing.T) {
	rt := &SignOffRuntime{
		In:    strings.NewReader("yes\n"),
		Out:   errWriter{},
		IsTTY: func() bool { return true },
	}
	if _, err := signOffInvoke(t, rt, "plan"); err == nil {
		t.Fatal("expected write error to surface")
	}
}

func TestProposePlan_InvalidJSONArgs(t *testing.T) {
	if _, err := proposePlanTool(ttySignOffRuntime("yes\n"))(context.Background(), []byte("not json")); err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestSignOffDenial_LocksMutatingToolsUntilApproved(t *testing.T) {
	rt := ttySignOffRuntime("yes\n")
	ctx := WithSignOffRuntime(context.Background(), rt)

	for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
		if denial := SignOffDenial(ctx, tool); denial == "" {
			t.Errorf("%s should be denied before approval", tool)
		} else if !strings.Contains(denial, "ProposePlan") {
			t.Errorf("%s denial should point at ProposePlan: %q", tool, denial)
		}
	}
	for _, tool := range []string{"Read", "Grep", "Glob", "Bash", "ProposePlan"} {
		if denial := SignOffDenial(ctx, tool); denial != "" {
			t.Errorf("%s should not be gated: %q", tool, denial)
		}
	}

	if _, err := signOffInvoke(t, rt, "plan"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
		if denial := SignOffDenial(ctx, tool); denial != "" {
			t.Errorf("%s should be allowed after approval: %q", tool, denial)
		}
	}
}

func TestSignOffDenial_NoRuntimeNoGate(t *testing.T) {
	if denial := SignOffDenial(context.Background(), "Write"); denial != "" {
		t.Errorf("runs without a sign-off gate must not deny: %q", denial)
	}
}

func TestExecuteToolCall_DeniedPendingSignOff(t *testing.T) {
	ctx := WithSignOffRuntime(context.Background(), ttySignOffRuntime(""))
	handlers := map[string]Handler{
		"Write": {Def: definitionWrite(), Call: func(context.Context, []byte) (string, error) {
			t.Fatal("handler must not run while the gate is locked")
			return "", nil
		}},
	}
	resp := executeToolCall(ctx, llms.ToolCall{
		ID: "1",
		FunctionCall: &llms.FunctionCall{
			Name:      "Write",
			Arguments: `{"file_path":"/tmp/x","content":"y"}`,
		},
	}, handlers)
	if !strings.Contains(resp.Content, "ProposePlan") {
		t.Errorf("denial should instruct the model to propose a plan: %q", resp.Content)
	}
}

func TestBuildHandlers_ProposePlanRegistrationGated(t *testing.T) {
	wd := t.TempDir()
	handlers, _ := buildHandlersWithSkill(wd, nil, nil, nil, nil, nil)
	if _, ok := handlers["ProposePlan"]; ok {
		t.Error("ProposePlan should not register when runtime is nil")
	}
	handlers, _ = buildHandlersWithSkill(wd, nil, nil, nil, nil, &SignOffRuntime{})
	if _, ok := handlers["ProposePlan"]; !ok {
		t.Error("ProposePlan should register when runtime is present")
	}
}

// scriptedLLM returns canned responses in order and records the messages
// passed to each call, so tests can assert what the model actually saw.
type scriptedLLM struct {
	responses []*llms.ContentResponse
	calls     int
	msgs      [][]llms.MessageContent
}

func (s *scriptedLLM) GenerateContent(_ context.Context, msgs []llms.MessageContent, _ ...llms.CallOption) (*llms.ContentResponse, error) {
	s.msgs = append(s.msgs, msgs)
	if s.calls >= len(s.responses) {
		return nil, errors.New("no scripted response left")
	}
	resp := s.responses[s.calls]
	s.calls++
	return resp, nil
}

func (s *scriptedLLM) Call(context.Context, string, ...llms.CallOption) (string, error) {
	return "", nil
}

func toolCallResp(id, name, args string) *llms.ContentResponse {
	return &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		ToolCalls: []llms.ToolCall{{
			ID:           id,
			Type:         "function",
			FunctionCall: &llms.FunctionCall{Name: name, Arguments: args},
		}},
	}}}
}

// TestRunWithTools_SignOffGateIntegration drives the full tool loop through
// a complete review cycle: a premature Write is denied, a first plan draws
// feedback, the revised plan is approved, and only then does a Write land
// on disk.
func TestRunWithTools_SignOffGateIntegration(t *testing.T) {
	dir := t.TempDir()
	rt := &SignOffRuntime{
		In:    &chunkReader{chunks: []string{"scope it down\n", "\n", "yes\n"}},
		Out:   &bytes.Buffer{},
		IsTTY: func() bool { return true },
	}
	ctx := WithSignOffRuntime(context.Background(), rt)

	llm := &scriptedLLM{responses: []*llms.ContentResponse{
		toolCallResp("1", "Write", `{"path":"early.txt","content":"should not land"}`),
		toolCallResp("2", "ProposePlan", `{"plan":"write approved.txt with a greeting"}`),
		toolCallResp("3", "ProposePlan", `{"plan":"write approved.txt with a SHORT greeting"}`),
		toolCallResp("4", "Write", `{"path":"approved.txt","content":"hello"}`),
		{Choices: []*llms.ContentChoice{{Content: "done"}}},
	}}

	out, err := RunWithTools(ctx, llm, "", "user", dir, RunWithToolsConfig{
		MaxIterations: 6,
		Executor:      &executor.LocalExecutor{WorkingDir: dir},
	})
	if err != nil {
		t.Fatalf("RunWithTools() error = %v", err)
	}
	if out != "done" {
		t.Fatalf("output = %q, want done", out)
	}

	if _, err := os.Stat(filepath.Join(dir, "early.txt")); !os.IsNotExist(err) {
		t.Error("Write before approval must not create the file")
	}
	content, err := os.ReadFile(filepath.Join(dir, "approved.txt"))
	if err != nil {
		t.Fatalf("Write after approval should create the file: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("approved.txt = %q, want hello", content)
	}
	if !rt.Approved() {
		t.Error("gate should be unlocked after the approval round")
	}

	// Verify what the model saw at each step: the denial, the relayed
	// feedback, then the approval.
	if len(llm.msgs) != 5 {
		t.Fatalf("expected 5 model calls, got %d", len(llm.msgs))
	}
	for i, want := range map[int]string{
		1: "locked until a plan is approved",
		2: "scope it down",
		3: "approved by the user",
	} {
		if feed := fmt.Sprintf("%+v", llm.msgs[i]); !strings.Contains(feed, want) {
			t.Errorf("model call %d should have seen %q", i, want)
		}
	}
}

func TestWithSignOffRuntime_RoundTrip(t *testing.T) {
	rt := &SignOffRuntime{}
	ctx := WithSignOffRuntime(context.Background(), rt)
	if got := GetSignOffRuntime(ctx); got != rt {
		t.Fatalf("expected runtime back, got %+v", got)
	}
	if got := GetSignOffRuntime(context.Background()); got != nil {
		t.Fatalf("expected nil for empty ctx, got %+v", got)
	}
	if got := GetSignOffRuntime(WithSignOffRuntime(context.Background(), nil)); got != nil {
		t.Fatalf("nil runtime should be a no-op, got %+v", got)
	}
}
