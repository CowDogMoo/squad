package runner

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cowdogmoo/squad/activity"
	"github.com/cowdogmoo/squad/agenticcli"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/tools"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// claudeFlushDelay is how long a message may go without another block before
// its tool calls are recorded. A message's blocks arrive in one burst and the
// CLI runs its tools only after the message ends, so a short quiet period
// means the message is complete; without it a slow tool call would not show
// up in events.jsonl until its result arrived. Var so tests can change it.
var claudeFlushDelay = 750 * time.Millisecond

// claudeActivity turns a claude-code stream into the same activity record
// (turn and tool spans, iteration/response/tool_call/tool_result events) the
// native tool loop writes. A turn is one model message; its tool calls are
// recorded once the message is complete, which the stream signals by moving
// on to anything other than more blocks of that message, or by a quiet
// period of claudeFlushDelay.
//
// Subagent messages (parent_tool_use_id set) form their own sequence of
// turns nested under the span of the tool call that started the subagent.
type claudeActivity struct {
	ctx context.Context
	rec activity.Recorder

	mu sync.Mutex
	// threads holds per-thread turn state, keyed by parent_tool_use_id
	// ("" for the main agent).
	threads map[string]*claudeThread
	// tools holds the tool calls that have started and not yet finished.
	tools map[string]*claudeTool
	// thinking is the reasoning-token estimate for a generation whose
	// message has not arrived yet.
	thinking int64
	started  time.Time
	// flushTimer records pending tool calls after a quiet period; closed
	// stops it from firing once the run is over.
	flushTimer *time.Timer
	closed     bool
}

type claudeThread struct {
	parentID string
	index    int
	turn     *activity.Turn
	msgID    string
	start    time.Time
	last     time.Time
	text     []string
	thinking []string
	tokens   int64
	usage    agenticcli.MessageUsage
	stop     string
	pending  []pendingToolUse
	recorded bool
}

type pendingToolUse struct {
	use agenticcli.ToolUse
	at  time.Time
}

type claudeTool struct {
	tool     *activity.Tool
	ctx      context.Context
	threadID string
}

func newClaudeActivity(ctx context.Context, model string) *claudeActivity {
	return &claudeActivity{
		ctx:     ctx,
		rec:     activity.Recorder{Provider: "claude-code", Model: model},
		threads: map[string]*claudeThread{},
		tools:   map[string]*claudeTool{},
		started: time.Now(),
	}
}

// SessionStarted records the CLI's session id and, when squad passed no
// --model, the model the CLI resolved.
func (a *claudeActivity) SessionStarted(sessionID, model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rec.Model == "" {
		a.rec.Model = model
	}
	session.FromContext(a.ctx).SetClaudeSessionID(sessionID)
	trace.SpanFromContext(a.ctx).SetAttributes(attribute.String("squad.claude.session_id", sessionID))
}

// ThinkingTokens attaches the running reasoning estimate to the main
// agent's message in progress, or holds it for the next one.
func (a *claudeActivity) ThinkingTokens(estimated int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if th := a.threads[""]; th != nil && th.turn != nil && !th.recorded {
		th.tokens = max(th.tokens, estimated)
		return
	}
	a.thinking = max(a.thinking, estimated)
}

// Assistant folds one assistant event into its thread's current turn,
// starting a new turn when the message id changes.
func (a *claudeActivity) Assistant(msg agenticcli.AssistantMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	th := a.thread(msg.ParentToolUseID)
	if th.turn == nil || th.msgID != msg.ID {
		a.closeTurn(th, msg.Time)
		a.openTurn(th, msg)
	}
	th.text = append(th.text, msg.Text...)
	for _, t := range msg.Thinking {
		if t != "" {
			th.thinking = append(th.thinking, t)
		}
	}
	for _, use := range msg.ToolUses {
		th.pending = append(th.pending, pendingToolUse{use: use, at: msg.Time})
	}
	if len(msg.ToolUses) > 0 {
		a.armFlush()
	}
	if msg.Usage != (agenticcli.MessageUsage{}) {
		th.usage = msg.Usage
	}
	if msg.StopReason != "" {
		th.stop = msg.StopReason
	}
	th.last = msg.Time
}

// ToolResults finishes the tool calls the results belong to.
func (a *claudeActivity) ToolResults(results []agenticcli.ToolResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushAll()
	for _, r := range results {
		ct := a.tools[r.ToolUseID]
		if ct == nil {
			continue
		}
		delete(a.tools, r.ToolUseID)
		if th := a.threads[ct.threadID]; th != nil && r.Time.After(th.last) {
			th.last = r.Time
		}
		if denied(r.PermissionDecision) {
			ct.tool.DenyAt(activity.DeniedByPermission, r.Content, r.Time)
			continue
		}
		res := activity.Result{OutputBytes: len(r.Content), End: r.Time}
		if r.IsError {
			res.ErrText = r.Content
			if res.ErrText == "" {
				res.ErrText = "tool returned an error"
			}
		}
		ct.tool.Finish(res)
	}
}

// ToolDenied records a refusal from squad's live sign-off gate.
func (a *claudeActivity) ToolDenied(toolUseID, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.flushAll()
	ct := a.tools[toolUseID]
	if ct == nil {
		return
	}
	delete(a.tools, toolUseID)
	ct.tool.Deny(activity.DeniedSignOff, message)
}

// Finished closes every open turn at the result event.
func (a *claudeActivity) Finished(at time.Time) {
	a.close(at)
}

// close ends whatever is still open: tool calls that never got a result are
// finished with an error, then every turn is recorded and ended. Safe to
// call more than once.
func (a *claudeActivity) close(at time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if a.flushTimer != nil {
		a.flushTimer.Stop()
	}
	a.flushAll()
	for id, ct := range a.tools {
		ct.tool.Finish(activity.Result{ErrText: "no tool result before the run ended", End: at})
		delete(a.tools, id)
	}
	for _, th := range a.threads {
		a.closeTurn(th, at)
	}
}

// armFlush (re)starts the quiet-period timer that records pending tool
// calls. Called with a.mu held.
func (a *claudeActivity) armFlush() {
	if a.flushTimer != nil {
		a.flushTimer.Stop()
	}
	a.flushTimer = time.AfterFunc(claudeFlushDelay, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.closed {
			a.flushAll()
		}
	})
}

func (a *claudeActivity) thread(parentID string) *claudeThread {
	th := a.threads[parentID]
	if th == nil {
		th = &claudeThread{parentID: parentID, last: a.started}
		a.threads[parentID] = th
	}
	return th
}

// openTurn starts the thread's next turn. The model began generating it when
// the thread's previous activity ended, so that is the turn's start time.
func (a *claudeActivity) openTurn(th *claudeThread, msg agenticcli.AssistantMessage) {
	parent := a.ctx
	if th.parentID != "" {
		if ct := a.tools[th.parentID]; ct != nil {
			parent = ct.ctx
		}
	}
	start := th.last
	if start.IsZero() || start.After(msg.Time) {
		start = msg.Time
	}
	th.index++
	th.turn = a.rec.StartTurn(parent, th.index, start)
	th.msgID = msg.ID
	th.start = start
	th.text, th.thinking, th.pending = nil, nil, nil
	th.usage, th.stop, th.recorded = agenticcli.MessageUsage{}, "", false
	th.tokens = 0
	if th.parentID == "" {
		th.tokens, a.thinking = a.thinking, 0
	}
}

// flushAll records every thread's completed message and starts its tool
// calls. Called whenever the stream moves past assistant blocks.
func (a *claudeActivity) flushAll() {
	for _, th := range a.threads {
		a.flush(th)
	}
}

// flush records the thread's turn the first time it runs, then starts any
// tool calls still pending. A tool_use block that arrives after the turn was
// recorded still gets its call started, though the turn's tool_calls count
// does not include it.
func (a *claudeActivity) flush(th *claudeThread) {
	if th.turn == nil {
		return
	}
	if !th.recorded {
		a.record(th)
	}
	a.startPending(th)
}

func (a *claudeActivity) record(th *claudeThread) {
	th.recorded = true
	th.turn.RecordAt(activity.TurnResult{
		Text:           strings.Join(th.text, "\n"),
		Thinking:       strings.Join(th.thinking, "\n"),
		ThinkingTokens: th.tokens,
		FinishReason:   th.stop,
		ToolCalls:      len(th.pending),
		Usage: activity.Usage{
			InputTokens:      th.usage.InputTokens,
			CacheReadTokens:  th.usage.CacheReadInputTokens,
			CacheWriteTokens: th.usage.CacheCreationInputTokens,
			OutputTokens:     th.usage.OutputTokens,
		},
		ParentToolUseID: th.parentID,
	}, th.last)
}

func (a *claudeActivity) startPending(th *claudeThread) {
	for _, p := range th.pending {
		args := string(p.use.Input)
		ctx, tool := activity.StartTool(th.turn.Context(), activity.ToolCall{
			Iteration:       th.index,
			CallID:          p.use.ID,
			Name:            p.use.Name,
			Args:            args,
			Summary:         claudeArgsSummary(p.use.Name, p.use.Input),
			Start:           p.at,
			ParentToolUseID: th.parentID,
		})
		a.tools[p.use.ID] = &claudeTool{tool: tool, ctx: ctx, threadID: th.parentID}
	}
	th.pending = nil
}

// closeTurn records and ends the thread's current turn at its last activity.
func (a *claudeActivity) closeTurn(th *claudeThread, at time.Time) {
	if th.turn == nil {
		return
	}
	a.flush(th)
	end := th.last
	if end.IsZero() || end.Before(th.start) {
		end = at
	}
	th.turn.EndAt(end)
	th.turn = nil
}

// denied reports whether a permission decision the CLI recorded refused the
// call. An absent decision means the CLI did not report one.
func denied(decision string) bool {
	switch strings.ToLower(decision) {
	case "", "accept", "allow", "approved":
		return false
	}
	return true
}

// claudeArgSummaryKeys are the claude-code argument names that describe a
// call in one line, tried in order when squad's own summary has nothing.
var claudeArgSummaryKeys = []string{"command", "file_path", "path", "pattern", "url", "query", "description"}

// claudeArgsSummary is squad's tool-argument summary extended to Claude
// Code's argument names (file_path for Read/Write/Edit, url for WebFetch).
func claudeArgsSummary(name string, input json.RawMessage) string {
	if s := tools.ToolArgsSummary(name, string(input)); s != "" {
		return s
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}
	for _, k := range claudeArgSummaryKeys {
		if v, ok := args[k].(string); ok && v != "" {
			return tools.TruncateString(v, 200)
		}
	}
	return ""
}
