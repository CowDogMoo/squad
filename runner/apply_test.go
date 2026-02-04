package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/tools"
)

func TestExtractUnifiedDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		contains []string
	}{
		{
			"single diff block",
			"before\n```diff\ndiff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n```\nafter",
			false,
			[]string{"diff --git a/a.txt b/a.txt"},
		},
		{
			"multiple diff blocks",
			strings.Join([]string{
				"before",
				"```diff",
				"diff --git a/a.txt b/a.txt",
				"--- a/a.txt",
				"+++ b/a.txt",
				"@@ -1 +1 @@",
				"-old",
				"+new",
				"```",
				"```patch",
				"diff --git a/b.txt b/b.txt",
				"--- a/b.txt",
				"+++ b/b.txt",
				"@@ -1 +1 @@",
				"-old",
				"+new",
				"```",
			}, "\n"),
			false,
			[]string{"diff --git a/a.txt", "diff --git a/b.txt"},
		},
		{
			"patch fence",
			"```patch\ndiff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a\n+b\n```",
			false,
			[]string{"diff --git a/x b/x"},
		},
		{
			"no fence",
			"no diff here",
			true,
			nil,
		},
		{
			"invalid diff content",
			"```diff\nnot a diff\n```",
			true,
			nil,
		},
		{
			"unclosed fence with valid diff",
			"```diff\ndiff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-x\n+y",
			false,
			[]string{"diff --git a/f b/f"},
		},
		{
			"empty body",
			"```diff\n```",
			true,
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diff, err := extractUnifiedDiff(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractUnifiedDiff() error = %v, wantErr %v", err, tt.wantErr)
			}
			for _, s := range tt.contains {
				if !strings.Contains(diff, s) {
					t.Fatalf("expected diff to contain %q, got %q", s, diff)
				}
			}
		})
	}
}

func TestLooksLikeDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"diff --git header", "diff --git a/a b/a", true},
		{"patch headers", "--- a/a\n+++ b/a", true},
		{"plain text", "no headers", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := looksLikeDiff(tt.input); got != tt.want {
				t.Fatalf("looksLikeDiff(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateActionableResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		edits   bool
		wantErr bool
	}{
		{
			"has diff block",
			"```diff\ndiff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-x\n+y\n```",
			false,
			false,
		},
		{"has files touched", "files touched: a.txt", false, false},
		{"has no changes", "No changes detected", false, false},
		{"bare text", "no diff or markers", false, true},
		{"edits applied bypasses check", "bare text", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tools.InitEdits(context.Background())
			if tt.edits {
				tools.MarkEditsApplied(ctx)
			}
			err := validateActionableResponse(ctx, tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateActionableResponse() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyResponseDiffNoChanges(t *testing.T) {
	t.Parallel()
	ctx := tools.InitEdits(context.Background())
	if err := applyResponseDiff(ctx, "No changes", ".", false); err != nil {
		t.Fatalf("applyResponseDiff() error = %v", err)
	}
}

func TestApplyResponseDiffBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		response string
		edits    bool
		wantErr  bool
	}{
		{
			name:     "missing diff returns error",
			response: "plain text response",
			edits:    false,
			wantErr:  true,
		},
		{
			name:     "edits applied skip",
			response: "plain text response",
			edits:    true,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tools.InitEdits(context.Background())
			if tt.edits {
				tools.MarkEditsApplied(ctx)
			}
			err := applyResponseDiff(ctx, tt.response, ".", false)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyResponseDiff() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyUnifiedDiffEmpty(t *testing.T) {
	t.Parallel()
	if err := applyUnifiedDiff(context.Background(), ".", "", false); err == nil {
		t.Fatalf("expected error for empty diff")
	}
}

func TestResponseIndicatesNoChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"has no changes", "No changes needed", true},
		{"lowercase", "no changes found", true},
		{"no match", "all done", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := responseIndicatesNoChanges(tt.input); got != tt.want {
				t.Fatalf("responseIndicatesNoChanges(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestApplyUnifiedDiffScenarios(t *testing.T) {
	tests := []struct {
		name          string
		gitScript     string
		patchScript   string
		applyFallback bool
		wantErr       bool
		wantContains  string
	}{
		{
			name:          "git success",
			gitScript:     "#!/bin/sh\ncat >/dev/null\nexit 0\n",
			patchScript:   "#!/bin/sh\nexit 1\n",
			applyFallback: false,
			wantErr:       false,
		},
		{
			name:          "git fails without fallback",
			gitScript:     "#!/bin/sh\necho git-fail 1>&2\nexit 1\n",
			patchScript:   "#!/bin/sh\nexit 1\n",
			applyFallback: false,
			wantErr:       true,
			wantContains:  "failed to apply diff with git",
		},
		{
			name:          "fallback to patch",
			gitScript:     "#!/bin/sh\necho git-fail 1>&2\nexit 1\n",
			patchScript:   "#!/bin/sh\ncat >/dev/null\nexit 0\n",
			applyFallback: true,
			wantErr:       false,
		},
		{
			name:          "git and patch fail",
			gitScript:     "#!/bin/sh\necho git-fail 1>&2\nexit 1\n",
			patchScript:   "#!/bin/sh\necho patch-fail 1>&2\nexit 1\n",
			applyFallback: true,
			wantErr:       true,
			wantContains:  "failed to apply diff with git or patch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			writeFakeTool(t, binDir, "git", tt.gitScript)
			writeFakeTool(t, binDir, "patch", tt.patchScript)
			pathEnv := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
			t.Setenv("PATH", pathEnv)

			diff := strings.Join([]string{
				"diff --git a/a.txt b/a.txt",
				"--- a/a.txt",
				"+++ b/a.txt",
				"@@ -1 +1 @@",
				"-old",
				"+new",
				"",
			}, "\n")
			err := applyUnifiedDiff(
				context.Background(), t.TempDir(), diff, tt.applyFallback,
			)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyUnifiedDiff() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestApplyResponseDiffAppliesUnifiedDiff(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTool(t, binDir, "git", "#!/bin/sh\ncat >/dev/null\nexit 0\n")
	writeFakeTool(t, binDir, "patch", "#!/bin/sh\nexit 1\n")
	pathEnv := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathEnv)

	response := strings.Join([]string{
		"```diff",
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"```",
	}, "\n")

	ctx := tools.InitEdits(context.Background())
	if err := applyResponseDiff(ctx, response, t.TempDir(), false); err != nil {
		t.Fatalf("applyResponseDiff() error = %v", err)
	}
}

func writeFakeTool(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", name, err)
	}
}
