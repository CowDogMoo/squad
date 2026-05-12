package style

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Panel renders `body` inside a rounded border with `title` embedded in
// the top edge. Lifted from DreadGOAD's scoreboard panelWithTitle —
// gives the squad TUI a unified chrome that matches the brand identity.
//
// Layout:
//
//	╭── TITLE ──────────────────────╮
//	│ body                          │
//	│ ...                           │
//	╰───────────────────────────────╯
//
// Width is the total external width (including borders). Body lines are
// padded right with spaces to width-4 (2 cols border, 2 cols padding).
// Lines wider than the inner width are truncated with an ellipsis.
func Panel(title, body string, width int) string {
	innerWidth := width - 4 // border (2) + padding (2)
	if innerWidth < 1 {
		innerWidth = 1
	}

	top := renderPanelTop(title, innerWidth)
	bottom := Border.Render("╰" + strings.Repeat("─", innerWidth+2) + "╯")

	rows := []string{top}
	for _, line := range strings.Split(body, "\n") {
		visW := lipgloss.Width(line)
		pad := innerWidth - visW
		if pad < 0 {
			line = truncateVisible(line, innerWidth)
			pad = 0
		}
		rows = append(rows,
			Border.Render("│")+" "+line+strings.Repeat(" ", pad)+" "+Border.Render("│"),
		)
	}
	rows = append(rows, bottom)
	return strings.Join(rows, "\n")
}

// PanelHeight returns the number of rendered rows a Panel(...) call
// will produce for the given body. Useful when the host needs to
// allocate vertical space deterministically.
func PanelHeight(body string) int {
	// 2 for borders + body line count.
	return 2 + len(strings.Split(body, "\n"))
}

func renderPanelTop(title string, innerWidth int) string {
	if title == "" {
		return Border.Render("╭" + strings.Repeat("─", innerWidth+2) + "╮")
	}
	titleText := " " + title + " "
	titleVis := lipgloss.Width(titleText)
	leadDashes := 2
	trailDashes := innerWidth + 2 - leadDashes - titleVis
	if trailDashes < 1 {
		trailDashes = 1
	}
	return Border.Render("╭"+strings.Repeat("─", leadDashes)) +
		Title.Render(titleText) +
		Border.Render(strings.Repeat("─", trailDashes)+"╮")
}

// truncateVisible cuts a styled string at width visible columns,
// appending an ellipsis. Best-effort — assumes ASCII payload after
// ANSI sequences (true for squad's content).
func truncateVisible(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	// Walk the string, counting visible runes until we hit width-1.
	// Skip ANSI escape sequences entirely.
	var b strings.Builder
	visible := 0
	target := width - 1
	in := []rune(s)
	for i := 0; i < len(in); i++ {
		r := in[i]
		if r == 0x1b && i+1 < len(in) && in[i+1] == '[' {
			// CSI — copy through the final letter inclusive.
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
			// Append any trailing reset sequence so the line stops
			// painting in the truncated style.
			break
		}
		b.WriteRune(r)
		visible++
	}
	return b.String()
}
