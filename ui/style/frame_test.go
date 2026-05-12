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
