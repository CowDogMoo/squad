// ProposePlan tool — Terraform-style human sign-off gate for an agent run.
//
// When a run is started with --interactive, the mutating file tools (Write,
// Edit, MultiEdit) stay locked until the agent presents a plan via
// ProposePlan and a human approves it at the terminal. The reviewer can
// approve, reject, or reply with free-text feedback; feedback is returned to
// the model so it can revise the plan and propose again. One runtime is
// shared by every agent in the run — children spawned via the Task tool
// inherit it through ctx — so a single approval unlocks the whole tree.
//
// Unlike Confirm, sign-off has no non-TTY auto policy: a plan review without
// a human is meaningless, so the CLI rejects --interactive up front when
// stdin is not a terminal and the tool fails loudly if reached anyway.

package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
)

// SignOffRuntime is the per-run state for the ProposePlan tool.
type SignOffRuntime struct {
	// In is the source of reviewer input. Typically os.Stdin.
	In io.Reader
	// Out is where the plan and prompt are written. Typically os.Stderr
	// because stdout is often captured for the agent's final response.
	Out io.Writer
	// IsTTY, when non-nil, reports whether a human is attached. A nil or
	// false IsTTY makes ProposePlan fail rather than fall back to a policy.
	IsTTY func() bool

	// mu serializes prompt I/O so parallel shards or tool calls can't
	// interleave two plan reviews on one terminal.
	mu       sync.Mutex
	approved atomic.Bool
	// reader lazily wraps In and persists across ProposePlan calls so a
	// multi-line paste buffered during one review round isn't discarded
	// before the next.
	reader *bufio.Reader
}

// bufIn returns the persistent buffered reader over In, creating it on
// first use. Callers must hold mu.
func (r *SignOffRuntime) bufIn() *bufio.Reader {
	if r.reader == nil {
		r.reader = bufio.NewReader(r.In)
	}
	return r.reader
}

// Approved reports whether a plan has been signed off for this run.
func (r *SignOffRuntime) Approved() bool { return r.approved.Load() }

type signOffKey struct{}

// WithSignOffRuntime attaches r to ctx so the tool loop can gate mutating
// tools and register the ProposePlan tool. A nil r returns ctx unchanged.
func WithSignOffRuntime(ctx context.Context, r *SignOffRuntime) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, signOffKey{}, r)
}

// GetSignOffRuntime returns the runtime stored on ctx, or nil if the run has
// no sign-off gate.
func GetSignOffRuntime(ctx context.Context) *SignOffRuntime {
	r, _ := ctx.Value(signOffKey{}).(*SignOffRuntime)
	return r
}

// SignOffDenial returns a non-empty denial message when toolName is a
// mutating tool and the run's sign-off gate has not been approved yet.
// Both tool dispatchers (LangChain and Responses API) consult this before
// invoking a handler, making the gate hold even when the model ignores the
// sign-off instructions in its system prompt.
func SignOffDenial(ctx context.Context, toolName string) string {
	rt := GetSignOffRuntime(ctx)
	if rt == nil || rt.Approved() || !IsMutatingTool(toolName) {
		return ""
	}
	return fmt.Sprintf("error: %s is locked until a plan is approved (interactive sign-off is enabled). "+
		"Call ProposePlan with your intended changes and wait for user approval before modifying files", toolName)
}

// Reviewer answers that resolve a plan without being treated as feedback.
// Anything outside these sets is free-text feedback for the model.
var (
	signOffApproveAnswers = map[string]bool{"yes": true, "y": true, "approve": true, "approved": true}
	signOffRejectAnswers  = map[string]bool{"no": true, "n": true, "reject": true, "rejected": true, "abort": true, "quit": true, "q": true}
)

func definitionProposePlan() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "ProposePlan",
			Description: "Present your implementation plan for human sign-off. This run keeps Write/Edit/MultiEdit " +
				"locked until a plan is approved. The user either approves (unlocking modifications), rejects " +
				"(stop without changes), or replies with feedback — incorporate the feedback and call ProposePlan " +
				"again with the revised plan.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"plan": map[string]any{
						"type": "string",
						"description": "The full plan for the user to review: what you intend to change, which files, " +
							"and why. Concise but complete — this is everything the user sees before deciding.",
					},
				},
				"required": []string{"plan"},
			},
		},
	}
}

type proposePlanArgs struct {
	Plan string `json:"plan"`
}

// proposePlanTool returns the Handler.Call for the ProposePlan tool.
func proposePlanTool(runtime *SignOffRuntime) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args proposePlanArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse ProposePlan args: %w", err)
		}
		args.Plan = strings.TrimSpace(args.Plan)
		if args.Plan == "" {
			return "", errors.New("propose-plan: plan is required")
		}

		result, feedback, err := resolveSignOff(runtime, args.Plan)
		logSignOffResolution(ctx, args.Plan, result, feedback, err)
		if err != nil {
			return "", err
		}
		switch result {
		case "approved":
			return "Plan approved by the user. File modifications are now unlocked — proceed with the approved plan.", nil
		default: // feedback
			return fmt.Sprintf("Plan NOT approved. The user replied with feedback:\n\n%s\n\n"+
				"Revise your plan to address the feedback and call ProposePlan again. "+
				"File modifications remain locked until a plan is approved.", feedback), nil
		}
	}
}

// Review outcomes returned by ReviewPlan.
const (
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
	ReviewFeedback = "feedback"
)

// ReviewPlan renders plan at the terminal and classifies the reviewer's
// answer as ReviewApproved (latching the gate open), ReviewRejected, or
// ReviewFeedback (with the feedback text). Empty input lines re-prompt
// rather than erroring — an accidental Enter shouldn't cost a model
// round-trip. Used directly by the claude-code live path, where the plan is
// assistant prose instead of a ProposePlan call.
func (r *SignOffRuntime) ReviewPlan(plan string) (outcome, feedback string, err error) {
	if r == nil || r.IsTTY == nil || !r.IsTTY() {
		return "", "", errors.New("propose-plan: sign-off requires an interactive terminal")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	answer, err := promptSignOff(r, plan)
	if err != nil {
		return "", "", err
	}
	switch lower := strings.ToLower(answer); {
	case signOffApproveAnswers[lower]:
		r.approved.Store(true)
		return ReviewApproved, "", nil
	case signOffRejectAnswers[lower]:
		return ReviewRejected, "", nil
	default:
		feedback, err := readFeedbackBody(r, answer)
		if err != nil {
			return "", "", err
		}
		return ReviewFeedback, feedback, nil
	}
}

// resolveSignOff adapts ReviewPlan for the ProposePlan tool, where a
// rejection must surface as an error so the model stops.
func resolveSignOff(runtime *SignOffRuntime, plan string) (result, feedback string, err error) {
	if runtime == nil {
		return "", "", errors.New("propose-plan: sign-off requires an interactive terminal")
	}
	outcome, feedback, err := runtime.ReviewPlan(plan)
	if err != nil {
		return "", "", err
	}
	if outcome == ReviewRejected {
		return ReviewRejected, "", errors.New("propose-plan: plan rejected by the user; make no changes, summarize your findings, and stop")
	}
	return outcome, feedback, nil
}

// promptSignOff renders the plan with review instructions and reads the
// reviewer's answer, re-prompting on blank lines until EOF or input arrives.
func promptSignOff(runtime *SignOffRuntime, plan string) (string, error) {
	if runtime.In == nil {
		return "", errors.New("propose-plan: no stdin available for interactive prompt")
	}
	if runtime.Out != nil {
		var buf strings.Builder
		buf.WriteString("\n──── proposed plan ────\n")
		buf.WriteString(plan)
		buf.WriteString("\n───────────────────────\n")
		buf.WriteString("Approve this plan? [yes / no] — anything else is feedback: type or paste it, then finish with an empty line\n> ")
		if _, err := io.WriteString(runtime.Out, buf.String()); err != nil {
			return "", fmt.Errorf("propose-plan: write prompt: %w", err)
		}
	}
	reader := runtime.bufIn()
	for {
		line, err := reader.ReadString('\n')
		answer := strings.TrimSpace(line)
		if answer != "" {
			return answer, nil
		}
		if err != nil {
			return "", fmt.Errorf("propose-plan: read user response: %w", err)
		}
		if runtime.Out != nil {
			if _, werr := io.WriteString(runtime.Out, "> "); werr != nil {
				return "", fmt.Errorf("propose-plan: write prompt: %w", werr)
			}
		}
	}
}

// readFeedbackBody collects the rest of a feedback reply after its first
// line. Lines are consumed until the reviewer enters an empty line at rest
// or stdin hits EOF (ctrl-d). A blank line with more input already buffered
// is part of a paste, not the terminator, so multi-paragraph pastes survive
// intact. Callers must hold mu.
func readFeedbackBody(runtime *SignOffRuntime, first string) (string, error) {
	reader := runtime.bufIn()
	lines := []string{first}
	for {
		line, err := reader.ReadString('\n')
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" && err == nil && reader.Buffered() == 0 {
			break // empty line typed at rest — feedback complete
		}
		if trimmed != "" || err == nil {
			lines = append(lines, trimmed)
		}
		if err != nil {
			break // EOF (or read error) also ends the feedback
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), nil
}

// logSignOffResolution writes a session event capturing the proposed plan,
// how it resolved ("approved", "feedback", "rejected", or "error"), and any
// feedback text, so the audit trail records every review round. Safe on a
// nil session logger.
func logSignOffResolution(ctx context.Context, plan, result, feedback string, resolveErr error) {
	if result == "" {
		result = "error"
	}
	payload := map[string]any{
		"plan":       TruncateString(plan, 4000),
		"resolution": result,
	}
	if feedback != "" {
		payload["feedback"] = feedback
	}
	if resolveErr != nil {
		payload["error"] = resolveErr.Error()
	}
	if err := session.FromContext(ctx).Append(session.EventSignOffResolved, payload); err != nil {
		logging.WarnContext(ctx, "failed to log sign_off_resolved event: %v", err)
	}
	logging.DebugContext(ctx, "ProposePlan resolved: %s", result)
}
