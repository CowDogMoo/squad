package watch

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
)

// summarize renders a one-line description of a session event for the
// tail panel. Payload shapes are best-effort: unknown fields are ignored,
// missing fields render as zero. Callers truncate by width.
func summarize(ev session.Event) string {
	p := decodePayload(ev.Payload)
	if fn, ok := summarizers[ev.Type]; ok {
		return fn(p)
	}
	return ev.Type
}

// summarizers dispatches per-event-type formatters. Adding a new event
// type is one map entry — keeps summarize() flat and cyclo-friendly.
var summarizers = map[string]func(map[string]any) string{
	session.EventRunStart:    sumRunStart,
	session.EventResume:      sumResume,
	session.EventPrompt:      sumPrompt,
	session.EventIteration:   sumIteration,
	session.EventToolCall:    sumToolCall,
	session.EventToolResult:  sumToolResult,
	session.EventToolDenied:  sumToolDenied,
	session.EventLargeResult: sumLargeResult,
	session.EventResponse:    sumResponse,
	session.EventError:       sumError,
	session.EventRunEnd:      sumRunEnd,
}

func decodePayload(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var p map[string]any
	if err := json.Unmarshal(raw, &p); err != nil {
		logging.Warn("failed to decode event payload: %v", err)
		return nil
	}
	return p
}

func sumRunStart(p map[string]any) string {
	return fmt.Sprintf("started · agent=%s model=%s", str(p, "agent"), str(p, "model"))
}

func sumResume(p map[string]any) string {
	return fmt.Sprintf("resumed · from=%s", str(p, "session_id"))
}

func sumPrompt(p map[string]any) string {
	text := str(p, "text")
	if text == "" {
		text = "(prompt sent)"
	}
	return "prompt · " + Truncate(text, 80)
}

func sumIteration(p map[string]any) string {
	return fmt.Sprintf("iter %s · %s tool calls", num(p, "index"), num(p, "tool_calls"))
}

// oneLine collapses whitespace runs (including newlines) so multi-line text
// fits a single tail row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func sumToolCall(p map[string]any) string {
	name := str(p, "name")
	args := str(p, "args")
	if args == "" {
		return name
	}
	return fmt.Sprintf("%s  %s", name, Truncate(oneLine(args), 80))
}

func sumToolResult(p map[string]any) string {
	out := fmt.Sprintf("result · %s", str(p, "name"))
	if _, ok := p["bytes"]; ok {
		out += fmt.Sprintf(" · %s B", num(p, "bytes"))
	}
	if _, ok := p["duration_ms"]; ok {
		out += fmt.Sprintf(" · %sms", num(p, "duration_ms"))
	}
	if e := str(p, "error"); e != "" {
		out += " · error: " + Truncate(oneLine(e), 60)
	}
	return out
}

func sumToolDenied(p map[string]any) string {
	return fmt.Sprintf("denied · %s · %s", str(p, "name"), str(p, "reason"))
}

func sumLargeResult(p map[string]any) string {
	return fmt.Sprintf("large_result · %s", str(p, "result_id"))
}

func sumResponse(p map[string]any) string {
	in := num(p, "input_tokens")
	out := num(p, "output_tokens")
	head := "response"
	if label := str(p, "label"); label != "" {
		head = fmt.Sprintf("response (%s)", label)
	} else if _, ok := p["iteration"]; ok {
		head = fmt.Sprintf("response (iter %s)", num(p, "iteration"))
	}
	line := fmt.Sprintf("%s · +%s in / +%s out", head, in, out)
	if text := str(p, "text"); text != "" {
		line += " · " + Truncate(oneLine(text), 100)
	}
	return line
}

func sumError(p map[string]any) string {
	msg := str(p, "error")
	if msg == "" {
		msg = str(p, "message")
	}
	return "error · " + Truncate(msg, 80)
}

func sumRunEnd(p map[string]any) string {
	st := str(p, "status")
	if e := str(p, "error"); e != "" {
		return fmt.Sprintf("ended · %s · %s", st, Truncate(e, 60))
	}
	return "ended · " + st
}

func str(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	if v, ok := p[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// num renders a numeric payload field as a string. Accepts JSON numbers
// (float64) or strings. Returns "0" for missing/unknown.
func num(p map[string]any, key string) string {
	if p == nil {
		return "0"
	}
	v, ok := p[key]
	if !ok {
		return "0"
	}
	switch x := v.(type) {
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	case string:
		return x
	default:
		return "0"
	}
}

// Truncate cuts s to w bytes, ending the cut with an ellipsis. It backs up to
// a rune boundary so the cut never splits a UTF-8 character. A non-positive w
// leaves s unchanged.
func Truncate(s string, w int) string {
	if w <= 0 || len(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	n := w - 1
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
