package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type asciiOnlyKeyType struct{}

// InitASCIIOnlyMode marks ctx as ASCII-only. While set, Edit and MultiEdit
// reject any change whose replacement text introduces non-ASCII characters
// that were not already present in the text being replaced.
func InitASCIIOnlyMode(ctx context.Context) context.Context {
	return context.WithValue(ctx, asciiOnlyKeyType{}, true)
}

// IsASCIIOnlyMode reports whether ctx is in ASCII-only mode.
func IsASCIIOnlyMode(ctx context.Context) bool {
	v, ok := ctx.Value(asciiOnlyKeyType{}).(bool)
	return ok && v
}

// ValidateASCIIOnly returns nil unless newText introduces non-ASCII runes that
// oldText did not contain (counted per rune, so an edit may keep or remove
// existing non-ASCII but never add more). This is a hard backstop for agents
// whose job is to REMOVE typographic tells — smart quotes (’ “ ”), em/en
// dashes (— –), non-breaking hyphens (‑), the ellipsis char (…) — so a model
// that "cleans" prose while sneaking in the very characters it should strip is
// rejected at the tool boundary rather than merely discouraged by prompt.
func ValidateASCIIOnly(oldText, newText string) error {
	oldCounts := nonASCIICounts(oldText)
	introduced := map[rune]int{}
	for r, n := range nonASCIICounts(newText) {
		if excess := n - oldCounts[r]; excess > 0 {
			introduced[r] = excess
		}
	}
	if len(introduced) == 0 {
		return nil
	}
	runes := make([]rune, 0, len(introduced))
	for r := range introduced {
		runes = append(runes, r)
	}
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	var parts []string
	for _, r := range runes {
		parts = append(parts, fmt.Sprintf("%q (U+%04X)×%d", string(r), r, introduced[r]))
	}
	return fmt.Errorf(
		"ascii-only edit rejected: replacement introduces non-ASCII character(s) not in the original: %s. "+
			"Use plain ASCII instead (straight quotes ' \", hyphen -, three dots ...). "+
			"Introducing typographic characters is itself an LLM tell",
		strings.Join(parts, ", "))
}

// editGuardError returns the first active edit-guard violation (comments-only,
// then ascii-only) for a replacement, or nil if all active guards pass. It
// centralizes the guard checks shared by the Edit and MultiEdit tools.
func editGuardError(ctx context.Context, oldText, newText string) error {
	if IsCommentsOnlyMode(ctx) {
		if err := ValidateCommentsOnly(oldText, newText); err != nil {
			return err
		}
	}
	if IsASCIIOnlyMode(ctx) {
		if err := ValidateASCIIOnly(oldText, newText); err != nil {
			return err
		}
	}
	return nil
}

// nonASCIICounts returns a multiset of the non-ASCII runes in text.
func nonASCIICounts(text string) map[rune]int {
	out := map[rune]int{}
	for _, r := range text {
		if r > 127 {
			out[r]++
		}
	}
	return out
}
