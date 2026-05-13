// Package sidebar renders the left-hand run list for the squad TUI: a
// vertical, grouped, selectable list of active and recent agent runs.
//
// Each row encodes two orthogonal signals via shape and color:
//
//	shape  — process liveness (▶ alive, ◇ alive-paused, ∙ exited)
//	color  — run state (cyan working, green completed, red failed)
//
// The renderer is pure: a Snapshot in, a styled string out. Tests use
// fixed-width fixtures plus ANSI stripping for stable goldens; the app
// layer handles input routing and state transitions.
package sidebar

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/cowdogmoo/squad/ui/style"
)

// State is the run's high-level status. Orthogonal to Run.Alive: an
// agent can be Working+alive (normal), NeedsInput+alive (paused for a
// human), or Completed+!alive (terminal).
type State int

const (
	StateConnecting State = iota
	StateWorking
	StateNeedsInput
	StateCompleted
	StateFailed
	StateBudget
)

// Run is one row's worth of data. Designed to be cheap to construct
// from a session.Meta + a liveness probe.
type Run struct {
	ID        string // session ID — opaque key for selection
	Agent     string // e.g. "go-review" or "bug-hunt:fix-bug"
	State     State
	Alive     bool // is the subprocess still running?
	Elapsed   time.Duration
	LastEvent string // optional one-liner; rendered dim on selected row
}

// Group is a named bucket of rows. The default grouping function buckets
// runs into Working / Needs input / Recent based on State + Alive; callers
// can pass a custom Group slice to override.
type Group struct {
	Title string
	Runs  []Run
}

// Snapshot is the input to Render.
type Snapshot struct {
	Groups   []Group // pre-bucketed; if nil, derive via Bucket(Runs, ...)
	Runs     []Run   // used when Groups is nil
	Selected string  // session ID of the currently selected row
	Width    int     // terminal column width for this pane
	// MaxPerGroup truncates each group with a "… N more" footer. 0 = no
	// truncation.
	MaxPerGroup int
}

// Bucket partitions runs into the default group set: Working / Needs Input /
// Recent. Recent excludes anything older than `recentWindow` (use 24h by
// default; pass 0 for "all terminal runs").
func Bucket(runs []Run, recentWindow time.Duration) []Group {
	working := Group{Title: "WORKING"}
	needsInput := Group{Title: "NEEDS INPUT"}
	recent := Group{Title: "RECENT"}
	for _, r := range runs {
		switch {
		case r.Alive && (r.State == StateWorking || r.State == StateConnecting):
			working.Runs = append(working.Runs, r)
		case r.Alive && (r.State == StateNeedsInput || r.State == StateBudget):
			needsInput.Runs = append(needsInput.Runs, r)
		case !r.Alive:
			if recentWindow == 0 || r.Elapsed >= 0 {
				recent.Runs = append(recent.Runs, r)
			}
		}
	}
	out := []Group{}
	if len(working.Runs) > 0 {
		out = append(out, working)
	}
	if len(needsInput.Runs) > 0 {
		out = append(out, needsInput)
	}
	if len(recent.Runs) > 0 {
		out = append(out, recent)
	}
	return out
}

// Render returns the sidebar's full styled output. Lines are joined with
// "\n"; no trailing newline.
func Render(s Snapshot) string {
	groups := s.Groups
	if groups == nil {
		groups = Bucket(s.Runs, 0)
	}
	width := s.Width
	if width <= 0 {
		width = 32
	}

	if len(groups) == 0 {
		return style.Faint.Render("  (no runs)")
	}

	var sections []string
	for _, g := range groups {
		sections = append(sections, renderGroup(g, s.Selected, s.MaxPerGroup, width))
	}
	return strings.Join(sections, "\n\n")
}

func renderGroup(g Group, selected string, maxPerGroup, width int) string {
	header := style.Title.Render(fmt.Sprintf("%s (%d)", g.Title, len(g.Runs)))
	rows := g.Runs
	truncated := 0
	if maxPerGroup > 0 && len(rows) > maxPerGroup {
		truncated = len(rows) - maxPerGroup
		rows = rows[:maxPerGroup]
	}
	out := []string{header}
	for _, r := range rows {
		out = append(out, renderRow(r, r.ID == selected, width))
	}
	if truncated > 0 {
		out = append(out, style.Faint.Render(fmt.Sprintf("  … %d more", truncated)))
	}
	return strings.Join(out, "\n")
}

func renderRow(r Run, selected bool, width int) string {
	glyph, glyphStyle := glyphFor(r.State, r.Alive)
	agentStyle := style.Body
	elapsedStyle := style.Secondary
	prefix := "  "
	if selected {
		prefix = style.Hint.Render("› ")
		agentStyle = style.Hint
		elapsedStyle = style.Hint
	}

	left := prefix + glyphStyle.Render(glyph) + " " + agentStyle.Render(r.Agent)
	right := elapsedStyle.Render(formatElapsed(r.Elapsed))

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	pad := width - leftW - rightW
	if pad < 1 {
		// Truncate agent name to make room for the elapsed column.
		// Reserve room for a leading marker + glyph + space + ellipsis + space + elapsed.
		maxAgentW := width - lipgloss.Width(prefix) - 2 - 1 - 1 - rightW
		if maxAgentW < 3 {
			maxAgentW = 3
		}
		agent := truncate(r.Agent, maxAgentW)
		left = prefix + glyphStyle.Render(glyph) + " " + agentStyle.Render(agent)
		leftW = lipgloss.Width(left)
		pad = width - leftW - rightW
		if pad < 1 {
			pad = 1
		}
	}
	return left + strings.Repeat(" ", pad) + right
}

// glyphFor returns the (rune, style) pair for a (state, alive) combination.
// Shape encodes liveness; color encodes state.
func glyphFor(state State, alive bool) (string, lipgloss.Style) {
	if alive {
		switch state {
		case StateWorking, StateConnecting:
			return "▶", style.Working
		case StateNeedsInput:
			return "◇", style.Hint
		case StateBudget:
			return "◇", style.Error.Bold(false)
		default:
			return "▶", style.Working
		}
	}
	switch state {
	case StateCompleted:
		return "∙", style.Success
	case StateFailed:
		return "∙", style.Error
	case StateBudget:
		return "∙", style.Error.Bold(false)
	default:
		return "∙", style.Faint
	}
}

// formatElapsed renders durations short enough to fit in the sidebar's
// right column. Compatible with status.formatElapsed but without the spaces:
// `8s`, `1m24s`, `1h12m04s`.
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh%02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// truncate cuts a string to width characters, appending an ellipsis if
// needed. Width-aware via lipgloss.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	// Cheap byte-level cut; sidebar labels are ASCII agent names.
	if w > len(s) {
		return s
	}
	return s[:w-1] + "…"
}
