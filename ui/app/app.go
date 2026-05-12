// Package app is the root Bubble Tea model for the squad TUI. It composes
// three sub-views — sidebar (left), focused-run panel (right), and a
// polymorphic bottom pane (default: composer) — under a single status
// indicator. The model owns selection, dimensions, and the animation
// frame; sub-views handle their own internal state.
package app

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/ui/pane"
	"github.com/cowdogmoo/squad/ui/registry"
	"github.com/cowdogmoo/squad/ui/sidebar"
	"github.com/cowdogmoo/squad/ui/status"
	"github.com/cowdogmoo/squad/ui/style"
	"github.com/cowdogmoo/squad/watch"
)

// sidebarWidth is the fixed column count reserved for the sidebar. The
// focused-run panel takes the rest of the width.
const sidebarWidth = 36

// eventTailRows is the default number of recent events shown in the
// focused-run panel. The renderer truncates to fit.
const eventTailRows = 8

// frameTickMsg drives both the status shimmer and per-frame liveness
// (tailer refresh, elapsed timers). Decoupled from status.TickMsg so
// the app owns its own animation clock.
type frameTickMsg time.Time

func frameTick() tea.Cmd {
	return tea.Tick(status.TickInterval, func(t time.Time) tea.Msg { return frameTickMsg(t) })
}

// App is the root model. It holds either a static list of runs (mock /
// test mode) or a set of disk-backed tailers, but not both. The renderer
// derives the sidebar list from whichever source is populated.
type App struct {
	static  []sidebar.Run
	tailers []*watch.Tailer

	sessionsRoot  string // disk-backed mode only; "" for static
	knownDirs     map[string]bool
	lastDiscovery time.Time

	registry   *registry.Registry
	workingDir string // for launched subprocesses
	toast      string // transient message ("Launched ..."), cleared after toastUntil
	toastUntil time.Time

	selected string // session ID of currently-selected sidebar row
	pane     pane.View

	width, height int
	frame         uint64

	quitting bool
}

const (
	// rediscoverEvery is how often the app re-scans the sessions root for
	// new directories. Cheap (ReadDir on a small directory) but not free.
	rediscoverEvery = 500 * time.Millisecond

	// toastDuration is how long a launch confirmation message stays
	// visible in the footer.
	toastDuration = 4 * time.Second
)

// New returns an App rendering a static list of runs (no disk I/O). Use
// this for tests and demos.
func New(runs []sidebar.Run) App {
	selected := ""
	if len(runs) > 0 {
		selected = runs[0].ID
	}
	return App{
		static:   runs,
		selected: selected,
		pane:     pane.NewComposer(),
		registry: registry.New(),
	}
}

// NewWithSessions returns an App that auto-discovers + tails the session
// directories under sessionsRoot (typically ".squad/sessions"). Missing
// root is not an error — the app renders empty until sessions appear.
// workingDir is the directory in which launched subprocesses are spawned.
func NewWithSessions(sessionsRoot, workingDir string) (App, error) {
	tailers, err := watch.Discover(sessionsRoot)
	if err != nil {
		return App{}, err
	}
	known := make(map[string]bool, len(tailers))
	for _, t := range tailers {
		known[t.Dir()] = true
		_, _ = t.Refresh()
	}
	selected := ""
	if len(tailers) > 0 {
		selected = tailers[0].SessionID()
	}
	return App{
		tailers:      tailers,
		knownDirs:    known,
		sessionsRoot: sessionsRoot,
		workingDir:   workingDir,
		selected:     selected,
		pane:         pane.NewComposer(),
		registry:     registry.New(),
	}, nil
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
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(tea.WindowSizeMsg{Width: m.Width, Height: 3})
		return a, cmd

	case frameTickMsg:
		a.frame++
		for _, t := range a.tailers {
			_, _ = t.Refresh()
		}
		// Periodically discover new session dirs (newly-launched subprocesses).
		if a.sessionsRoot != "" && time.Since(a.lastDiscovery) >= rediscoverEvery {
			a.rediscover()
		}
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
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(msg)
		return a, cmd

	default:
		// Route submits and launch requests BEFORE forwarding to the
		// pane so the pane can also clear its buffer in the same tick.
		if sub, ok := pane.AsSubmitted(msg); ok {
			a.handleSubmit(sub)
		}
		if req, ok := pane.AsLaunchRequest(msg); ok {
			a.launch(req.Agent, req.Prompt, req.WorkingDir, req.MaxCost, req.Mode, req.MaxIter)
		}
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(msg)
		return a, cmd
	}
}

// View renders the full screen.
func (a App) View() string {
	if a.quitting {
		return ""
	}
	w := a.width
	if w <= 0 {
		w = 120
	}

	sb := a.renderSidebar()
	focus := a.renderFocused(w - sidebarWidth - 2)
	body := lipgloss.JoinHorizontal(lipgloss.Top, sb, "  ", focus)

	sep := style.Faint.Render(strings.Repeat("─", w))
	statusLine := a.renderStatus(w)
	composer := a.pane.View(w, 3)

	parts := []string{body, sep, statusLine}
	if toast := a.currentToast(); toast != "" {
		parts = append(parts, toast)
	}
	parts = append(parts, composer)
	return strings.Join(parts, "\n")
}

// AsApp narrows a tea.Model back to App. Kept in production code so the
// per-file go-critic runner can resolve the type — test files alone
// cannot reference sibling-file types under gocritic's caseOrder check.
func AsApp(m tea.Model) (App, bool) {
	a, ok := m.(App)
	return a, ok
}

// Selected returns the currently-selected run's session ID, or "" if no
// run is selected.
func (a App) Selected() string { return a.selected }

// Quitting reports whether ctrl-c has been pressed.
func (a App) Quitting() bool { return a.quitting }

// Size returns the current (width, height) from the most recent
// WindowSizeMsg, or (0, 0) before the first resize.
func (a App) Size() (int, int) { return a.width, a.height }

// currentRuns derives the sidebar's row list from whichever source is
// populated. Returns a fresh slice each call.
func (a App) currentRuns() []sidebar.Run {
	if len(a.tailers) > 0 {
		out := make([]sidebar.Run, 0, len(a.tailers))
		for _, t := range a.tailers {
			out = append(out, runFromTailer(t))
		}
		return out
	}
	return a.static
}

// rediscover scans sessionsRoot for any session dirs not yet known and
// adds a fresh Tailer for each. Called from the frame loop at most every
// rediscoverEvery; cost is one ReadDir.
func (a *App) rediscover() {
	a.lastDiscovery = time.Now()
	all, err := watch.Discover(a.sessionsRoot)
	if err != nil {
		return
	}
	for _, t := range all {
		if a.knownDirs[t.Dir()] {
			continue
		}
		_, _ = t.Refresh()
		a.tailers = append([]*watch.Tailer{t}, a.tailers...) // prepend (newest first)
		a.knownDirs[t.Dir()] = true
		// If nothing was selected before, focus the new session.
		if a.selected == "" {
			a.selected = t.SessionID()
		}
	}
}

// handleSubmit dispatches a pane.Submitted into the right action. Today:
// only "/run agent prompt" is wired; other commands are toasted as
// unknown so the user sees something happen.
func (a *App) handleSubmit(sub pane.Submitted) {
	switch sub.Kind {
	case pane.KindCommand:
		a.handleCommand(sub.Text)
	case pane.KindShell:
		a.setToast(style.Faint.Render("shell pass-through is not yet wired"))
	case pane.KindFile:
		a.setToast(style.Faint.Render("file mention picker coming in a later step"))
	default:
		a.setToast(style.Faint.Render("bare prompts need an agent — try /run <agent> <prompt>"))
	}
}

func (a *App) handleCommand(text string) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "run":
		a.cmdRun(parts[1:])
	case "new":
		a.openLaunchForm()
	case "quit", "exit":
		a.quitting = true
	default:
		a.setToast(style.Faint.Render(fmt.Sprintf("unknown command: /%s", parts[0])))
	}
}

// openLaunchForm swaps the bottom pane to a Launch view. The composer
// is preserved as the form's parent so Esc / submit returns to it.
func (a *App) openLaunchForm() {
	form := pane.NewLaunch(a.pane, pane.LaunchDefaults{
		WorkingDir: a.workingDir,
	})
	form.SetSize(a.width, a.height)
	a.pane = form
}

func (a *App) cmdRun(args []string) {
	if len(args) < 2 {
		a.setToast(style.Error.Render("usage: /run <agent> <prompt>"))
		return
	}
	agent := args[0]
	prompt := strings.Join(args[1:], " ")
	a.launch(agent, prompt, "", 0, "", 0)
}

// launch is the single subprocess-launch entry point. Empty workingDir,
// maxCost, mode, or maxIter fall back to the app's defaults or squad
// run's defaults (skipped from argv when zero).
func (a *App) launch(agent, prompt, workingDir string, maxCost float64, mode string, maxIter int) {
	if a.registry == nil {
		a.setToast(style.Error.Render("registry not initialized"))
		return
	}
	if workingDir == "" {
		workingDir = a.workingDir
	}
	if workingDir == "" {
		workingDir = "."
	}
	argv := []string{
		registry.SquadBinary(),
		"run",
		"--agent", agent,
		"--prompt", prompt,
		"--working-dir", workingDir,
		"--print=false",
	}
	if maxCost > 0 {
		argv = append(argv, "--max-cost", fmt.Sprintf("%g", maxCost))
	}
	if mode != "" {
		argv = append(argv, "--mode", mode)
	}
	if maxIter > 0 {
		argv = append(argv, "--max-iterations", strconv.Itoa(maxIter))
	}
	lr, err := a.registry.Launch(workingDir, argv)
	if err != nil {
		a.setToast(style.Error.Render("launch failed: " + err.Error()))
		return
	}
	a.setToast(style.Success.Render(fmt.Sprintf("Launched %s (%s)", agent, lr.ID)))
	// Force a discovery on the next tick so the new session shows up promptly.
	a.lastDiscovery = time.Time{}
}

func (a *App) setToast(msg string) {
	a.toast = msg
	a.toastUntil = time.Now().Add(toastDuration)
}

func (a App) currentToast() string {
	if a.toast == "" || time.Now().After(a.toastUntil) {
		return ""
	}
	return a.toast
}

func (a *App) cycleSelection(delta int) {
	runs := a.currentRuns()
	if len(runs) == 0 {
		return
	}
	idx := 0
	for i, r := range runs {
		if r.ID == a.selected {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(runs)) % len(runs)
	a.selected = runs[idx].ID
}

func (a App) renderSidebar() string {
	return sidebar.Render(sidebar.Snapshot{
		Runs:        a.currentRuns(),
		Selected:    a.selected,
		Width:       sidebarWidth,
		MaxPerGroup: 12,
	})
}

func (a App) renderFocused(width int) string {
	run, ok := a.selectedSidebarRun()
	if !ok {
		return style.Faint.Render("  (no run selected)")
	}
	title := style.Title.Render(fmt.Sprintf("FOCUSED · %s", run.Agent))
	rows := []string{
		title,
		"",
		style.Secondary.Render(fmt.Sprintf("  session   %s", run.ID)),
		style.Secondary.Render(fmt.Sprintf("  state     %s", stateLabel(run))),
		style.Secondary.Render(fmt.Sprintf("  elapsed   %s", formatHMS(run.Elapsed))),
	}
	if run.LastEvent != "" {
		rows = append(rows, style.Secondary.Render(fmt.Sprintf("  last      %s", run.LastEvent)))
	}

	// Tailer-backed runs get a metrics block + recent events tail.
	if state, ok := a.tailerStateFor(run.ID); ok {
		rows = append(rows, "",
			style.Header.Render("  METRICS"),
			style.Secondary.Render(fmt.Sprintf("  iter      %d", state.Counts.Iterations)),
			style.Secondary.Render(fmt.Sprintf("  tools     %d", state.Counts.ToolCalls)),
			style.Secondary.Render(fmt.Sprintf("  responses %d", state.Counts.Responses)),
			style.Secondary.Render(fmt.Sprintf("  cost      $%.2f", state.Meta.Cost)),
			style.Secondary.Render(fmt.Sprintf("  tokens    %d in · %d out", state.Meta.InputTokens, state.Meta.OutputTokens)),
		)
		if events := state.Events; len(events) > 0 {
			rows = append(rows, "", style.Header.Render("  EVENTS"))
			rows = append(rows, renderEventTail(events, eventTailRows, width-4)...)
		}
	} else {
		rows = append(rows, "",
			style.Faint.Render("  (events tail loads when this run has a session on disk)"),
		)
	}
	return strings.Join(rows, "\n")
}

// renderEventTail renders the most recent n events (newest at top).
func renderEventTail(events []watch.EventLine, n, width int) []string {
	if n <= 0 {
		return nil
	}
	start := len(events) - n
	if start < 0 {
		start = 0
	}
	recent := events[start:]
	// Reverse so newest is at the top.
	rows := make([]string, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		ev := recent[i]
		ts := ev.Ts.Local().Format("15:04:05")
		// Pad type to a fixed width for alignment.
		typ := padType(ev.Type, 12)
		summary := ev.Summary
		// Truncate summary to fit terminal width.
		const tsW, typW, gutterW = 8, 12, 6 // approx columns
		maxSummary := width - tsW - typW - gutterW
		if maxSummary > 0 && len(summary) > maxSummary {
			summary = summary[:maxSummary-1] + "…"
		}
		line := fmt.Sprintf("  %s  %s  %s",
			style.Secondary.Render(ts),
			style.Hint.Render(typ),
			style.Body.Render(summary),
		)
		rows = append(rows, line)
	}
	return rows
}

func padType(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

func (a App) renderStatus(width int) string {
	run, ok := a.selectedSidebarRun()
	if !ok {
		return status.Render(status.Snapshot{State: status.StateIdle, Width: width})
	}
	st, label := indicatorFor(run)
	detail := run.Agent
	if run.LastEvent != "" {
		detail += " · " + run.LastEvent
	}
	return status.Render(status.Snapshot{
		State:     st,
		Label:     label,
		Detail:    detail,
		Elapsed:   run.Elapsed,
		Frame:     a.frame,
		Width:     width,
		Interrupt: run.Alive,
	})
}

func (a App) selectedSidebarRun() (sidebar.Run, bool) {
	for _, r := range a.currentRuns() {
		if r.ID == a.selected {
			return r, true
		}
	}
	return sidebar.Run{}, false
}

func (a App) tailerStateFor(sessionID string) (watch.State, bool) {
	for _, t := range a.tailers {
		if t.SessionID() == sessionID {
			return t.State(), true
		}
	}
	return watch.State{}, false
}

// runFromTailer converts a tailer's current state into a sidebar row.
// Elapsed is computed live for alive sessions, frozen otherwise.
func runFromTailer(t *watch.Tailer) sidebar.Run {
	s := t.State()
	meta := s.Meta
	state, alive := stateFromMeta(meta.Status)
	elapsed := time.Duration(0)
	if alive && !meta.Created.IsZero() {
		elapsed = time.Since(meta.Created)
	} else if !meta.Created.IsZero() && !meta.Updated.IsZero() {
		elapsed = meta.Updated.Sub(meta.Created)
	}
	agent := meta.Agent
	if agent == "" {
		agent = filepath.Base(t.Dir())
	}
	return sidebar.Run{
		ID:        t.SessionID(),
		Agent:     agent,
		State:     state,
		Alive:     alive,
		Elapsed:   elapsed,
		LastEvent: s.LastTool,
	}
}

// stateFromMeta maps meta.Status to a sidebar State + alive flag. Empty
// status (meta.json not yet written) is treated as Connecting.
func stateFromMeta(status string) (sidebar.State, bool) {
	switch status {
	case session.StatusRunning:
		return sidebar.StateWorking, true
	case session.StatusCompleted:
		return sidebar.StateCompleted, false
	case session.StatusError:
		return sidebar.StateFailed, false
	case session.StatusBudget:
		return sidebar.StateBudget, false
	case "":
		return sidebar.StateConnecting, true
	default:
		return sidebar.StateCompleted, false
	}
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
