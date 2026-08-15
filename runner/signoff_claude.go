// Sign-off gate adapter for the claude-code live path.
//
// The native providers enforce --interactive inside squad's tool dispatchers
// (ProposePlan + SignOffDenial). Claude Code runs its own tool loop in a
// separate process, so the gate rides the CLI's stream-json control protocol
// instead: every permission-gated tool call surfaces as a can_use_tool
// request, and this adapter answers it from the same tools.SignOffRuntime
// the native gate uses. There is no ProposePlan tool on this path — the deny
// message plays its role, steering the model to write its plan as prose,
// which then becomes the text the reviewer sees at the terminal.

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
)

// claudeMutatingTools are the Claude Code tools the sign-off gate locks:
// squad's own mutating set plus claude's NotebookEdit, mirroring the
// readonly-mode restriction. Bash stays allowed, matching the native gate's
// semantics (prompt-level discouragement only — a known, documented gap
// shared with readonly mode).
var claudeMutatingTools = map[string]bool{
	"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true,
}

const (
	denyNoPlanMsg = "This run requires human sign-off before file modifications. " +
		"Write out your complete plan (what you will change, which files, and why) " +
		"as a normal message, then retry the edit."
	denyRejectedMsg = "Plan rejected by the user. Make no changes; summarize your findings and stop."
)

// claudeSignOffGate adapts tools.SignOffRuntime into an agenticcli.CanUseTool
// callback, tracking the rejected state so a refused plan keeps every later
// mutating call locked without re-prompting the reviewer.
type claudeSignOffGate struct {
	rt *tools.SignOffRuntime

	mu       sync.Mutex
	rejected bool
}

// canUseTool implements the decision policy from the sign-off gate's
// semantics: non-mutating tools and approved runs pass, a locked gate with
// no plan text steers the model to write one, and a locked gate with a plan
// puts it in front of the reviewer.
func (g *claudeSignOffGate) canUseTool(ctx context.Context, toolName string, input json.RawMessage, assistantText string) (agenticcli.Decision, error) {
	if !claudeMutatingTools[toolName] {
		return agenticcli.Decision{Allow: true}, nil
	}
	if g.rt.Approved() {
		g.logDecision(ctx, toolName, true, "plan-approved")
		return agenticcli.Decision{Allow: true}, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.rejected {
		g.logDecision(ctx, toolName, false, "plan-rejected")
		return agenticcli.Decision{Message: denyRejectedMsg}, nil
	}
	plan := strings.TrimSpace(assistantText)
	if plan == "" {
		g.logDecision(ctx, toolName, false, "no-plan")
		return agenticcli.Decision{Message: denyNoPlanMsg}, nil
	}

	outcome, feedback, err := g.rt.ReviewPlan(renderClaudePlan(plan, toolName, input))
	logClaudeSignOffResolution(ctx, plan, outcome, feedback, err)
	if err != nil {
		return agenticcli.Decision{}, err
	}
	switch outcome {
	case tools.ReviewApproved:
		g.logDecision(ctx, toolName, true, "plan-approved")
		return agenticcli.Decision{Allow: true}, nil
	case tools.ReviewRejected:
		g.rejected = true
		g.logDecision(ctx, toolName, false, "plan-rejected")
		return agenticcli.Decision{Message: denyRejectedMsg, Interrupt: true}, nil
	default:
		g.logDecision(ctx, toolName, false, "feedback")
		return agenticcli.Decision{Message: fmt.Sprintf(
			"Plan not approved. The user replied with feedback:\n\n%s\n\n"+
				"Revise your plan to address the feedback, write it out as a message, "+
				"and retry the edit. File modifications remain locked until a plan is approved.",
			feedback)}, nil
	}
}

// runAgenticCLI executes one CLI invocation, dispatching to the live
// permission-protocol session when a sign-off gate is armed on ctx (only
// claude-code is routed here gate-armed) and to single-shot print mode
// otherwise. The returned gate is non-nil only on the live path.
func runAgenticCLI(ctx context.Context, req agenticcli.Request) (agenticcli.Result, *claudeSignOffGate, error) {
	rt := tools.GetSignOffRuntime(ctx)
	if rt == nil {
		res, err := agenticcli.Run(ctx, req)
		return res, nil, err
	}
	gate := &claudeSignOffGate{rt: rt}
	res, err := agenticcli.RunLive(ctx, agenticcli.LiveRequest{Request: req, CanUseTool: gate.canUseTool})
	if err == nil && !rt.Approved() && !gate.wasRejected() {
		logging.InfoContext(ctx, "interactive run ended without any file-modification attempt; no plan was reviewed")
	}
	return res, gate, err
}

// wasRejected reports whether the reviewer refused a plan during the run.
func (g *claudeSignOffGate) wasRejected() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rejected
}

// renderClaudePlan is what the reviewer sees: the model's prose plan plus
// the tool call that is waiting on the decision.
func renderClaudePlan(plan, toolName string, input json.RawMessage) string {
	return fmt.Sprintf("%s\n\n── pending tool call: %s ──\n%s",
		plan, toolName, tools.TruncateString(string(input), 600))
}

// logDecision records one can_use_tool answer as a permission_decision
// session event, giving the live path the same audit shape as the native
// dispatchers. Safe on a nil session logger.
func (g *claudeSignOffGate) logDecision(ctx context.Context, toolName string, allowed bool, reason string) {
	decision := "deny"
	if allowed {
		decision = "allow"
	}
	if err := session.FromContext(ctx).Append(session.EventPermissionDecision, map[string]any{
		"tool":     toolName,
		"decision": decision,
		"reason":   reason,
	}); err != nil {
		logging.WarnContext(ctx, "failed to log permission_decision event: %v", err)
	}
	logging.DebugContext(ctx, "claude live gate: %s %s (%s)", decision, toolName, reason)
}

// logClaudeSignOffResolution mirrors the native ProposePlan audit event for
// plan reviews held on the live path.
func logClaudeSignOffResolution(ctx context.Context, plan, outcome, feedback string, resolveErr error) {
	if outcome == "" {
		outcome = "error"
	}
	payload := map[string]any{
		"plan":       tools.TruncateString(plan, 4000),
		"resolution": outcome,
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
}
