// Live stream-json session for Claude Code.
//
// RunLive drives `claude` through its stream-json control protocol
// (`--input-format stream-json --output-format stream-json
// --permission-prompt-tool stdio`): the CLI emits a `control_request`
// (subtype `can_use_tool`) on stdout for every permission-gated tool call and
// blocks until the parent answers on stdin with a `control_response` carrying
// allow/deny. That synchronous in-process interceptor is the seam squad's
// interactive sign-off gate needs — the single-shot `--print` path never
// surfaces tool calls at all.
//
// Wire shapes are pinned against claude CLI 2.1.220 and recorded as golden
// fixtures in testdata/claude_live_session.jsonl (CLI → parent) and
// testdata/claude_live_parent.jsonl (parent → CLI).

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

// claudeLiveExitGrace bounds how long RunLive waits for the CLI to exit after
// the session is over (result received and stdin closed, or a kill was
// issued) before force-killing it. Var so tests can shorten it.
var claudeLiveExitGrace = 15 * time.Second

// maxProtocolErrors is how many consecutive undecodable or unanswerable
// stdout events RunLive tolerates before failing closed and killing the run.
const maxProtocolErrors = 2

// Decision is the parent's answer to one can_use_tool request.
type Decision struct {
	Allow bool
	// Message is the deny reason fed back to the model as an error tool
	// result; ignored on allow.
	Message string
	// UpdatedInput optionally rewrites the tool input on allow; nil keeps
	// the input the model proposed.
	UpdatedInput json.RawMessage
	// Interrupt requests a control-protocol interrupt right after this
	// decision is delivered, aborting the current turn (used when the
	// reviewer rejects a plan outright).
	Interrupt bool
}

// CanUseTool is consulted for every can_use_tool control request.
// assistantText is the most recent assistant prose (the model's plan, when it
// has written one). Implemented by the runner adapting tools.SignOffRuntime;
// agenticcli stays free of a tools/ import. Any error fails closed: the tool
// call is denied and the run is killed.
type CanUseTool func(ctx context.Context, toolName string, input json.RawMessage, assistantText string) (Decision, error)

// LiveRequest carries one live-session invocation.
type LiveRequest struct {
	Request
	CanUseTool CanUseTool
}

// claudeLiveArgs maps a Request onto the live-session argv. Unlike the
// single-shot path there is no --dangerously-skip-permissions: permission
// checks must stay on so gated tool calls route to the stdio prompt tool.
// --permission-mode default is passed explicitly so a project settings file
// (e.g. defaultMode: acceptEdits) can't silently bypass the prompt.
func claudeLiveArgs(req Request) []string {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-prompt-tool", "stdio",
		"--permission-mode", "default",
	}
	return append(args, claudeCommonArgs(req)...)
}

// RunLive executes a claude-code live session for req, answering every
// can_use_tool request via req.CanUseTool, and returns the terminal result.
// The subprocess inherits the parent environment so the CLI's own credential
// store keeps working; ctx cancellation kills it.
func RunLive(ctx context.Context, req LiveRequest) (Result, error) {
	if req.Provider != "claude-code" {
		return Result{}, fmt.Errorf("live session is only supported for provider %q, got %q", "claude-code", req.Provider)
	}
	if req.CanUseTool == nil {
		return Result{}, errors.New("live session requires a CanUseTool callback")
	}
	spec, _ := Lookup("claude-code")
	path, err := exec.LookPath(spec.Binary)
	if err != nil {
		return Result{}, fmt.Errorf("provider %q requires the %q CLI on PATH (install: %s): %w",
			spec.Provider, spec.Binary, spec.Install, err)
	}

	cmd := exec.CommandContext(ctx, path, claudeLiveArgs(req.Request)...)
	cmd.Dir = req.WorkDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, fmt.Errorf("claude live: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("claude live: stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("claude live: start: %w", err)
	}

	s := &liveSession{
		ctx:        ctx,
		canUseTool: req.CanUseTool,
		stdin:      stdin,
		reader:     bufio.NewReaderSize(stdout, 64*1024),
		kill:       func() { _ = cmd.Process.Kill() },
	}
	res, runErr := s.loop(req.UserPrompt)
	s.closeStdin()
	if runErr != nil {
		// Fail closed: a callback or protocol failure must not leave the
		// CLI running (and possibly editing) unsupervised.
		s.kill()
	}
	exitGuard := time.AfterFunc(claudeLiveExitGrace, s.kill)
	waitErr := cmd.Wait()
	exitGuard.Stop()
	if s.watchdog != nil {
		s.watchdog.Stop()
	}

	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return res, ctxErr
		}
		if !s.resultSeen {
			hint := ""
			if s.sessionID == "" {
				// Died before the init handshake: most likely an installed
				// CLI that predates the control protocol.
				hint = " (hint: --interactive with claude-code needs a claude CLI supporting --input-format stream-json and --permission-prompt-tool stdio; verified with 2.1.220 — check `claude --version`)"
			}
			return res, fmt.Errorf("claude live session failed: %w%s%s", runErr, outputTail(stderr.Bytes(), nil), hint)
		}
		return res, runErr
	}
	if waitErr != nil {
		// The result envelope already arrived; a nonzero exit afterwards
		// doesn't invalidate it.
		logging.Warn("claude live process exited uncleanly after result: %v", waitErr)
	}
	return res, nil
}

// liveSession is the single-goroutine event loop over the CLI's stdout.
// Writes to stdin happen only from this loop (initial message, control
// responses), so no locking is needed.
type liveSession struct {
	ctx        context.Context
	canUseTool CanUseTool
	stdin      io.WriteCloser
	reader     *bufio.Reader
	kill       func()

	sessionID    string
	stdinClosed  bool
	resultSeen   bool
	res          Result
	resErr       error
	protoErrs    int
	interruptSeq int
	watchdog     *time.Timer

	// Plan-text accumulation: assistant events arrive one content block at
	// a time, tagged with their message id. curText collects the text
	// blocks of the in-flight message; lastText holds the previous
	// message's prose so a tool_use-only message still has a plan to show.
	curMsgID string
	curText  strings.Builder
	lastText string
}

// loop sends the initial user message and processes stdout events until EOF.
// The terminal result event closes stdin (asking the CLI to exit) but the
// loop keeps draining until EOF so the process never blocks on a full pipe.
func (s *liveSession) loop(userPrompt string) (Result, error) {
	if err := s.send(userMessageEvent(userPrompt)); err != nil {
		return Result{}, err
	}
	for {
		line, readErr := s.reader.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			if fatal := s.handle([]byte(trimmed)); fatal != nil {
				return s.res, fatal
			}
		}
		if readErr != nil {
			if s.ctx.Err() != nil {
				return s.res, s.ctx.Err()
			}
			if !s.resultSeen {
				return s.res, fmt.Errorf("claude exited without a result event: %w", errOrEOF(readErr))
			}
			return s.res, s.resErr
		}
	}
}

// errOrEOF keeps "unexpected EOF before result" errors readable: a plain
// io.EOF adds no information, so it is reported as the process ending early.
func errOrEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return errors.New("process closed stdout")
	}
	return err
}

// liveEvent is the envelope common to every stream-json stdout line. Result
// fields are decoded separately (claudeOutput) from the raw line.
type liveEvent struct {
	Type      string        `json:"type"`
	Subtype   string        `json:"subtype"`
	SessionID string        `json:"session_id"`
	RequestID string        `json:"request_id"`
	Request   *controlEvent `json:"request"`
	Message   *streamedMsg  `json:"message"`
}

// controlEvent is the request half of a control_request event.
type controlEvent struct {
	Subtype   string          `json:"subtype"`
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// streamedMsg carries the content blocks of an assistant event.
type streamedMsg struct {
	ID      string `json:"id"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// handle processes one stdout line, storing the terminal result (with its
// is_error verdict) on the session. It returns a fatal error when the
// session must be killed (callback failure, repeated protocol errors).
func (s *liveSession) handle(line []byte) error {
	var ev liveEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return s.protocolError(fmt.Errorf("undecodable stdout line: %w", err))
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			s.sessionID = ev.SessionID
			s.protoErrs = 0
			logging.InfoContext(s.ctx, "claude live session started (session_id=%s)", ev.SessionID)
		}
	case "assistant":
		s.protoErrs = 0
		s.accumulate(ev.Message)
	case "control_request":
		return s.handleControlRequest(ev)
	case "result":
		s.protoErrs = 0
		s.resultSeen = true
		var out claudeOutput
		if err := json.Unmarshal(line, &out); err != nil {
			return fmt.Errorf("undecodable result event: %w", err)
		}
		s.res, s.resErr = claudeResult(out)
		// The session is over: ask the CLI to exit (stdin EOF) and arm the
		// watchdog now — the loop keeps draining stdout until EOF, and a CLI
		// that never exits would otherwise block it forever.
		s.closeStdin()
		s.watchdog = time.AfterFunc(claudeLiveExitGrace, s.kill)
	case "control_response":
		// Ack of an interrupt we sent; nothing to do.
		s.protoErrs = 0
	default:
		// rate_limit_event, thinking-token telemetry, echoed tool
		// results, future event types: irrelevant to the session.
		logging.DebugContext(s.ctx, "claude live: ignoring %q event", ev.Type)
	}
	return nil
}

// handleControlRequest answers a control_request event. Unknown subtypes get
// an error response and count toward the protocol-error budget; can_use_tool
// consults the callback and fails closed (deny + kill) if it errors.
func (s *liveSession) handleControlRequest(ev liveEvent) error {
	if ev.Request == nil || ev.RequestID == "" {
		return s.protocolError(errors.New("control_request without request body or request_id"))
	}
	if ev.Request.Subtype != "can_use_tool" {
		if err := s.send(controlErrorEvent(ev.RequestID, "squad does not handle this control request")); err != nil {
			return err
		}
		return s.protocolError(fmt.Errorf("unsupported control_request subtype %q", ev.Request.Subtype))
	}
	s.protoErrs = 0
	decision, err := s.canUseTool(s.ctx, ev.Request.ToolName, ev.Request.Input, s.latestAssistantText())
	if err != nil {
		// Fail closed (signoff-tty-error): deny this call so the CLI
		// can't proceed even if the kill races, then abort the run.
		deny := Decision{Message: "squad sign-off callback failed; tool call denied"}
		if sendErr := s.send(controlResponseEvent(ev.RequestID, ev.Request.Input, deny)); sendErr != nil {
			logging.WarnContext(s.ctx, "claude live: failed to deliver fail-closed deny: %v", sendErr)
		}
		return fmt.Errorf("sign-off callback failed (tool %s): %w", ev.Request.ToolName, err)
	}
	logging.DebugContext(s.ctx, "claude live: can_use_tool %s -> allow=%t", ev.Request.ToolName, decision.Allow)
	if err := s.send(controlResponseEvent(ev.RequestID, ev.Request.Input, decision)); err != nil {
		return err
	}
	if decision.Interrupt {
		s.interruptSeq++
		if err := s.send(interruptEvent(fmt.Sprintf("squad-interrupt-%d", s.interruptSeq))); err != nil {
			return err
		}
	}
	return nil
}

// protocolError records one undecodable/unanswerable event and fails the
// session once maxProtocolErrors consecutive ones accumulate.
func (s *liveSession) protocolError(err error) error {
	s.protoErrs++
	logging.WarnContext(s.ctx, "claude live protocol error (%d/%d): %v", s.protoErrs, maxProtocolErrors, err)
	if s.protoErrs >= maxProtocolErrors {
		return fmt.Errorf("claude live protocol failure: %w", err)
	}
	return nil
}

// accumulate folds an assistant event's text blocks into the plan-text
// state. A new message id rolls the in-flight text into lastText first.
func (s *liveSession) accumulate(msg *streamedMsg) {
	if msg == nil {
		return
	}
	if msg.ID != s.curMsgID {
		if s.curText.Len() > 0 {
			s.lastText = s.curText.String()
		}
		s.curText.Reset()
		s.curMsgID = msg.ID
	}
	for _, block := range msg.Content {
		if block.Type != "text" || block.Text == "" {
			continue
		}
		if s.curText.Len() > 0 {
			s.curText.WriteString("\n")
		}
		s.curText.WriteString(block.Text)
		logging.DebugContext(s.ctx, "claude live assistant: %s", truncate(block.Text, 400))
	}
}

// latestAssistantText returns the most recent assistant prose (the model's
// plan): the in-flight message's text when it has any, otherwise the
// previous message's.
func (s *liveSession) latestAssistantText() string {
	if s.curText.Len() > 0 {
		return strings.TrimSpace(s.curText.String())
	}
	return strings.TrimSpace(s.lastText)
}

// send writes one newline-delimited JSON event to the CLI's stdin.
func (s *liveSession) send(event []byte) error {
	if s.stdinClosed {
		return errors.New("claude live: stdin already closed")
	}
	if _, err := s.stdin.Write(append(event, '\n')); err != nil {
		return fmt.Errorf("claude live: write stdin: %w", err)
	}
	return nil
}

// closeStdin closes the CLI's stdin once; with no more input pending the CLI
// exits after finishing its current output.
func (s *liveSession) closeStdin() {
	if s.stdinClosed {
		return
	}
	s.stdinClosed = true
	if err := s.stdin.Close(); err != nil {
		logging.Debug("claude live: close stdin: %v", err)
	}
}

// userMessageEvent frames the initial task prompt as a stream-json user event.
func userMessageEvent(text string) []byte {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	event := struct {
		Type    string `json:"type"`
		Message struct {
			Role    string      `json:"role"`
			Content []textBlock `json:"content"`
		} `json:"message"`
	}{Type: "user"}
	event.Message.Role = "user"
	event.Message.Content = []textBlock{{Type: "text", Text: text}}
	return mustMarshal(event)
}

// controlResponseEvent encodes the parent's answer to a can_use_tool request.
// Allow always carries updatedInput (the rewrite when the decision has one,
// otherwise the input the model proposed) — the shape verified against the
// live CLI.
func controlResponseEvent(requestID string, originalInput json.RawMessage, d Decision) []byte {
	inner := map[string]any{}
	if d.Allow {
		inner["behavior"] = "allow"
		input := d.UpdatedInput
		if input == nil {
			input = originalInput
		}
		inner["updatedInput"] = input
	} else {
		inner["behavior"] = "deny"
		message := d.Message
		if message == "" {
			message = "denied by squad sign-off gate"
		}
		inner["message"] = message
	}
	return mustMarshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   inner,
		},
	})
}

// controlErrorEvent encodes an error reply for a control request squad
// cannot service.
func controlErrorEvent(requestID, message string) []byte {
	return mustMarshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "error",
			"request_id": requestID,
			"error":      message,
		},
	})
}

// interruptEvent encodes a parent-initiated interrupt control request,
// aborting the CLI's current turn.
func interruptEvent(requestID string) []byte {
	return mustMarshal(map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    map[string]any{"subtype": "interrupt"},
	})
}

// mustMarshal panics on a marshal failure, which cannot happen for the fixed
// shapes above.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("agenticcli: marshal control event: %v", err))
	}
	return b
}
