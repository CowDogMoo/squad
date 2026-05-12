// Package app is the root Bubble Tea model for the squad TUI. It composes
// three sub-views — sidebar (left), focused-run panel (right), and a
// polymorphic bottom pane (default: composer) — under a single status
// indicator. The model owns selection, dimensions, and the animation
// frame; sub-views handle their own internal state.
package app

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cowdogmoo/squad/ui/pane"
	"github.com/cowdogmoo/squad/ui/sidebar"
	"github.com/cowdogmoo/squad/ui/status"
	"github.com/cowdogmoo/squad/ui/style"
)

// sidebarWidth is the fixed column count reserved for the sidebar. The
// focused-run panel takes the rest of the width.
const sidebarWidth = 36

// frameTickMsg drives both the status shimmer and any per-frame liveness
// in sub-views. Decoupled from status.TickMsg so the app owns its own
// animation clock.
type frameTickMsg time.Time

func frameTick() tea.Cmd {
	return tea.Tick(status.TickInterval, func(t time.Time) tea.Msg { return frameTickMsg(t) })
}

// runStart pairs a sidebar.Run with a wall-clock anchor so the status
// indicator's elapsed value drifts forward in real time even though the
// run data is static (mock or session-derived).
type runStart struct {
	run     sidebar.Run
	startAt time.Time
}

// App is the root model.
type App struct {
	runs     []runStart // anchored copies of the input runs
	selected string     // session ID of currently-selected sidebar row

	pane pane.View

	width, height int
	frame         uint64

	quitting bool
}

// New returns an App with the given initial runs and the composer mounted
// as the default pane view.
func New(runs []sidebar.Run) App {
	now := time.Now()
	anchored := make([]runStart, len(runs))
	for i, r := range runs {
		anchored[i] = runStart{run: r, startAt: now.Add(-r.Elapsed)}
	}
	selected := ""
	if len(anchored) > 0 {
		selected = anchored[0].run.ID
	}
	return App{
		runs:     anchored,
		selected: selected,
		pane:     pane.NewComposer(),
	}
}

// Init starts the animation tick and forwards Init to the pane.
func (a App) Init() tea.Cmd {
	return tea.Batch(frameTick(), a.pane.Init())
}

// Update is the root message router.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		// Forward to pane so internal widgets (textarea) resize too,
		// but constrain to the pane's region (full width).
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(tea.WindowSizeMsg{Width: m.Width, Height: 3})
		return a, cmd

	case frameTickMsg:
		a.frame++
		return a, frameTick()

	case tea.KeyMsg:
		switch m.String() {
		case "ctrl+c", "ctrl+q":
			a.quitting = true
			return a, tea.Quit
		case "tab", "down", "ctrl+n":
			a.cycleSelection(+1)
			return a, nil
		case "shift+tab", "up", "ctrl+p":
			a.cycleSelection(-1)
			return a, nil
		}
		// Forward unhandled keys to the pane (typing into composer, etc.).
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(msg)
		return a, cmd

	default:
		// Forward everything else (cursor blinks, custom msgs) to the pane.
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(msg)
		// Handle pane.Submitted by clearing the pane's "submit" channel.
		// For step 4 we just drop the payload — step 5+ will dispatch it.
		if sub, ok := pane.AsSubmitted(msg); ok {
			_ = sub // placeholder; future steps route by Kind
		}
		return a, cmd
	}
}

// View renders the full screen.
func (a App) View() string {
	if a.quitting {
		return ""
	}
	w, h := a.width, a.height
	if w <= 0 {
		w = 120
	}
	if h <= 0 {
		h = 32
	}

	// Reserve rows: 2 for status+composer area + 1 separator = 3.
	bodyHeight := h - 4
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	sb := a.renderSidebar(bodyHeight)
	focus := a.renderFocused(w-sidebarWidth-2, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sb, "  ", focus)

	sep := style.Faint.Render(strings.Repeat("─", w))
	statusLine := a.renderStatus(w)
	composer := a.pane.View(w, 3)

	return strings.Join([]string{body, sep, statusLine, composer}, "\n")
}

// AsApp narrows a tea.Model back to App. Kept in production code so the
// per-file go-critic runner can resolve the type — test files alone
// cannot reference sibling-file types under gocritic's caseOrder check.
func AsApp(m tea.Model) (App, bool) {
	a, ok := m.(App)
	return a, ok
}

// Selected returns the currently-selected run's session ID, or "" if no
// run is selected. Exposed for tests and host introspection.
func (a App) Selected() string { return a.selected }

// Quitting reports whether ctrl-c has been pressed (and the next tick
// will exit the program).
func (a App) Quitting() bool { return a.quitting }

// Size returns the current (width, height) from the most recent
// WindowSizeMsg, or (0, 0) before the first resize.
func (a App) Size() (int, int) { return a.width, a.height }

func (a *App) cycleSelection(delta int) {
	if len(a.runs) == 0 {
		return
	}
	idx := 0
	for i, rs := range a.runs {
		if rs.run.ID == a.selected {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(a.runs)) % len(a.runs)
	a.selected = a.runs[idx].run.ID
}

func (a App) renderSidebar(_ int) string {
	runs := make([]sidebar.Run, len(a.runs))
	for i, rs := range a.runs {
		// Refresh elapsed so live runs drift forward each frame.
		r := rs.run
		if rs.run.Alive {
			r.Elapsed = time.Since(rs.startAt)
		}
		runs[i] = r
	}
	return sidebar.Render(sidebar.Snapshot{
		Runs:     runs,
		Selected: a.selected,
		Width:    sidebarWidth,
	})
}

func (a App) renderFocused(width, _ int) string {
	rs, ok := a.selectedRun()
	if !ok {
		return style.Faint.Render("  (no run selected)")
	}
	title := style.Title.Render(fmt.Sprintf("FOCUSED · %s", rs.run.Agent))
	rows := []string{
		title,
		"",
		style.Secondary.Render(fmt.Sprintf("  session   %s", rs.run.ID)),
		style.Secondary.Render(fmt.Sprintf("  state     %s", stateLabel(rs.run))),
		style.Secondary.Render(fmt.Sprintf("  elapsed   %s", formatHMS(liveElapsed(rs)))),
	}
	if rs.run.LastEvent != "" {
		rows = append(rows, style.Secondary.Render(fmt.Sprintf("  last      %s", rs.run.LastEvent)))
	}
	rows = append(rows,
		"",
		style.Faint.Render("  (events tail, metrics, and diff viewer coming in later steps)"),
	)
	_ = width // reserved for future column-aware rendering
	return strings.Join(rows, "\n")
}

func (a App) renderStatus(width int) string {
	rs, ok := a.selectedRun()
	if !ok {
		return status.Render(status.Snapshot{State: status.StateIdle, Width: width})
	}
	st, label := indicatorFor(rs.run)
	detail := rs.run.Agent
	if rs.run.LastEvent != "" {
		detail += " · " + rs.run.LastEvent
	}
	return status.Render(status.Snapshot{
		State:     st,
		Label:     label,
		Detail:    detail,
		Elapsed:   liveElapsed(rs),
		Frame:     a.frame,
		Width:     width,
		Interrupt: rs.run.Alive,
	})
}

func (a App) selectedRun() (runStart, bool) {
	for _, rs := range a.runs {
		if rs.run.ID == a.selected {
			return rs, true
		}
	}
	return runStart{}, false
}

// indicatorFor maps a sidebar.Run state to a status.State + label.
func indicatorFor(r sidebar.Run) (status.State, string) {
	switch r.State {
	case sidebar.StateConnecting:
		return status.StateConnecting, "Connecting"
	case sidebar.StateWorking:
		return status.StateWorking, "Working"
	case sidebar.StateNeedsInput:
		return status.StatePaused, "Needs input"
	case sidebar.StateCompleted:
		return status.StateCompleted, "Completed"
	case sidebar.StateFailed:
		return status.StateError, "Failed"
	case sidebar.StateBudget:
		return status.StatePaused, "Budget exceeded"
	}
	return status.StateIdle, ""
}

func stateLabel(r sidebar.Run) string {
	_, label := indicatorFor(r)
	if !r.Alive {
		return label + " (exited)"
	}
	return label
}

func liveElapsed(rs runStart) time.Duration {
	if rs.run.Alive {
		return time.Since(rs.startAt)
	}
	return rs.run.Elapsed
}

func formatHMS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
