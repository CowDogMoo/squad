// Package watch tails an active or completed squad session directory
// (.squad/sessions/<id>/) and folds the events.jsonl + meta.json contents
// into a single observable State. The TUI consumes State to render the
// sidebar, focused panel, and event tail; CLI tools can consume it for
// once-off rendering (squad watch --once <id>).
//
// The tailer is stateful: it tracks the byte offset into events.jsonl and
// the last-known mtime of meta.json so repeated Refresh() calls cost O(new
// bytes), not O(file size). Event tails are capped (default 200) so memory
// stays bounded for long-running sessions.
package watch

import (
	"time"

	"github.com/cowdogmoo/squad/session"
)

// DefaultEventCap is the upper bound on retained events in State.Events.
// Older entries are dropped from the front as new ones arrive.
const DefaultEventCap = 200

// State is the consolidated view of a session at a point in time. The
// renderer is pure over a State value — no I/O during render.
type State struct {
	Meta        session.Meta // last-known meta.json contents
	Events      []EventLine  // capped recent events, oldest first
	Counts      Counts       // running totals
	LastTool    string       // name of the most recent tool_call
	LastError   string       // most recent error message (from error or run_end events)
	LastEventAt time.Time    // ts of the most recent event
}

// EventLine is one summarized row for the events tail panel. Summary is
// rendered as-is (no further parsing) by the renderer.
type EventLine struct {
	Ts      time.Time
	Type    string // session.Event* constant
	Summary string // one-line, width-truncatable
}

// Counts tracks running totals derived from events.jsonl. Useful for the
// header tallies (iter, tools called, error count, etc.).
type Counts struct {
	Iterations   int
	ToolCalls    int
	Responses    int
	Errors       int
	LargeResults int
}
