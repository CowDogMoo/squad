// Package status provides the animated run-status indicator that anchors the
// squad TUI footer. It owns a single line: a shimmering glyph encoding the
// run state, a bold label, optional detail text, an elapsed timer, and a
// trailing "esc to interrupt" affordance shown only while a run is alive.
//
// The renderer is pure (Render takes a frame counter and elapsed duration,
// not the wall clock) so callers can golden-test the output without time
// flakiness. The bubble-tea Model wraps Render with a 32ms ticker.
package status

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cowdogmoo/squad/ui/style"
)

// TickInterval is the animation cadence (~31 FPS). Lifted from codex's
// status indicator — fast enough that the shimmer reads as motion, slow
// enough that a sleeping terminal doesn't burn CPU.
const TickInterval = 32 * time.Millisecond

// State is what the indicator is currently communicating.
type State int

const (
	StateIdle State = iota
	StateConnecting
	StateWorking
	StateCompleted
	StateError
	StatePaused // budget exceeded, awaiting input, etc.
)

// shimmerGlyphs cycle while StateWorking. Six frames at 32ms ≈ 5 cycles/sec.
var shimmerGlyphs = []string{"✻", "✦", "✸", "✶", "✦", "✺"}

// spinnerGlyphs cycle while StateConnecting. Four frames.
var spinnerGlyphs = []string{"◐", "◓", "◑", "◒"}

// Snapshot is the immutable input to Render. Keeping the render function
// pure (no time.Now, no internal state) makes it trivially testable.
type Snapshot struct {
	State     State
	Label     string        // e.g. "Working", "Completed"
	Detail    string        // e.g. "31/40 iter · 142 tools · $1.84/$5.00"
	Elapsed   time.Duration // since the run started
	Frame     uint64        // animation tick counter
	Width     int           // terminal width for right-aligning the timer
	Interrupt bool          // show "esc to interrupt" affordance
}

// Render returns a single styled line. ANSI escapes are emitted unless the
// caller has set lipgloss.SetColorProfile(termenv.Ascii) — which test code
// should do to produce stable goldens.
func Render(s Snapshot) string {
	glyph := renderGlyph(s.State, s.Frame)
	label := renderLabel(s.State, s.Label)

	left := glyph + " " + label
	if s.Detail != "" {
		left += style.Secondary.Render(" · " + s.Detail)
	}

	// Right side: elapsed + optional interrupt hint.
	var rightParts []string
	if s.Elapsed > 0 || s.State == StateWorking || s.State == StateConnecting {
		rightParts = append(rightParts, style.Faint.Render(formatElapsed(s.Elapsed)))
	}
	if s.Interrupt && (s.State == StateWorking || s.State == StateConnecting) {
		rightParts = append(rightParts, style.Faint.Render("esc to interrupt"))
	}
	right := strings.Join(rightParts, style.Faint.Render(" · "))

	if right == "" {
		return left
	}
	if s.Width <= 0 {
		return left + "  " + right
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	pad := s.Width - leftW - rightW
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right
}

func renderGlyph(state State, frame uint64) string {
	switch state {
	case StateWorking:
		return style.Working.Render(shimmerGlyphs[int(frame)%len(shimmerGlyphs)])
	case StateConnecting:
		return style.Hint.Render(spinnerGlyphs[int(frame)%len(spinnerGlyphs)])
	case StateCompleted:
		return style.Success.Render("✓")
	case StateError:
		return style.Error.Render("✗")
	case StatePaused:
		return style.Error.Bold(false).Render("◇")
	case StateIdle:
		fallthrough
	default:
		return style.Faint.Render("·")
	}
}

func renderLabel(state State, label string) string {
	if label == "" {
		label = defaultLabel(state)
	}
	switch state {
	case StateWorking, StateConnecting:
		return style.Working.Render(label)
	case StateCompleted:
		return style.Success.Render(label)
	case StateError:
		return style.Error.Render(label)
	case StatePaused:
		return style.Body.Bold(true).Render(label)
	default:
		return style.Secondary.Render(label)
	}
}

func defaultLabel(state State) string {
	switch state {
	case StateWorking:
		return "Working"
	case StateConnecting:
		return "Connecting"
	case StateCompleted:
		return "Completed"
	case StateError:
		return "Error"
	case StatePaused:
		return "Paused"
	default:
		return "Idle"
	}
}

// formatElapsed renders a duration as `8s`, `1m 24s`, or `1h 12m 04s`.
// Negative durations render as `0s`.
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
		return fmt.Sprintf("%dh %02dm %02ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// Model is a Bubble Tea wrapper around Render. It owns the animation tick
// and the start timestamp; callers update state/label/detail via setters.
type Model struct {
	snap    Snapshot
	started time.Time
}

// New returns a Model in StateIdle.
func New() Model {
	return Model{}
}

// Init starts the animation ticker.
func (m Model) Init() tea.Cmd {
	return tick()
}

// TickMsg is delivered every TickInterval. Exported so callers that
// multiplex bubble tea programs can route ticks correctly.
type TickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(TickInterval, func(t time.Time) tea.Msg { return TickMsg(t) })
}

// Update advances the animation frame and refreshes elapsed time. It
// schedules the next tick.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(TickMsg); ok {
		m.snap.Frame++
		if !m.started.IsZero() {
			m.snap.Elapsed = time.Since(m.started)
		}
		return m, tick()
	}
	return m, nil
}

// View renders the current snapshot. Width comes from the host model.
func (m Model) View() string { return Render(m.snap) }

// SetState transitions the indicator. Starting from StateIdle or StateError
// into StateWorking resets the elapsed timer; transitioning to a terminal
// state (Completed/Error/Paused) freezes it.
func (m *Model) SetState(s State) {
	prev := m.snap.State
	m.snap.State = s
	switch {
	case s == StateWorking && prev != StateWorking:
		m.started = time.Now()
	case s == StateCompleted || s == StateError || s == StatePaused:
		if !m.started.IsZero() {
			m.snap.Elapsed = time.Since(m.started)
		}
		m.started = time.Time{}
	}
}

// SetLabel overrides the default label for the current state.
func (m *Model) SetLabel(label string) { m.snap.Label = label }

// SetDetail sets the secondary text shown after the label.
func (m *Model) SetDetail(detail string) { m.snap.Detail = detail }

// SetWidth sets the render width for right-aligning the timer.
func (m *Model) SetWidth(w int) { m.snap.Width = w }

// SetInterrupt toggles the "esc to interrupt" hint.
func (m *Model) SetInterrupt(b bool) { m.snap.Interrupt = b }

// Snapshot returns the current immutable render input.
func (m Model) Snapshot() Snapshot { return m.snap }
