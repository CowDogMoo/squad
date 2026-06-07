// Package style defines the squad TUI's color palette and pre-built lipgloss
// styles. The palette is intentionally narrow (cyan/green/red/magenta + a
// single brand orange for chrome) following codex's styling discipline:
// fewer colors read more cleanly, and consistent semantic meaning makes the
// interface self-documenting.
//
// Semantic mapping:
//
//	Brand   — app chrome only (panel borders, titles)
//	Cyan    — selection, hints, the "working" / "focused" affordance
//	Green   — success, completed runs
//	Red     — errors, failed runs
//	Magenta — agent identity
//
// No blue, no yellow. "Warning" states use dim red; "running" uses cyan plus
// motion (see ui/status).
package style

import "github.com/charmbracelet/lipgloss"

// Color values. Hex strings are kept here as the single source of truth.
const (
	ColorBrand   = "#ca5e44" // Dreadnode chrome — borders, titles
	ColorCyan    = "#5fd7d7" // selection, hints, working/focused
	ColorGreen   = "#68c147" // success
	ColorRed     = "#e44f4f" // error
	ColorMagenta = "#d18ad1" // agent identity

	ColorFG      = "#e2e7ec" // body text
	ColorFGMuted = "#9da0a5" // secondary text
	ColorFGFaint = "#686d73" // hints, separators
)

// Pre-built styles. Constructed at package init so callers don't allocate per
// render. lipgloss styles are value types — calling .Bold(true), .Foreground(),
// etc. returns a new style without mutating the original.
var (
	// Chrome.
	Title  = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand)).Bold(true)
	Border = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrand))

	// Typography hierarchy.
	Header    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorFG)).Bold(true)
	Body      = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorFG))
	Secondary = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorFGMuted))
	Faint     = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorFGFaint))

	// Semantic.
	Hint    = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan))
	Working = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorCyan)).Bold(true)
	Success = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Bold(true)
	Error   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true)
	Agent   = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMagenta))
)
