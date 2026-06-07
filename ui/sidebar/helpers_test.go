package sidebar

import (
	"testing"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{"under width", "hello", 10, "hello"},
		{"equal width", "hello", 5, "hello"},
		{"over width", "hello world", 6, "hello…"},
		{"width zero", "hello", 0, ""},
		{"width negative", "hello", -1, ""},
		{"width one is ellipsis", "hello", 1, "…"},
		{"width greater than length", "ab", 100, "ab"},
		{"empty", "", 5, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.s, tc.w); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
			}
		})
	}
}

func TestGlyphForAliveBudget(t *testing.T) {
	g, _ := glyphFor(StateBudget, true)
	if g != "◇" {
		t.Errorf("alive+budget glyph: got %q, want ◇", g)
	}
}

func TestGlyphForAliveDefault(t *testing.T) {
	// StateCompleted while alive is the "fallthrough" branch.
	g, _ := glyphFor(StateCompleted, true)
	if g != "▶" {
		t.Errorf("alive default glyph: got %q, want ▶", g)
	}
}

func TestGlyphForDeadBudget(t *testing.T) {
	g, _ := glyphFor(StateBudget, false)
	if g != "∙" {
		t.Errorf("dead+budget glyph: got %q, want ∙", g)
	}
}

func TestGlyphForDeadDefault(t *testing.T) {
	// StateConnecting while not alive — falls through to faint dot.
	g, _ := glyphFor(StateConnecting, false)
	if g != "∙" {
		t.Errorf("dead default glyph: got %q, want ∙", g)
	}
}

func TestFormatElapsedClampsNegative(t *testing.T) {
	if got := formatElapsed(-5); got != "0s" {
		t.Errorf("negative duration should clamp to 0s, got %q", got)
	}
}
