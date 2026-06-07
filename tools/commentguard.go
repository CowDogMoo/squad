package tools

import (
	"context"
	"fmt"
	"strings"
)

type commentsOnlyKeyType struct{}

// InitCommentsOnlyMode marks ctx as comments-only. While set, Edit and
// MultiEdit calls reject any change that modifies non-comment lines.
func InitCommentsOnlyMode(ctx context.Context) context.Context {
	return context.WithValue(ctx, commentsOnlyKeyType{}, true)
}

// IsCommentsOnlyMode reports whether ctx is in comments-only mode.
func IsCommentsOnlyMode(ctx context.Context) bool {
	v, ok := ctx.Value(commentsOnlyKeyType{}).(bool)
	return ok && v
}

// ValidateCommentsOnly returns nil when the diff between oldText and newText
// touches only comment lines or blank lines, and an error otherwise. The
// check is line-based and language-agnostic for common comment markers
// (`//`, `#`, `--`, `/*`, `*/`, leading `*` inside a block comment).
//
// "Comment line" means the trimmed line is empty or starts with one of
// those markers. The check enforces that the multiset of non-comment,
// non-blank lines is identical in old and new — any added or removed
// code line is a rejection.
func ValidateCommentsOnly(oldText, newText string) error {
	added, removed := codeLineDiff(oldText, newText)
	if len(added) == 0 && len(removed) == 0 {
		return nil
	}
	var sample string
	switch {
	case len(added) > 0:
		sample = added[0]
	default:
		sample = removed[0]
	}
	return fmt.Errorf(
		"comments-only edit rejected: %d non-comment line(s) added, %d removed (e.g. %q). "+
			"This agent may only delete or trim comments. Edits that add/remove/modify code are forbidden",
		len(added), len(removed), TruncateString(sample, 80))
}

// codeLineDiff returns the multiset difference of non-comment, non-blank
// lines: lines present in newText but not (or in excess of) oldText are
// "added"; the reverse are "removed".
func codeLineDiff(oldText, newText string) (added, removed []string) {
	oldCounts := countCodeLines(oldText)
	newCounts := countCodeLines(newText)
	for line, n := range newCounts {
		excess := n - oldCounts[line]
		for i := 0; i < excess; i++ {
			added = append(added, line)
		}
	}
	for line, n := range oldCounts {
		excess := n - newCounts[line]
		for i := 0; i < excess; i++ {
			removed = append(removed, line)
		}
	}
	return added, removed
}

// countCodeLines returns a multiset of non-comment, non-blank lines from
// text, keyed by the trimmed line content with any trailing `// ...`
// comment removed.
func countCodeLines(text string) map[string]int {
	out := make(map[string]int)
	for _, raw := range strings.Split(text, "\n") {
		line := stripTrailingLineComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isCommentMarker(trimmed) {
			continue
		}
		out[trimmed]++
	}
	return out
}

// isCommentMarker reports whether a trimmed line begins with a known
// single- or multi-line comment marker.
func isCommentMarker(trimmed string) bool {
	switch {
	case strings.HasPrefix(trimmed, "//"):
		return true
	case strings.HasPrefix(trimmed, "#"):
		return true
	case strings.HasPrefix(trimmed, "--"):
		return true
	case strings.HasPrefix(trimmed, "/*"), strings.HasPrefix(trimmed, "*/"):
		return true
	case strings.HasPrefix(trimmed, "*"):
		return true
	}
	return false
}

// stripTrailingLineComment removes a trailing `// ...` on a code line. It
// does not parse strings, so a `//` inside a string literal would also be
// stripped — acceptable for this validator because the goal is to compare
// code structure, not to render code.
func stripTrailingLineComment(line string) string {
	if i := strings.Index(line, "//"); i >= 0 {
		return strings.TrimRight(line[:i], " \t")
	}
	return line
}
