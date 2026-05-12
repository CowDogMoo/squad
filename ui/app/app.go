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
	"github.com/cowdogmoo/squad/ui/presets"
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

	// launchPairs maps session ID → registry Launch ID. Filled when
	// rediscover() sees a new session dir following a recent launch
	// (FIFO pairing). Used by /cancel and k to signal the correct child.
	launchPairs     map[string]string
	pendingLaunches []string // launch IDs awaiting their session dir

	// launchBudgets maps registry Launch ID → max-cost budget the run
	// was started with. Used to render the cost progress bar in the
	// focused panel against a known cap (external runs have no entry).
	launchBudgets map[string]float64

	presets    *presets.Store      // optional — nil disables /preset
	lastLaunch *pane.LaunchRequest // remembered for /preset save

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
		static:        runs,
		selected:      selected,
		pane:          pane.NewComposer(),
		registry:      registry.New(),
		launchPairs:   map[string]string{},
		launchBudgets: map[string]float64{},
	}
}

// WithPresets attaches a presets store. Returns the receiver so calls
// chain cleanly with the constructors.
func (a App) WithPresets(store *presets.Store) App {
	a.presets = store
	return a
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
		tailers:       tailers,
		knownDirs:     known,
		sessionsRoot:  sessionsRoot,
		workingDir:    workingDir,
		selected:      selected,
		pane:          pane.NewComposer(),
		registry:      registry.New(),
		launchPairs:   map[string]string{},
		launchBudgets: map[string]float64{},
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
		case "ctrl+k":
			// ctrl+k cancels the focused run's subprocess. Bare "k"
			// would conflict with composer typing; ctrl+k is global.
			a.cancelFocused()
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
		// FIFO-pair this new session with the oldest pending launch.
		// Imperfect (race against a manual `squad run` happening at the
		// same moment), but covers the common case of "I just launched
		// from the form and the dir just appeared".
		if len(a.pendingLaunches) > 0 {
			launchID := a.pendingLaunches[0]
			a.pendingLaunches = a.pendingLaunches[1:]
			a.launchPairs[t.SessionID()] = launchID
		}
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
	case "preset":
		a.cmdPreset(parts[1:])
	case "help", "?":
		a.cmdHelp()
	case "quit", "exit":
		a.quitting = true
	default:
		a.setToast(style.Faint.Render(fmt.Sprintf("unknown command: /%s — try /help", parts[0])))
	}
}

// cmdHelp toasts the available slash commands and global keys.
func (a *App) cmdHelp() {
	a.setToast(style.Hint.Render(
		"commands: /new (launch form) · /run AGENT PROMPT · /preset save|load|list|delete NAME · /quit · /help" +
			" · keys: tab/shift-tab cycle · ctrl+k cancel · ctrl+c quit",
	))
}

// cmdPreset dispatches /preset subcommands: save, load, list, delete.
func (a *App) cmdPreset(args []string) {
	if a.presets == nil {
		a.setToast(style.Faint.Render("presets disabled (no store configured)"))
		return
	}
	if len(args) == 0 {
		a.setToast(style.Faint.Render("usage: /preset {save|load|list|delete} [name]"))
		return
	}
	switch args[0] {
	case "save":
		a.cmdPresetSave(args[1:])
	case "load":
		a.cmdPresetLoad(args[1:])
	case "list":
		a.cmdPresetList()
	case "delete", "rm":
		a.cmdPresetDelete(args[1:])
	default:
		a.setToast(style.Faint.Render(fmt.Sprintf("unknown preset action: %s", args[0])))
	}
}

func (a *App) cmdPresetSave(args []string) {
	if len(args) == 0 {
		a.setToast(style.Error.Render("usage: /preset save NAME"))
		return
	}
	name := args[0]
	if a.lastLaunch == nil {
		a.setToast(style.Error.Render("nothing to save — launch a run first"))
		return
	}
	p := presets.Preset{
		Name:       name,
		Agent:      a.lastLaunch.Agent,
		WorkingDir: a.lastLaunch.WorkingDir,
		MaxCost:    a.lastLaunch.MaxCost,
		Mode:       a.lastLaunch.Mode,
		MaxIter:    a.lastLaunch.MaxIter,
		Prompt:     a.lastLaunch.Prompt,
	}
	if err := a.presets.Set(p); err != nil {
		a.setToast(style.Error.Render("save failed: " + err.Error()))
		return
	}
	a.setToast(style.Success.Render(fmt.Sprintf("Saved preset '%s'", name)))
}

func (a *App) cmdPresetLoad(args []string) {
	if len(args) == 0 {
		a.setToast(style.Error.Render("usage: /preset load NAME"))
		return
	}
	name := args[0]
	p, ok := a.presets.Get(name)
	if !ok {
		a.setToast(style.Error.Render(fmt.Sprintf("preset '%s' not found", name)))
		return
	}
	// Open the form with the preset's values pre-populated.
	form := pane.NewLaunch(a.pane, pane.LaunchDefaults{
		Agent:      p.Agent,
		WorkingDir: presetOrDefault(p.WorkingDir, a.workingDir),
		MaxCost:    p.MaxCost,
		Mode:       p.Mode,
		MaxIter:    p.MaxIter,
	})
	form.SetSize(a.width, a.height)
	// If the preset has a prompt, seed the textarea.
	if p.Prompt != "" {
		form.SetPromptValue(p.Prompt)
	}
	a.pane = form
	a.setToast(style.Hint.Render(fmt.Sprintf("Loaded preset '%s'", name)))
}

func (a *App) cmdPresetList() {
	names := a.presets.Names()
	if len(names) == 0 {
		a.setToast(style.Faint.Render("no presets saved yet"))
		return
	}
	a.setToast(style.Hint.Render("presets: " + strings.Join(names, ", ")))
}

func (a *App) cmdPresetDelete(args []string) {
	if len(args) == 0 {
		a.setToast(style.Error.Render("usage: /preset delete NAME"))
		return
	}
	name := args[0]
	removed, err := a.presets.Remove(name)
	if err != nil {
		a.setToast(style.Error.Render("delete failed: " + err.Error()))
		return
	}
	if !removed {
		a.setToast(style.Faint.Render(fmt.Sprintf("preset '%s' not found", name)))
		return
	}
	a.setToast(style.Success.Render(fmt.Sprintf("Deleted preset '%s'", name)))
}

// presetOrDefault returns v if non-empty, otherwise fallback.
func presetOrDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// renderCostRow returns the cost metric line, with a progress bar when
// the run was launched from this TUI (so we know the budget cap).
// External runs render as a plain cost figure.
func (a App) renderCostRow(sessionID string, spent float64) string {
	budget := a.budgetFor(sessionID)
	if budget <= 0 {
		return style.Secondary.Render(fmt.Sprintf("  cost      $%.2f", spent))
	}
	bar := renderProgressBar(spent, budget, 10)
	pct := int(spent / budget * 100)
	if pct > 100 {
		pct = 100
	}
	return style.Secondary.Render(
		fmt.Sprintf("  cost      $%.2f / $%.2f  ", spent, budget),
	) + bar + style.Secondary.Render(fmt.Sprintf("  %d%%", pct))
}

// budgetFor returns the budget cap for sessionID, or 0 if the run wasn't
// launched from this TUI (we don't know the cap).
func (a App) budgetFor(sessionID string) float64 {
	launchID, ok := a.launchPairs[sessionID]
	if !ok {
		return 0
	}
	return a.launchBudgets[launchID]
}

// renderProgressBar returns a width-segment block bar. Segments fill
// green up to spent/budget, with the leading filled segment red when
// the run is over 90% of budget.
func renderProgressBar(spent, budget float64, width int) string {
	if budget <= 0 || width <= 0 {
		return ""
	}
	ratio := spent / budget
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	fillStyle := style.Success
	if ratio >= 0.9 {
		fillStyle = style.Error
	}
	var b strings.Builder
	for i := 0; i < filled; i++ {
		b.WriteString(fillStyle.Render("▰"))
	}
	for i := filled; i < width; i++ {
		b.WriteString(style.Faint.Render("▱"))
	}
	return b.String()
}

// wrapPrefix wraps `s` to fit within `width` columns, prefixing each
// continuation line with `prefix`. Word-aware (splits on whitespace).
// Used for multi-line error messages in the focused panel.
func wrapPrefix(s, prefix string, width int) string {
	if width <= len(prefix)+1 {
		return s
	}
	usable := width - len(prefix)
	if len(s) <= usable {
		return s
	}
	var out strings.Builder
	for len(s) > usable {
		// Find a space within the window to break on.
		cut := usable
		for cut > usable/2 && s[cut] != ' ' {
			cut--
		}
		if cut == usable/2 {
			cut = usable // no good break point — hard cut
		}
		out.WriteString(s[:cut])
		out.WriteByte('\n')
		out.WriteString(prefix)
		s = strings.TrimLeft(s[cut:], " ")
	}
	out.WriteString(s)
	return out.String()
}

// cancelFocused sends SIGTERM to the subprocess associated with the
// currently-selected sidebar row. No-op (with explanatory toast) when
// the row is an external session not launched by this TUI, or when the
// subprocess has already exited.
func (a *App) cancelFocused() {
	if a.selected == "" {
		a.setToast(style.Faint.Render("no run selected"))
		return
	}
	launchID, ok := a.launchPairs[a.selected]
	if !ok {
		a.setToast(style.Faint.Render("only runs launched from this TUI can be cancelled"))
		return
	}
	if err := a.registry.Stop(launchID); err != nil {
		a.setToast(style.Error.Render("cancel failed: " + err.Error()))
		return
	}
	a.setToast(style.Success.Render(fmt.Sprintf("Cancellation signal sent to %s", launchID)))
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
	a.pendingLaunches = append(a.pendingLaunches, lr.ID)
	if maxCost > 0 {
		a.launchBudgets[lr.ID] = maxCost
	}
	a.lastLaunch = &pane.LaunchRequest{
		Agent:      agent,
		Prompt:     prompt,
		WorkingDir: workingDir,
		MaxCost:    maxCost,
		Mode:       mode,
		MaxIter:    maxIter,
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
		if state.LastError != "" {
			rows = append(rows, "",
				style.Header.Render("  ERROR"),
				style.Error.Render("  "+wrapPrefix(state.LastError, "  ", width-2)),
			)
		}
		rows = append(rows, "",
			style.Header.Render("  METRICS"),
			style.Secondary.Render(fmt.Sprintf("  iter      %d", state.Counts.Iterations)),
			style.Secondary.Render(fmt.Sprintf("  tools     %d", state.Counts.ToolCalls)),
			style.Secondary.Render(fmt.Sprintf("  responses %d", state.Counts.Responses)),
			a.renderCostRow(run.ID, state.Meta.Cost),
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
