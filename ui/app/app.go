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
	agents     []string            // names of discoverable agents, fed to the launch form

	selected string // session ID of currently-selected sidebar row
	pane     pane.View

	// focusedRegion routes ↑/↓ to the right sub-view: composer (history),
	// sidebar (cycle runs), or the focused-detail panel (no-op for now).
	// Tab/Shift+Tab cycle the regions when no modal is active.
	focusedRegion focusRegion

	width, height int
	frame         uint64

	quitting bool
}

// focusRegion is the App's "which sub-view owns arrow keys right now"
// state. Three values keep the cycle small; new regions can slot in
// later if the UI grows panels.
type focusRegion int

const (
	regionComposer focusRegion = iota
	regionSidebar
	regionFocused
	regionCount
)

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

// WithAgents seeds the discovered-agents list shown in the launch form
// typeahead. Order is preserved; callers should pass already-sorted names.
func (a App) WithAgents(names []string) App {
	a.agents = append([]string(nil), names...)
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
		a.pane, cmd = a.pane.Update(m)
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
		// ctrl+c always quits. Modal pane (launch form) handles its own
		// keys, so everything else is forwarded unchanged.
		if m.String() == "ctrl+c" || m.String() == "ctrl+q" {
			a.quitting = true
			return a, tea.Quit
		}
		if _, modal := pane.AsLaunchView(a.pane); !modal {
			if cmd, handled := a.handleRegionKey(m); handled {
				return a, cmd
			}
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
			a.launch(req)
		}
		var cmd tea.Cmd
		a.pane, cmd = a.pane.Update(msg)
		return a, cmd
	}
}

// View renders the full screen.
//
// Layout:
//
//	╭── SQUAD ──────────────────────────────────────────────────╮
//	│  RUNS                    FOCUSED                          │
//	│  WORKING (3)             session id ...                   │
//	│  ▶ go-review             metrics, events, errors          │
//	│  ...                                                      │
//	│  ✻ Working · go-review · 3m 12s          esc to interrupt │
//	╰───────────────────────────────────────────────────────────╯
//	  toast message (if any)
//	> type a prompt, /command, !shell, or @file …
//
// The panel is padded vertically to fill (height - toast - composer), so
// the chrome reaches the bottom of the terminal instead of leaving dead
// black space below.
func (a App) View() string {
	if a.quitting {
		return ""
	}
	w := a.width
	if w <= 0 {
		w = 120
	}
	h := a.height
	if h <= 0 {
		h = 30
	}

	// Modal panes (the launch form) take over the whole screen — no
	// SQUAD panel above. The pane is responsible for filling the height
	// when given a non-zero second argument.
	if _, ok := pane.AsLaunchView(a.pane); ok {
		return a.pane.View(w, h)
	}

	innerW := w - 4 // panel border (2) + padding (2)
	if innerW < 40 {
		innerW = 40
	}

	// Decide on layout: empty state collapses to a single full-width
	// welcome card (no sidebar split) so the first launch doesn't feel
	// like a barren grid.
	var body string
	if len(a.currentRuns()) == 0 {
		body = padBlock(a.renderWelcome(innerW), innerW)
	} else {
		sidebarW := sidebarWidth
		if sidebarW > innerW/2 {
			sidebarW = innerW / 2
		}
		focusW := innerW - sidebarW - 2
		sbBody := padBlock(a.renderSidebar(sidebarW), sidebarW)
		focusBody := padBlock(a.renderFocused(focusW), focusW)
		body = lipgloss.JoinHorizontal(lipgloss.Top, sbBody, "  ", focusBody)
	}

	composer := a.pane.View(w, 0)
	composerH := strings.Count(composer, "\n") + 1
	focusBar := a.renderFocusBar()
	focusBarH := 1
	toast := a.currentToast()
	toastH := 0
	if toast != "" {
		toastH = 1
	}

	// Status indicator sits on the last body row of the panel so the
	// chrome encloses it — no orphaned text between panel and composer.
	statusLine := a.renderStatus(innerW)

	panelH := h - composerH - toastH - focusBarH
	minH := style.PanelHeight(body) + 1 // body + 1 row for status
	if panelH < minH {
		panelH = minH
	}
	// Pad the body up to (panelH - 2 borders - 1 status row) blank rows,
	// then append the status line so it pins to the bottom of the panel.
	bodyRows := strings.Count(body, "\n") + 1
	pad := panelH - 2 - 1 - bodyRows
	if pad < 0 {
		pad = 0
	}
	panelBody := body + strings.Repeat("\n", pad+1) + statusLine

	panel := style.PanelFixed("SQUAD", panelBody, w, panelH)

	parts := []string{panel, focusBar}
	if toast != "" {
		parts = append(parts, toast)
	}
	parts = append(parts, composer)
	return strings.Join(parts, "\n")
}

// renderFocusBar is the one-line strip between the SQUAD panel and the
// composer. Shows which region currently owns Tab focus so the user can
// see what they're cycling — arrow keys themselves are not region-
// gated, but Tab + the focus indicator give a coherent affordance.
func (a App) renderFocusBar() string {
	labels := []struct {
		region focusRegion
		name   string
	}{
		{regionComposer, "composer"},
		{regionSidebar, "sidebar"},
		{regionFocused, "focused"},
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		if l.region == a.focusedRegion {
			parts = append(parts, style.Hint.Render("● "+l.name))
		} else {
			parts = append(parts, style.Faint.Render("· "+l.name))
		}
	}
	hint := style.Faint.Render("tab cycles · ctrl+↑/↓ history")
	return "  " + strings.Join(parts, "  ") + "    " + hint
}

// padBlock normalizes each line of s to exactly `width` visible columns:
// shorter lines are right-padded with spaces, longer lines are truncated
// with an ellipsis. Used to give both sidebar and focused-panel columns
// matching widths so JoinHorizontal produces a clean grid even when one
// side is shorter and the other might overflow on a narrow terminal.
func padBlock(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		visW := lipgloss.Width(line)
		switch {
		case visW < width:
			lines[i] = line + strings.Repeat(" ", width-visW)
		case visW > width:
			lines[i] = truncateAnsi(line, width)
		}
	}
	return strings.Join(lines, "\n")
}

// truncateAnsi cuts a styled line to `width` visible columns, appending
// an ellipsis. ANSI escape sequences pass through untouched so the
// remaining color/bold styling stays intact.
func truncateAnsi(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return "…"
	}
	target := width - 1
	in := []rune(s)
	var b strings.Builder
	visible := 0
	for i := 0; i < len(in); i++ {
		r := in[i]
		if r == 0x1b && i+1 < len(in) && in[i+1] == '[' {
			b.WriteRune(r)
			i++
			for i < len(in) {
				b.WriteRune(in[i])
				c := in[i]
				if c >= 0x40 && c <= 0x7e {
					break
				}
				i++
			}
			continue
		}
		if visible >= target {
			b.WriteRune('…')
			break
		}
		b.WriteRune(r)
		visible++
	}
	return b.String()
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

// handleSubmit dispatches a pane.Submitted into the right action.
// Commands route through handleCommand; other kinds toast a not-yet-
// wired hint so the user sees feedback.
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
		"commands: /run (launch form) · /preset save|load|list|delete NAME · /quit · /help" +
			" · keys: tab cycle regions · ↑/↓ sidebar · ctrl+↑/↓ history · ctrl+k cancel · ctrl+c quit",
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
		Provider:   a.lastLaunch.Provider,
		Model:      a.lastLaunch.Model,
		Isolate:    a.lastLaunch.Isolate,
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
		Provider:   p.Provider,
		Model:      p.Model,
		Isolate:    p.Isolate,
		Agents:     a.agents,
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

// truncate cuts s to width chars, appending an ellipsis. Used for
// single-line summaries that need to fit a column budget.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return s[:width-1] + "…"
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
		Agents:     a.agents,
	})
	form.SetSize(a.width, a.height)
	a.pane = form
}

// cmdRun opens the launch form. Inline launches belong on the CLI
// (`squad run ...`); inside the TUI the form is the only path so all
// fields stay editable until the user actually submits.
func (a *App) cmdRun(_ []string) {
	a.openLaunchForm()
}

// launch is the single subprocess-launch entry point. Empty optional
// fields fall back to the app's defaults or squad run's defaults
// (skipped from argv when zero).
func (a *App) launch(req pane.LaunchRequest) {
	if a.registry == nil {
		a.setToast(style.Error.Render("registry not initialized"))
		return
	}
	workingDir := req.WorkingDir
	if workingDir == "" {
		workingDir = a.workingDir
	}
	if workingDir == "" {
		workingDir = "."
	}
	argv := []string{
		registry.SquadBinary(),
		"run",
		"--agent", req.Agent,
		"--prompt", req.Prompt,
		"--working-dir", workingDir,
		"--print=false",
	}
	if req.MaxCost > 0 {
		argv = append(argv, "--max-cost", fmt.Sprintf("%g", req.MaxCost))
	}
	if req.Mode != "" {
		argv = append(argv, "--mode", req.Mode)
	}
	if req.MaxIter > 0 {
		argv = append(argv, "--max-iterations", strconv.Itoa(req.MaxIter))
	}
	if req.Provider != "" {
		argv = append(argv, "--provider", req.Provider)
	}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if req.Isolate != "" {
		argv = append(argv, "--isolate", req.Isolate)
	}
	lr, err := a.registry.Launch(workingDir, argv)
	if err != nil {
		a.setToast(style.Error.Render("launch failed: " + err.Error()))
		return
	}
	a.pendingLaunches = append(a.pendingLaunches, lr.ID)
	if req.MaxCost > 0 {
		a.launchBudgets[lr.ID] = req.MaxCost
	}
	// Normalize the stored last-launch with the final workingDir.
	stored := req
	stored.WorkingDir = workingDir
	a.lastLaunch = &stored
	a.setToast(style.Success.Render(fmt.Sprintf("Launched %s (%s)", req.Agent, lr.ID)))
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

// handleRegionKey is the non-modal arrow/tab dispatcher. Returns
// (cmd, true) when the key was a region-level shortcut and the App
// state has been mutated in place; (_, false) tells the caller to
// forward the message to the focused pane untouched.
//
// Bindings:
//   - tab / shift+tab: cycle focus among regions (composer ↔ sidebar ↔
//     focused detail). Visual indicator only — arrow keys are not
//     region-gated, so the user can always cycle runs without leaving
//     composer focus (an explicit user request).
//   - ↑/↓: cycle sidebar runs from any region. Composer history lives
//     on ctrl+↑/ctrl+↓ to keep ↑/↓ available globally.
//   - ctrl+n / ctrl+p: aliases for ↑/↓ sidebar cycle.
//   - ctrl+k: cancel the focused run's subprocess from anywhere.
func (a *App) handleRegionKey(m tea.KeyMsg) (tea.Cmd, bool) {
	switch m.String() {
	case "tab":
		a.focusedRegion = (a.focusedRegion + 1) % regionCount
		return nil, true
	case "shift+tab":
		a.focusedRegion = (a.focusedRegion + regionCount - 1) % regionCount
		return nil, true
	case "up", "ctrl+p":
		a.cycleSelection(-1)
		return nil, true
	case "down", "ctrl+n":
		a.cycleSelection(+1)
		return nil, true
	case "ctrl+k":
		a.cancelFocused()
		return nil, true
	}
	return nil, false
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

// renderSidebar renders the left column. When no runs exist it renders
// a brief onboarding hint instead of an empty `(no runs)` label so the
// column has a visible identity from first launch.
func (a App) renderSidebar(width int) string {
	runs := a.currentRuns()
	if len(runs) == 0 {
		return style.Title.Render("RUNS") + "\n\n" +
			style.Faint.Render("  no runs yet") + "\n" +
			style.Faint.Render("  type /run to launch")
	}
	return sidebar.Render(sidebar.Snapshot{
		Runs:        runs,
		Selected:    a.selected,
		Width:       width,
		MaxPerGroup: 12,
	})
}

func (a App) renderFocused(width int) string {
	run, ok := a.selectedSidebarRun()
	if !ok {
		return a.renderWelcome(width)
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

	// Tailer-backed runs get a prompt summary, metrics block, recent events tail.
	if state, ok := a.tailerStateFor(run.ID); ok {
		if state.Meta.Prompt != "" {
			rows = append(rows,
				style.Secondary.Render("  prompt    "+truncate(state.Meta.Prompt, width-12)),
			)
		}
		if state.Meta.Model != "" {
			rows = append(rows,
				style.Secondary.Render(fmt.Sprintf("  model     %s · %s",
					state.Meta.Provider, state.Meta.Model)),
			)
		}
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

// renderWelcome is the focused-panel content when no run is selected
// (typically because none exist yet). Onboards the user toward /run
// and lists a few useful shortcuts.
func (a App) renderWelcome(_ int) string {
	rows := []string{
		style.Title.Render("WELCOME"),
		"",
		style.Body.Render("  squad — a multi-agent runner"),
		"",
		style.Header.Render("  GET STARTED"),
		"  " + style.Hint.Render("/run") + style.Secondary.Render("                     open the launch form"),
		"  " + style.Hint.Render("/preset load NAME") + style.Secondary.Render("        load a saved config"),
		"  " + style.Hint.Render("/help") + style.Secondary.Render("                    full command list"),
		"",
		style.Header.Render("  KEYS"),
		"  " + style.Hint.Render("tab / shift+tab") + style.Secondary.Render("          cycle focus regions"),
		"  " + style.Hint.Render("↑ / ↓") + style.Secondary.Render("                    cycle runs in sidebar"),
		"  " + style.Hint.Render("ctrl+↑ / ctrl+↓") + style.Secondary.Render("          recall prompt history"),
		"  " + style.Hint.Render("ctrl+k") + style.Secondary.Render("                   cancel focused run"),
		"  " + style.Hint.Render("ctrl+c") + style.Secondary.Render("                   quit"),
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
