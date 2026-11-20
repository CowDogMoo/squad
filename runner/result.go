package runner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
)

// routineStatusBlock is appended to the system prompt of a routine fire. An
// unattended fire has nobody reading its prose, so the agent ends with a
// machine-readable verdict that squad checks (reportedStatus) before it
// records the fire as ok.
const routineStatusBlock = `

## Run status

This run is an unattended routine fire. End your final response with exactly
one status line, alone on the last line:

STATUS: ok
STATUS: skipped: <why there was nothing to do>
STATUS: blocked: <what stopped you>

Report blocked whenever you could not do the job: a tool, MCP server or
credential you need is missing or failing, a sign-in is required, or the input
you need is absent. A fire whose last line reports blocked is recorded as
failed.
`

// statusLine matches a "STATUS: value[: detail]" line, tolerating the
// markdown decoration models add (bold, code spans, list or quote markers).
var statusLine = regexp.MustCompile("(?i)^[\\s>*_`#-]*status[\\s*_`]*:[\\s*_`]*([a-z]+)[\\s*_`]*(?:[:—–-]\\s*)?(.*)$")

// reportedStatus reads the status the agent reported on the last non-empty
// line of its final response, written as "STATUS: <value>[: detail]". It
// returns the lowercased status and its detail, or "" when the response ends
// without one.
func reportedStatus(response string) (status, detail string) {
	lines := strings.Split(strings.TrimSpace(response), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		m := statusLine.FindStringSubmatch(line)
		if m == nil {
			return "", ""
		}
		return strings.ToLower(m[1]), strings.Trim(strings.TrimSpace(m[2]), "*_`")
	}
	return "", ""
}

// applyRoutineStatusContract asks a routine fire's agent to end with a
// status line. Interactive and one-off runs are left alone: a person reads
// their output.
func applyRoutineStatusContract(opts *RunOptions, bundle *agent.Bundle) {
	if opts.RoutineID == "" {
		return
	}
	bundle.System += routineStatusBlock
}

// routineStatusError fails a routine fire whose agent reported blocked, so
// the fire is recorded failed rather than ok.
func routineStatusError(opts *RunOptions, status, detail string) error {
	if opts.RoutineID == "" || status != "blocked" {
		return nil
	}
	if detail == "" {
		return fmt.Errorf("routine fire failed: the agent reported %s", status)
	}
	return fmt.Errorf("routine fire failed: the agent reported %s: %s", status, detail)
}

// recordResult persists the run's final response, with the status it
// reported, as a result event in the session log. Without it an agentic CLI
// run leaves no record of what the agent actually said.
func recordResult(ctx context.Context, logger *session.Logger, response, status string) {
	if logger == nil || response == "" {
		return
	}
	payload := map[string]any{"text": response}
	if status != "" {
		payload["reported_status"] = status
	}
	if err := logger.AppendContext(ctx, session.EventResult, payload); err != nil {
		logging.Warn("failed to record run result: %v", err)
	}
}
