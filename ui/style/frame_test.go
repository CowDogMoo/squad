package style

import (
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestPanelTopAndBottom(t *testing.T) {
	out := stripANSI(Panel("HEADER", "line one", 30))
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (top, body, bottom), got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") {
		t.Errorf("top row malformed: %q", lines[0])
	}
	if !strings.Contains(lines[0], "HEADER") {
		t.Errorf("top row missing title: %q", lines[0])
	}
	if !strings.HasPrefix(lines[2], "╰") || !strings.HasSuffix(lines[2], "╯") {
		t.Errorf("bottom row malformed: %q", lines[2])
	}
}

func TestPanelBodyPadsToWidth(t *testing.T) {
	out := stripANSI(Panel("T", "hi", 20))
	body := strings.Split(out, "\n")[1]
	// "│ hi" + pad + " │" — total visible width = 20.
	if len([]rune(body)) != 20 {
		t.Errorf("body row width: got %d, want 20 (line=%q)", len([]rune(body)), body)
	}
}

func TestPanelMultilineBody(t *testing.T) {
	body := "row 1\nrow 2\nrow 3"
	out := stripANSI(Panel("MULTI", body, 30))
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("want top + 3 body + bottom = 5, got %d", len(lines))
	}
}

func TestPanelHeight(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		{"", 3},        // empty body still renders one body line
		{"a", 3},       // top + 1 + bottom
		{"a\nb\nc", 5}, // top + 3 + bottom
	}
	for _, tc := range cases {
		if got := PanelHeight(tc.body); got != tc.want {
			t.Errorf("PanelHeight(%q) = %d, want %d", tc.body, got, tc.want)
		}
	}
}

func TestPanelTruncatesOversizedLines(t *testing.T) {
	body := strings.Repeat("x", 100)
	out := stripANSI(Panel("T", body, 20))
	body2 := strings.Split(out, "\n")[1]
	if !strings.Contains(body2, "…") {
		t.Errorf("oversized line should be truncated with ellipsis: %q", body2)
	}
	if len([]rune(body2)) != 20 {
		t.Errorf("truncated row width: got %d, want 20", len([]rune(body2)))
	}
}

func TestPanelNoTitle(t *testing.T) {
	out := stripANSI(Panel("", "body", 20))
	top := strings.Split(out, "\n")[0]
	if strings.Contains(top, " ") && !strings.HasPrefix(top, "╭") {
		t.Errorf("titleless top row should be all border: %q", top)
	}
}

func TestPanelTinyWidthClamps(t *testing.T) {
	// width=3 → innerWidth would be -1; Panel clamps to 1.
	out := stripANSI(Panel("T", "x", 3))
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╮") {
		t.Errorf("tiny width should still render chrome: %q", out)
	}
}

func TestPanelLongTitleClampsTrailingDashes(t *testing.T) {
	// Title longer than inner width forces trailDashes to clamp at 1.
	out := stripANSI(Panel("VERY-LONG-TITLE", "body", 12))
	top := strings.Split(out, "\n")[0]
	if !strings.HasSuffix(top, "╮") {
		t.Errorf("top still ends with corner even when title overflows: %q", top)
	}
}

func TestPanelFixedPadsBlankLines(t *testing.T) {
	out := stripANSI(PanelFixed("T", "a", 30, 6))
	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Errorf("PanelFixed should produce exactly 6 lines, got %d", len(lines))
	}
}

func TestPanelFixedLeavesLargeBodyAlone(t *testing.T) {
	body := "a\nb\nc\nd"
	out := stripANSI(PanelFixed("T", body, 30, 3))
	lines := strings.Split(out, "\n")
	// Body has 4 lines, height=3 → needed=1, so no padding added.
	if len(lines) < 6 {
		t.Errorf("PanelFixed should not truncate body below natural size, got %d lines", len(lines))
	}
}

func TestTruncateVisibleStripsANSI(t *testing.T) {
	// Crafted styled string. 5 visible chars, with a CSI sequence in the middle.
	styled := "\x1b[31mHELLO\x1b[0m world"
	out := truncateVisible(styled, 6)
	// stripped output should contain ellipsis and at most 6 visible chars.
	plain := stripANSI(out)
	if !strings.Contains(plain, "…") {
		t.Errorf("expected ellipsis in truncated output: %q (plain=%q)", out, plain)
	}
	if w := len([]rune(plain)); w > 6 {
		t.Errorf("truncateVisible exceeded width: visible=%d, want ≤6 (%q)", w, plain)
	}
}

func TestTruncateVisibleZeroAndOneWidth(t *testing.T) {
	if got := truncateVisible("hello", 1); got != "…" {
		t.Errorf("width=1 should yield bare ellipsis, got %q", got)
	}
	if got := truncateVisible("hello", 0); got != "…" {
		t.Errorf("width=0 should yield bare ellipsis, got %q", got)
	}
}

func TestTruncateVisibleNoChange(t *testing.T) {
	if got := truncateVisible("abc", 10); got != "abc" {
		t.Errorf("under-width should pass through, got %q", got)
	}
}
