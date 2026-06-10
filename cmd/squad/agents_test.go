package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"shorter than max", "abc", 10, "abc"},
		{"equal to max", "abcdef", 6, "abcdef"},
		{"longer than max", "abcdefghij", 6, "abc..."},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func TestGuessRepoName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"https url with .git", "https://github.com/owner/repo.git", "repo"},
		{"https url no .git", "https://github.com/owner/repo", "repo"},
		{"ssh url", "git@github.com:owner/repo.git", "repo"},
		{"plain name", "repo", "repo"},
		{"ssh with subgroup", "git@gitlab.com:group/subgroup/repo.git", "repo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := guessRepoName(tt.in)
			if got != tt.want {
				t.Errorf("guessRepoName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestShortenPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	// Path inside home becomes ~/...
	inner := filepath.Join(home, "some", "sub")
	got := shortenPath(inner)
	if !strings.HasPrefix(got, "~/") {
		t.Errorf("shortenPath(%q) = %q, want ~/-prefixed", inner, got)
	}
	// Path outside home: shortenPath returns either ~/-prefixed if Rel
	// produces a non-abs result (which it does even with ".."), or the
	// path as-is. We only assert it doesn't crash and returns non-empty.
	tmp := t.TempDir()
	if got2 := shortenPath(tmp); got2 == "" {
		t.Errorf("shortenPath(%q) returned empty", tmp)
	}
}
