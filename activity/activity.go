// Package activity records what an agent does, one model turn and one tool
// call at a time, as session events (events.jsonl) and OpenTelemetry spans.
// Every provider path feeds it the same way, so the event record and the
// span tree have one shape whether squad runs the tool loop itself (API
// providers) or reads it back from an agentic CLI (claude-code).
//
// events.jsonl is a local file and keeps full assistant text, tool
// arguments and tool errors. Spans may leave the machine, so they carry only
// names, timing, sizes, token counts and error status, never assistant text
// or tool output and, by default, no tool arguments. A failed tool call's
// span gets a fixed error status; SQUAD_OTEL_EXPORT_COMMANDS=1 opts in to
// the redacted, truncated text of the call's Go error, never to its output.
package activity

import (
	"context"
	"errors"
	"time"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Size caps for event payloads. events.jsonl stays local and is meant to
// hold full content; the caps only keep one runaway value from producing a
// line too long for readers such as `squad ui`.
const (
	maxEventTextBytes     = 64 * 1024
	maxEventThinkingBytes = 16 * 1024
	maxEventArgsBytes     = 64 * 1024
	maxEventErrorBytes    = 4 * 1024
)

// Denial reasons recorded on tool_denied events and the squad.tool.denied_reason
// span attribute.
const (
	DeniedReadOnly     = "readonly"
	DeniedSignOff      = "sign_off"
	DeniedSkillTools   = "skill_allowed_tools"
	DeniedByPermission = "permission"
)

// Recorder names the provider and model a run's turns belong to.
type Recorder struct {
	Provider string
	Model    string
}

type recorderKey struct{}

type turnKey struct{}

type callKey struct{}

type childKey struct{}

// child names the Task child agent a context belongs to and the tool call
// that dispatched it.
type child struct {
	agent  string
	callID string
}

// WithRecorder attaches r to ctx so nested loops record turns under the
// right provider and model.
func WithRecorder(ctx context.Context, r Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, r)
}

// RecorderFrom returns the Recorder attached to ctx, or the zero Recorder.
func RecorderFrom(ctx context.Context) Recorder {
	r, _ := ctx.Value(recorderKey{}).(Recorder)
	return r
}

// TurnIndex returns the 1-based index of the turn whose context ctx derives
// from, or 0 outside a turn.
func TurnIndex(ctx context.Context) int {
	i, _ := ctx.Value(turnKey{}).(int)
	return i
}

// WithChildAgent returns the context for child agent agent, dispatched by the
// tool call whose context (from StartTool) is ctx. Events recorded under it
// carry the agent's name as agent and, unless they name their own, the
// call's id as parent_tool_use_id.
func WithChildAgent(ctx context.Context, agent string) context.Context {
	callID, _ := ctx.Value(callKey{}).(string)
	return context.WithValue(ctx, childKey{}, child{agent: agent, callID: callID})
}

// Usage is one turn's token counts. InputTokens is fresh prompt input; the
// cache counts are the prompt tokens that went through the prompt cache.
type Usage struct {
	InputTokens      int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	OutputTokens     int64
}

// TurnResult is what one model turn produced.
type TurnResult struct {
	// Text is the assistant's visible text for the turn.
	Text string
	// Thinking is the model's reasoning text, when the provider returns it.
	Thinking string
	// ThinkingTokens is the provider's estimate of reasoning tokens, for
	// providers that report a count but not the text (claude-code).
	ThinkingTokens int64
	// FinishReason is the provider's stop reason, when it reports one.
	FinishReason string
	Usage        Usage
	// ToolCalls is how many tool calls the turn requested.
	ToolCalls int
	// ParentToolUseID, when set, names the tool call (a subagent dispatch)
	// this turn ran under.
	ParentToolUseID string
}

// Turn is one model iteration: the model call and the tool calls it
// requested. Its span parents the tool spans started from Context.
type Turn struct {
	rec   Recorder
	ctx   context.Context
	span  trace.Span
	index int
	start time.Time
}

// StartTurn opens turn index (1-based) and its "chat <model>" span. A zero
// start means now; agentic CLI paths pass the time the turn began.
func (r Recorder) StartTurn(ctx context.Context, index int, start time.Time) *Turn {
	if start.IsZero() {
		start = time.Now()
	}
	name := "chat"
	if r.Model != "" {
		name += " " + r.Model
	}
	ctx, span := telemetry.Tracer().Start(ctx, name,
		trace.WithTimestamp(start),
		trace.WithAttributes(
			attribute.String("gen_ai.operation.name", "chat"),
			attribute.String("gen_ai.provider.name", r.Provider),
			attribute.String("gen_ai.system", r.Provider),
			attribute.String("gen_ai.request.model", r.Model),
			attribute.Int("squad.turn.index", index),
		),
	)
	ctx = context.WithValue(ctx, turnKey{}, index)
	return &Turn{rec: r, ctx: ctx, span: span, index: index, start: start}
}

// Context returns the context carrying the turn's span. Model calls and tool
// calls made for this turn should use it so their spans nest under the turn.
func (t *Turn) Context() context.Context { return t.ctx }

// Record writes the turn's iteration and response events and sets the turn
// span's token, finish-reason and size attributes.
func (t *Turn) Record(res TurnResult) {
	t.recordAt(res, time.Now(), time.Time{})
}

// RecordAt is Record for a turn recorded after its tool calls, as an agentic
// CLI's turns are: at is when the model call finished, and the iteration and
// response events are stamped with the turn's start time so they sort before
// the turn's tool calls.
func (t *Turn) RecordAt(res TurnResult, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	t.recordAt(res, at, t.start)
}

func (t *Turn) recordAt(res TurnResult, at, ts time.Time) {
	iteration := map[string]any{
		"index":      t.index,
		"tool_calls": res.ToolCalls,
	}
	if res.ParentToolUseID != "" {
		iteration["parent_tool_use_id"] = res.ParentToolUseID
	}
	appendAt(t.ctx, ts, session.EventIteration, iteration)
	payload := map[string]any{
		"iteration":     t.index,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
		"tool_calls":    res.ToolCalls,
		"duration_ms":   at.Sub(t.start).Milliseconds(),
	}
	if res.Text != "" {
		payload["text"] = CapText(res.Text)
	}
	if res.Thinking != "" {
		payload["thinking"] = truncate(res.Thinking, maxEventThinkingBytes)
		payload["thinking_bytes"] = len(res.Thinking)
	}
	if res.ThinkingTokens > 0 {
		payload["thinking_tokens"] = res.ThinkingTokens
	}
	if res.FinishReason != "" {
		payload["finish_reason"] = res.FinishReason
	}
	if res.ParentToolUseID != "" {
		payload["parent_tool_use_id"] = res.ParentToolUseID
	}
	if res.Usage.CacheReadTokens > 0 {
		payload["cache_read_tokens"] = res.Usage.CacheReadTokens
	}
	if res.Usage.CacheWriteTokens > 0 {
		payload["cache_write_tokens"] = res.Usage.CacheWriteTokens
	}
	appendAt(t.ctx, ts, session.EventResponse, payload)

	attrs := []attribute.KeyValue{
		attribute.Int64("gen_ai.usage.input_tokens", res.Usage.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", res.Usage.OutputTokens),
		attribute.Int64("gen_ai.usage.cache_read.input_tokens", res.Usage.CacheReadTokens),
		attribute.Int64("gen_ai.usage.cache_creation.input_tokens", res.Usage.CacheWriteTokens),
		attribute.Int("squad.turn.tool_calls", res.ToolCalls),
		attribute.Int("squad.turn.text_bytes", len(res.Text)),
		attribute.Int("squad.turn.thinking_bytes", len(res.Thinking)),
	}
	if res.ThinkingTokens > 0 {
		attrs = append(attrs, attribute.Int64("squad.turn.thinking_tokens", res.ThinkingTokens))
	}
	if res.FinishReason != "" {
		attrs = append(attrs, attribute.StringSlice("gen_ai.response.finish_reasons", []string{res.FinishReason}))
	}
	t.span.SetAttributes(attrs...)
}

// Fail records a failed model call for this turn as an error event and marks
// the turn span as failed.
func (t *Turn) Fail(phase string, err error) {
	if err == nil {
		return
	}
	Append(t.ctx, session.EventError, map[string]any{
		"phase":     phase,
		"iteration": t.index,
		"error":     truncate(err.Error(), maxEventErrorBytes),
	})
	t.span.RecordError(err)
	t.span.SetStatus(codes.Error, truncate(err.Error(), 200))
}

// End closes the turn span now.
func (t *Turn) End() { t.span.End() }

// EndAt closes the turn span at a known time; a zero time means now.
func (t *Turn) EndAt(at time.Time) {
	if at.IsZero() {
		t.span.End()
		return
	}
	t.span.End(trace.WithTimestamp(at))
}

// Tool is one tool call. Exactly one of Deny or Finish ends it.
type Tool struct {
	ctx       context.Context
	span      trace.Span
	callID    string
	name      string
	iteration int
	start     time.Time
	ended     bool
	// parentToolUseID is the call's ToolCall.ParentToolUseID, repeated on
	// its tool_result and tool_denied events.
	parentToolUseID string
}

// ToolCall describes a tool call when it starts.
type ToolCall struct {
	// Iteration is the 1-based turn that requested the call (0 if unknown).
	Iteration int
	CallID    string
	Name      string
	// Args is the raw argument JSON. It is written to events.jsonl in
	// full; spans only see its size.
	Args string
	// Start is when the call began; zero means now.
	Start time.Time
	// ParentToolUseID, when set, names the tool call (a subagent dispatch)
	// this call ran under.
	ParentToolUseID string
}

// StartTool writes the tool_call event, stamped with the call's start, and
// opens the "tool.<name>" span as a child of the span on ctx (normally a
// turn). The returned context carries
// the tool span and call id, for handlers that add attributes to the span or
// dispatch a child agent (WithChildAgent).
func StartTool(ctx context.Context, call ToolCall) (context.Context, *Tool) {
	start := call.Start
	if start.IsZero() {
		start = time.Now()
	}
	if call.Iteration == 0 {
		call.Iteration = TurnIndex(ctx)
	}
	ctx, span := telemetry.Tracer().Start(ctx, "tool."+call.Name,
		trace.WithTimestamp(start),
		trace.WithAttributes(
			attribute.String("squad.tool.name", call.Name),
			attribute.String("squad.tool.call_id", call.CallID),
			attribute.Int("squad.tool.args_bytes", len(call.Args)),
		),
	)
	ctx = context.WithValue(ctx, callKey{}, call.CallID)
	payload := map[string]any{
		"call_id": call.CallID,
		"name":    call.Name,
		"args":    truncate(call.Args, maxEventArgsBytes),
	}
	if call.Iteration > 0 {
		payload["iteration"] = call.Iteration
	}
	if call.ParentToolUseID != "" {
		payload["parent_tool_use_id"] = call.ParentToolUseID
	}
	appendAt(ctx, start, session.EventToolCall, payload)
	return ctx, &Tool{ctx: ctx, span: span, callID: call.CallID, name: call.Name, iteration: call.Iteration, start: start, parentToolUseID: call.ParentToolUseID}
}

// Deny records that the call was refused before it ran: a tool_denied event
// and a span with squad.tool.denied=true. message is what the model was told.
func (t *Tool) Deny(reason, message string) {
	t.DenyAt(reason, message, time.Time{})
}

// DenyAt is Deny for a refusal observed at a known time; zero means now.
func (t *Tool) DenyAt(reason, message string, at time.Time) {
	if t == nil || t.ended {
		return
	}
	t.ended = true
	payload := map[string]any{
		"call_id": t.callID,
		"name":    t.name,
		"reason":  reason,
	}
	if message != "" {
		payload["message"] = truncate(message, maxEventErrorBytes)
	}
	if t.iteration > 0 {
		payload["iteration"] = t.iteration
	}
	if t.parentToolUseID != "" {
		payload["parent_tool_use_id"] = t.parentToolUseID
	}
	appendAt(t.ctx, at, session.EventToolDenied, payload)
	t.span.SetAttributes(
		attribute.Bool("squad.tool.denied", true),
		attribute.String("squad.tool.denied_reason", reason),
	)
	endSpan(t.span, at)
}

// Result is a finished tool call's outcome.
type Result struct {
	// OutputBytes is the size of the output the tool produced.
	OutputBytes int
	// Err is the tool's failure, if any. ErrText may stand in for it when
	// the error only exists as text (an agentic CLI's error result).
	Err     error
	ErrText string
	// End is when the call finished; zero means now.
	End time.Time
	// Extra adds provider-specific fields to the tool_result event.
	Extra map[string]any
}

// OutputError is a tool failure whose text is the tool's own output, such as
// an MCP tool's error result. Finish treats it as it does Result.ErrText.
type OutputError string

// Error returns the tool's output.
func (e OutputError) Error() string { return string(e) }

// toolErrorStatus is the span status description for a failed call.
const toolErrorStatus = "tool returned an error"

// Finish writes the tool_result event, with the full error text, and closes
// the span with the output size and error status. ErrText and an OutputError
// are the tool's own output, so their text never reaches the span. Any other
// Err can name the call's arguments (a refused path), so its text reaches
// the span only under SQUAD_OTEL_EXPORT_COMMANDS=1 (see
// telemetry.RecordContentError).
func (t *Tool) Finish(res Result) {
	if t == nil || t.ended {
		return
	}
	t.ended = true
	end := res.End
	if end.IsZero() {
		end = time.Now()
	}
	errText := res.ErrText
	if res.Err != nil {
		errText = res.Err.Error()
	}
	isError := res.Err != nil || res.ErrText != ""
	payload := map[string]any{
		"call_id":     t.callID,
		"name":        t.name,
		"bytes":       res.OutputBytes,
		"duration_ms": end.Sub(t.start).Milliseconds(),
		"is_error":    isError,
	}
	if errText != "" {
		payload["error"] = truncate(errText, maxEventErrorBytes)
	}
	if t.iteration > 0 {
		payload["iteration"] = t.iteration
	}
	if t.parentToolUseID != "" {
		payload["parent_tool_use_id"] = t.parentToolUseID
	}
	for k, v := range res.Extra {
		payload[k] = v
	}
	appendAt(t.ctx, end, session.EventToolResult, payload)
	t.span.SetAttributes(
		attribute.Int("squad.tool.output_bytes", res.OutputBytes),
		attribute.Bool("squad.tool.denied", false),
	)
	var output OutputError
	switch {
	case res.Err != nil && !errors.As(res.Err, &output):
		telemetry.RecordContentError(t.span, res.Err, toolErrorStatus)
	case isError:
		t.span.SetStatus(codes.Error, toolErrorStatus)
	}
	endSpan(t.span, end)
}

// Error records a run-level error event (one not tied to a turn) on the span
// active on ctx.
func Error(ctx context.Context, phase string, err error) {
	if err == nil {
		return
	}
	Append(ctx, session.EventError, map[string]any{
		"phase": phase,
		"error": truncate(err.Error(), maxEventErrorBytes),
	})
}

func endSpan(span trace.Span, at time.Time) {
	if at.IsZero() {
		span.End()
		return
	}
	span.End(trace.WithTimestamp(at))
}

// Append writes one event to the session log on ctx, tagged with the child
// agent ctx belongs to (see WithChildAgent). It is a no-op without a session
// logger.
func Append(ctx context.Context, eventType string, payload map[string]any) {
	appendAt(ctx, time.Time{}, eventType, payload)
}

func appendAt(ctx context.Context, ts time.Time, eventType string, payload map[string]any) {
	l := session.FromContext(ctx)
	if l == nil {
		return
	}
	if c, ok := ctx.Value(childKey{}).(child); ok {
		payload["agent"] = c.agent
		if _, own := payload["parent_tool_use_id"]; !own && c.callID != "" {
			payload["parent_tool_use_id"] = c.callID
		}
	}
	if err := l.AppendContextAt(ctx, ts, eventType, payload); err != nil {
		logging.Warn("session: append %s failed: %v", eventType, err)
	}
}

// CapText cuts assistant text to the size an events.jsonl value is capped at,
// for callers that write response events without a Turn.
func CapText(s string) string { return truncate(s, maxEventTextBytes) }

// truncate cuts s to at most n bytes, backing up to a UTF-8 boundary, and
// marks the cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…[truncated]"
}
