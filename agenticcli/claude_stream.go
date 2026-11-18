// Stream-json decoding shared by Claude Code's single-shot and live paths.
//
// Both paths run claude with `--output-format stream-json --verbose`, which
// prints one JSON event per line as the session runs: assistant messages
// (text, thinking and tool_use blocks), user messages carrying tool_result
// blocks, thinking-token estimates, and a terminal `result` event whose
// fields are a superset of the `--output-format json` envelope. squad reads
// the final result from that last event, and hands every other record to an
// Observer so the run's turns and tool calls can be recorded as they happen.

package agenticcli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/cowdogmoo/squad/logging"
)

// Observer receives the activity decoded from a claude stream-json session.
// Methods are called in stream order from the goroutine reading the CLI's
// stdout. Implementations must not block for long: the CLI stalls once its
// stdout pipe fills.
type Observer interface {
	// SessionStarted reports the CLI's session id and resolved model.
	SessionStarted(sessionID, model string)
	// ThinkingTokens reports the CLI's running estimate of reasoning tokens
	// for the generation in progress.
	ThinkingTokens(estimated int64)
	// Assistant reports one assistant event: some or all content blocks of
	// one model message.
	Assistant(msg AssistantMessage)
	// ToolResults reports the tool results carried by one user event.
	ToolResults(results []ToolResult)
	// ToolDenied reports a tool call the parent refused through the live
	// permission protocol; message is what the model was told.
	ToolDenied(toolUseID, message string)
	// Finished reports the terminal result event.
	Finished(at time.Time)
}

// AssistantMessage is one assistant event.
type AssistantMessage struct {
	// ID is the model message id. A message's blocks may arrive split over
	// several events that share it.
	ID string
	// ParentToolUseID is set when the message belongs to a subagent the
	// main agent dispatched with that tool call.
	ParentToolUseID string
	Time            time.Time
	Text            []string
	Thinking        []string
	ToolUses        []ToolUse
	StopReason      string
	Usage           MessageUsage
}

// ToolUse is one tool_use block.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// MessageUsage is the token usage the CLI attaches to an assistant event.
// The CLI streams it from the start of the message, so OutputTokens can be
// lower than the message's final count.
type MessageUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

// ToolResult is one tool_result block.
type ToolResult struct {
	ToolUseID       string
	ParentToolUseID string
	Time            time.Time
	IsError         bool
	// Content is the result as text: a string result verbatim, or the text
	// parts of a block-list result joined with newlines.
	Content string
	// PermissionDecision is the CLI's recorded permission decision for the
	// call ("accept" when it was allowed), when it reports one.
	PermissionDecision string
}

// liveEvent is the envelope common to every stream-json stdout line. Result
// fields are decoded separately (claudeOutput) from the raw line.
type liveEvent struct {
	Type            string        `json:"type"`
	Subtype         string        `json:"subtype"`
	SessionID       string        `json:"session_id"`
	RequestID       string        `json:"request_id"`
	Model           string        `json:"model"`
	Timestamp       string        `json:"timestamp"`
	ParentToolUseID string        `json:"parent_tool_use_id"`
	EstimatedTokens int64         `json:"estimated_tokens"`
	Request         *controlEvent `json:"request"`
	Message         *streamedMsg  `json:"message"`
	ToolResultMeta  []struct {
		ID                 string `json:"id"`
		PermissionDecision struct {
			Decision string `json:"decision"`
		} `json:"permission_decision"`
	} `json:"tool_result_meta"`
}

// controlEvent is the request half of a control_request event.
type controlEvent struct {
	Subtype   string          `json:"subtype"`
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// streamedMsg carries the content blocks of an assistant or user event.
type streamedMsg struct {
	ID         string        `json:"id"`
	StopReason string        `json:"stop_reason"`
	Usage      *MessageUsage `json:"usage"`
	Content    []streamBlock `json:"content"`
}

// streamBlock is one content block: text, thinking, tool_use or tool_result.
// A user event's content may also be a plain string, which leaves Content
// empty (see decodeLiveEvent).
type streamBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// decodeLiveEvent decodes one stdout line. A user message whose content is a
// plain string (not a block list) decodes with no content blocks.
func decodeLiveEvent(line []byte) (liveEvent, error) {
	var ev liveEvent
	err := json.Unmarshal(line, &ev)
	if err == nil {
		return ev, nil
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return ev, err
	}
	// Retry without the message body, which is the only field whose shape
	// varies between events.
	var bare struct {
		liveEvent
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(line, &bare); err != nil {
		return liveEvent{}, err
	}
	bare.liveEvent.Message = nil
	return bare.liveEvent, nil
}

// eventTime parses an event's ISO timestamp, falling back to now.
func eventTime(ts string) time.Time {
	if ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return t
		}
	}
	return time.Now()
}

// observe hands one decoded event to obs. It is a no-op without an observer.
func observe(obs Observer, ev liveEvent) {
	if obs == nil {
		return
	}
	switch ev.Type {
	case "system":
		switch ev.Subtype {
		case "init":
			obs.SessionStarted(ev.SessionID, ev.Model)
		case "thinking_tokens":
			obs.ThinkingTokens(ev.EstimatedTokens)
		}
	case "assistant":
		if ev.Message != nil {
			obs.Assistant(assistantMessage(ev))
		}
	case "user":
		if results := toolResults(ev); len(results) > 0 {
			obs.ToolResults(results)
		}
	case "result":
		obs.Finished(time.Now())
	}
}

func assistantMessage(ev liveEvent) AssistantMessage {
	msg := AssistantMessage{
		ID:              ev.Message.ID,
		ParentToolUseID: ev.ParentToolUseID,
		Time:            eventTime(ev.Timestamp),
		StopReason:      ev.Message.StopReason,
	}
	if ev.Message.Usage != nil {
		msg.Usage = *ev.Message.Usage
	}
	for _, b := range ev.Message.Content {
		switch b.Type {
		case "text":
			if b.Text != "" {
				msg.Text = append(msg.Text, b.Text)
			}
		case "thinking":
			msg.Thinking = append(msg.Thinking, b.Thinking)
		case "tool_use":
			msg.ToolUses = append(msg.ToolUses, ToolUse{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	return msg
}

func toolResults(ev liveEvent) []ToolResult {
	if ev.Message == nil {
		return nil
	}
	decisions := make(map[string]string, len(ev.ToolResultMeta))
	for _, m := range ev.ToolResultMeta {
		decisions[m.ID] = m.PermissionDecision.Decision
	}
	at := eventTime(ev.Timestamp)
	var out []ToolResult
	for _, b := range ev.Message.Content {
		if b.Type != "tool_result" {
			continue
		}
		out = append(out, ToolResult{
			ToolUseID:          b.ToolUseID,
			ParentToolUseID:    ev.ParentToolUseID,
			Time:               at,
			IsError:            b.IsError,
			Content:            toolResultText(b.Content),
			PermissionDecision: decisions[b.ToolUseID],
		})
	}
	return out
}

// toolResultText flattens a tool_result's content, which is either a string
// or a list of blocks, to text.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []streamBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// runClaudeStream runs claude single-shot with stream-json output, feeding
// obs as events arrive, and returns the terminal result exactly as the
// `--output-format json` envelope would have produced it.
func runClaudeStream(ctx context.Context, spec Spec, path string, req Request) (Result, error) {
	args, stdin := buildArgs(req)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("%s CLI failed: stdout pipe: %w", spec.Binary, err)
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("%s CLI failed: %w", spec.Binary, err)
	}

	sr := &streamReader{obs: req.Observer}
	readErr := sr.read(stdout)
	waitErr := cmd.Wait()
	if sr.resultSeen {
		if waitErr != nil && sr.resErr == nil {
			logging.Warn("%s exited uncleanly after its result: %v", spec.Binary, waitErr)
		}
		return sr.res, sr.resErr
	}
	if waitErr != nil {
		return Result{}, fmt.Errorf("%s CLI failed: %w%s", spec.Binary, waitErr,
			outputTail(stderr.Bytes(), sr.raw.Bytes()))
	}
	if readErr != nil {
		return Result{}, fmt.Errorf("%s CLI failed: read stdout: %w", spec.Binary, readErr)
	}
	// No result event: a CLI that ignored stream-json and printed the
	// plain JSON envelope, or text. parseOutput handles both exactly as the
	// json output format always has.
	return parseOutput(spec.Provider, sr.raw.Bytes())
}

// streamReader consumes a single-shot stream-json stdout.
type streamReader struct {
	obs        Observer
	resultSeen bool
	res        Result
	resErr     error
	// raw collects lines that are not stream-json events, parsed as the
	// output when no result event arrives. Partial events are dropped.
	raw bytes.Buffer
}

func (sr *streamReader) read(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			sr.handle(bytes.TrimSpace(line))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func (sr *streamReader) handle(line []byte) {
	if len(line) == 0 {
		return
	}
	ev, err := decodeLiveEvent(line)
	if err != nil && line[0] == '{' {
		return
	}
	if err != nil || ev.Type == "" {
		sr.raw.Write(line)
		sr.raw.WriteByte('\n')
		return
	}
	if ev.Type == "result" {
		sr.resultSeen = true
		var out claudeOutput
		if err := json.Unmarshal(line, &out); err != nil {
			sr.resErr = fmt.Errorf("undecodable result event: %w", err)
		} else {
			sr.res, sr.resErr = claudeResult(out)
		}
	}
	observe(sr.obs, ev)
}
