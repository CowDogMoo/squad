// Package activity records what an agent does, one model turn and one tool
// call at a time, as session events (events.jsonl) and OpenTelemetry spans.
// Every provider path feeds it the same way, so the event record and the
// span tree have one shape whether squad runs the tool loop itself (API
// providers) or reads it back from an agentic CLI (claude-code).
//
// events.jsonl is a local file and keeps full assistant text and tool
// arguments. Spans may leave the machine, so by default they carry only
// names, timing, sizes, token counts and errors; assistant text and argument
// summaries are added only behind the telemetry opt-ins
// (SQUAD_OTEL_EXPORT_TEXT, SQUAD_OTEL_EXPORT_COMMANDS).
package activity

import (
	"context"
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
// line too long for readers such as `squad watch`.
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

// Index returns the turn's 1-based index.
func (t *Turn) Index() int { return t.index }

// Record writes the turn's iteration and response events and sets the turn
// span's token, finish-reason, size and (opt-in) text attributes.
func (t *Turn) Record(res TurnResult) {
	t.recordAt(res, time.Now())
}

// RecordAt is Record for a turn whose model call finished at a known time.
func (t *Turn) RecordAt(res TurnResult, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	t.recordAt(res, at)
}

func (t *Turn) recordAt(res TurnResult, at time.Time) {
	logger := session.FromContext(t.ctx)
	iteration := map[string]any{
		"index":      t.index,
		"tool_calls": res.ToolCalls,
	}
	if res.ParentToolUseID != "" {
		iteration["parent_tool_use_id"] = res.ParentToolUseID
	}
	appendEvent(t.ctx, logger, session.EventIteration, iteration)
	payload := map[string]any{
		"iteration":     t.index,
		"input_tokens":  res.Usage.InputTokens,
		"output_tokens": res.Usage.OutputTokens,
		"tool_calls":    res.ToolCalls,
		"duration_ms":   at.Sub(t.start).Milliseconds(),
	}
	if res.Text != "" {
		payload["text"] = truncate(res.Text, maxEventTextBytes)
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
	appendEvent(t.ctx, logger, session.EventResponse, payload)

	attrs := []attribute.KeyValue{
		attribute.Int64("gen_ai.usage.input_tokens", res.Usage.InputTokens),
		attribute.Int64("gen_ai.usage.output_tokens", res.Usage.OutputTokens),
		attribute.Int64("gen_ai.usage.cache_read.input_tokens", res.Usage.CacheReadTokens),
		attribute.Int64("gen_ai.usage.cache_creation.input_tokens", res.Usage.CacheWriteTokens),
		attribute.Int("squad.turn.tool_calls", res.ToolCalls),
		attribute.Int("squad.turn.thinking_bytes", len(res.Thinking)),
	}
	if res.ThinkingTokens > 0 {
		attrs = append(attrs, attribute.Int64("squad.turn.thinking_tokens", res.ThinkingTokens))
	}
	if res.FinishReason != "" {
		attrs = append(attrs, attribute.StringSlice("gen_ai.response.finish_reasons", []string{res.FinishReason}))
	}
	attrs = append(attrs, telemetry.TextAttributes(res.Text)...)
	t.span.SetAttributes(attrs...)
}

// Fail records a failed model call for this turn as an error event and marks
// the turn span as failed.
func (t *Turn) Fail(phase string, err error) {
	if err == nil {
		return
	}
	appendEvent(t.ctx, session.FromContext(t.ctx), session.EventError, map[string]any{
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
	// Summary is a short description of the arguments (a command, path or
	// pattern) exported on the span only behind SQUAD_OTEL_EXPORT_COMMANDS.
	Summary string
	// Start is when the call began; zero means now.
	Start time.Time
	// ParentToolUseID, when set, names the tool call (a subagent dispatch)
	// this call ran under.
	ParentToolUseID string
}

// StartTool writes the tool_call event and opens the "tool.<name>" span as a
// child of the span on ctx (normally a turn). The returned context carries
// the tool span, for handlers that add attributes to it.
func StartTool(ctx context.Context, call ToolCall) (context.Context, *Tool) {
	start := call.Start
	if start.IsZero() {
		start = time.Now()
	}
	if call.Iteration == 0 {
		call.Iteration = TurnIndex(ctx)
	}
	attrs := []attribute.KeyValue{
		attribute.String("squad.tool.name", call.Name),
		attribute.String("squad.tool.call_id", call.CallID),
	}
	attrs = append(attrs, telemetry.ToolArgsAttributes(len(call.Args), call.Summary)...)
	ctx, span := telemetry.Tracer().Start(ctx, "tool."+call.Name,
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
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
	appendEvent(ctx, session.FromContext(ctx), session.EventToolCall, payload)
	return ctx, &Tool{ctx: ctx, span: span, callID: call.CallID, name: call.Name, iteration: call.Iteration, start: start}
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
	appendEvent(t.ctx, session.FromContext(t.ctx), session.EventToolDenied, payload)
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

// Finish writes the tool_result event and closes the span with the output
// size and error status.
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
	for k, v := range res.Extra {
		payload[k] = v
	}
	appendEvent(t.ctx, session.FromContext(t.ctx), session.EventToolResult, payload)
	t.span.SetAttributes(
		attribute.Int("squad.tool.output_bytes", res.OutputBytes),
		attribute.Bool("squad.tool.denied", false),
	)
	if isError {
		if res.Err != nil {
			t.span.RecordError(res.Err)
		}
		t.span.SetStatus(codes.Error, truncate(errText, 200))
	}
	endSpan(t.span, end)
}

// Error records a run-level error event (one not tied to a turn) on the span
// active on ctx.
func Error(ctx context.Context, phase string, err error) {
	if err == nil {
		return
	}
	appendEvent(ctx, session.FromContext(ctx), session.EventError, map[string]any{
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

func appendEvent(ctx context.Context, l *session.Logger, eventType string, payload any) {
	if l == nil {
		return
	}
	if err := l.AppendContext(ctx, eventType, payload); err != nil {
		logging.Warn("session: append %s failed: %v", eventType, err)
	}
}

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
