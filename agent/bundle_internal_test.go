package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for unexported functions that can't be reached
// through the public API.

func TestRepoSummaryVisitDir(t *testing.T) {
	tests := []struct {
		name    string
		rel     string
		entry   fs.DirEntry
		wantErr error
	}{
		{
			name:    "skip hidden dir",
			rel:     ".git",
			entry:   fakeDirEntry{fname: ".git", dir: true},
			wantErr: filepath.SkipDir,
		},
		{
			name:    "skip node_modules",
			rel:     "node_modules",
			entry:   fakeDirEntry{fname: "node_modules", dir: true},
			wantErr: filepath.SkipDir,
		},
		{
			name:    "skip vendor",
			rel:     "vendor",
			entry:   fakeDirEntry{fname: "vendor", dir: true},
			wantErr: filepath.SkipDir,
		},
		{
			name:    "allow normal dir at depth 0",
			rel:     "pkg",
			entry:   fakeDirEntry{fname: "pkg", dir: true},
			wantErr: nil,
		},
		{
			name:    "skip dir deeper than 3",
			rel:     "a/b/c/d",
			entry:   fakeDirEntry{fname: "d", dir: true},
			wantErr: filepath.SkipDir,
		},
		{
			name:    "allow dir at depth 3",
			rel:     "a/b/c",
			entry:   fakeDirEntry{fname: "c", dir: true},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoSummaryVisitDir(tt.rel, tt.entry)
			if got != tt.wantErr {
				t.Errorf("repoSummaryVisitDir(%q) = %v, want %v",
					tt.rel, got, tt.wantErr)
			}
		})
	}
}

func TestRepoSummaryVisitFile(t *testing.T) {
	tests := []struct {
		name      string
		rel       string
		entry     fs.DirEntry
		wantFiles int
		wantExt   string // expected extension key in dirs, empty if none
		wantCnt   int    // expected count for wantExt
	}{
		{
			name:      "regular go file",
			rel:       "pkg/foo.go",
			entry:     fakeDirEntry{fname: "foo.go", dir: false},
			wantFiles: 1,
			wantExt:   ".go",
			wantCnt:   1,
		},
		{
			name:      "hidden file skipped",
			rel:       ".hidden",
			entry:     fakeDirEntry{fname: ".hidden", dir: false},
			wantFiles: 0,
		},
		{
			name:      "root dir file",
			rel:       "main.go",
			entry:     fakeDirEntry{fname: "main.go", dir: false},
			wantFiles: 1,
			wantExt:   ".go",
			wantCnt:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := make(map[string]*dirInfo)
			totalFiles := 0
			_ = repoSummaryVisitFile(
				tt.rel, tt.entry, dirs, &totalFiles,
			)
			if totalFiles != tt.wantFiles {
				t.Errorf("totalFiles = %d, want %d",
					totalFiles, tt.wantFiles)
			}
			if tt.wantExt != "" {
				dir := filepath.Dir(tt.rel)
				if dir == "." {
					dir = "."
				}
				di := dirs[dir]
				if di == nil {
					t.Fatalf("dirs[%q] is nil", dir)
				}
				if got := di.exts[tt.wantExt]; got != tt.wantCnt {
					t.Errorf("exts[%q] = %d, want %d",
						tt.wantExt, got, tt.wantCnt)
				}
			}
		})
	}
}

func TestTopNExts(t *testing.T) {
	tests := []struct {
		name      string
		exts      map[string]int
		n         int
		wantParts int      // number of space-separated parts
		wantOrder []string // expected parts in order (verified when non-nil)
	}{
		{
			name:      "fewer than n",
			exts:      map[string]int{".go": 5},
			n:         3,
			wantParts: 1,
			wantOrder: []string{".go(5)"},
		},
		{
			name:      "exactly n",
			exts:      map[string]int{".go": 5, ".md": 3, ".yml": 1},
			n:         3,
			wantParts: 3,
			wantOrder: []string{".go(5)", ".md(3)", ".yml(1)"},
		},
		{
			name: "more than n returns top by count",
			exts: map[string]int{
				".go": 5, ".md": 3, ".yml": 1, ".txt": 2,
			},
			n:         2,
			wantParts: 2,
			wantOrder: []string{".go(5)", ".md(3)"},
		},
		{
			name:      "empty map",
			exts:      map[string]int{},
			n:         3,
			wantParts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := topNExts(tt.exts, tt.n)
			if tt.wantParts == 0 {
				if got != "" {
					t.Errorf("topNExts() = %q, want empty", got)
				}
				return
			}
			parts := strings.Split(got, " ")
			if len(parts) != tt.wantParts {
				t.Errorf("topNExts() has %d parts, want %d: %q",
					len(parts), tt.wantParts, got)
			}
			if tt.wantOrder != nil {
				want := strings.Join(tt.wantOrder, " ")
				if got != want {
					t.Errorf("topNExts() = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestChildMaxIterations(t *testing.T) {
	tests := []struct {
		name      string
		budget    *BudgetConfig
		agentName string
		want      int
	}{
		{
			name:      "nil budget",
			budget:    nil,
			agentName: "test",
			want:      0,
		},
		{
			name: "matching child",
			budget: &BudgetConfig{
				Children: []ChildBudget{
					{Name: "child1", MaxIterations: 50},
				},
			},
			agentName: "child1",
			want:      50,
		},
		{
			name: "no matching child",
			budget: &BudgetConfig{
				Children: []ChildBudget{
					{Name: "child1", MaxIterations: 50},
				},
			},
			agentName: "child2",
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.budget.ChildMaxIterations(tt.agentName)
			if got != tt.want {
				t.Errorf("ChildMaxIterations(%q) = %d, want %d",
					tt.agentName, got, tt.want)
			}
		})
	}
}

func TestCompactRepoSummary(t *testing.T) {
	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		got := compactRepoSummary(dir)
		if got != "" {
			t.Errorf("expected empty string for empty dir, got %q",
				got)
		}
	})

	t.Run("directory with files", func(t *testing.T) {
		dir := t.TempDir()
		// Create some test files
		if err := os.WriteFile(
			filepath.Join(dir, "main.go"),
			[]byte("package main"),
			0644,
		); err != nil {
			t.Fatal(err)
		}
		subDir := filepath.Join(dir, "pkg")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(subDir, "lib.go"),
			[]byte("package pkg"),
			0644,
		); err != nil {
			t.Fatal(err)
		}

		got := compactRepoSummary(dir)
		if got == "" {
			t.Error("expected non-empty summary")
		}
		if len(got) < 20 {
			t.Errorf("summary too short: %q", got)
		}
	})
}

func TestResolveInlinePromptDir(t *testing.T) {
	t.Parallel()

	t.Run("stage dir exists with entrypoint", func(t *testing.T) {
		t.Parallel()
		baseDir := t.TempDir()
		stageDir := filepath.Join(baseDir, "stages", "my-stage")
		if err := os.MkdirAll(stageDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stageDir, "system.md"), []byte("stage system"), 0o644); err != nil {
			t.Fatal(err)
		}

		got := resolveInlinePromptDir(baseDir, "my-stage", "system.md")
		if got != stageDir {
			t.Fatalf("expected %q, got %q", stageDir, got)
		}
	})

	t.Run("stage dir missing falls back to baseDir", func(t *testing.T) {
		t.Parallel()
		baseDir := t.TempDir()

		got := resolveInlinePromptDir(baseDir, "no-stage", "system.md")
		if got != baseDir {
			t.Fatalf("expected %q, got %q", baseDir, got)
		}
	})

	t.Run("stage dir exists but no entrypoint falls back", func(t *testing.T) {
		t.Parallel()
		baseDir := t.TempDir()
		stageDir := filepath.Join(baseDir, "stages", "my-stage")
		if err := os.MkdirAll(stageDir, 0o755); err != nil {
			t.Fatal(err)
		}

		got := resolveInlinePromptDir(baseDir, "my-stage", "system.md")
		if got != baseDir {
			t.Fatalf("expected fallback to %q, got %q", baseDir, got)
		}
	})
}

// setupInlineDir creates a temp dir with the given files for inline agent tests.
func setupInlineDir(t *testing.T, files map[string]string) string {
	t.Helper()
	baseDir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(baseDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return baseDir
}

func TestBuildBundleInline_Basic(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{
		"system.md": "inline system prompt",
		"agent.md":  "inline wrapper",
	})

	cfg := &InlineAgentConfig{Name: "test-inline", EntryPoint: "system.md", Wrapper: "agent.md"}
	bundle, err := BuildBundleInline(baseDir, cfg, "review this", "/tmp/work", "edit", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(bundle.System, "inline system prompt") {
		t.Error("expected system prompt content")
	}
	if !strings.Contains(bundle.System, "inline wrapper") {
		t.Error("expected wrapper content")
	}
	if bundle.User != "review this" {
		t.Errorf("User = %q, want 'review this'", bundle.User)
	}
	if bundle.WorkDir != "/tmp/work" {
		t.Errorf("WorkDir = %q, want '/tmp/work'", bundle.WorkDir)
	}
}

func TestBuildBundleInline_EmptyPrompt(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{"system.md": "sys", "agent.md": "wrap"})

	cfg := &InlineAgentConfig{Name: "test-inline", EntryPoint: "system.md", Wrapper: "agent.md"}
	bundle, err := BuildBundleInline(baseDir, cfg, "", "/tmp", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.User != "Begin." {
		t.Errorf("User = %q, want 'Begin.'", bundle.User)
	}
}

func TestBuildBundleInline_WithModels(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{"system.md": "sys", "agent.md": "wrap"})

	cfg := &InlineAgentConfig{
		Name: "test-inline", EntryPoint: "system.md", Wrapper: "agent.md",
		Models: []ModelPreference{{Model: "gpt-4", Provider: "openai"}},
	}
	bundle, err := BuildBundleInline(baseDir, cfg, "go", "/tmp", "edit", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", bundle.Model)
	}
	if bundle.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", bundle.Provider)
	}
}

func TestBuildBundleInline_WithReferences(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{
		"system.md": "sys", "agent.md": "wrap", "ref.md": "reference content",
	})

	cfg := &InlineAgentConfig{
		Name: "test-inline", EntryPoint: "system.md", Wrapper: "agent.md",
		References: []string{"ref.md"},
	}
	bundle, err := BuildBundleInline(baseDir, cfg, "go", "/tmp", "edit", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(bundle.System, "reference content") {
		t.Error("expected reference content in system prompt")
	}
}

func TestBuildBundleInline_StageSpecificPrompts(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{
		"stages/my-stage/system.md": "stage-specific system",
		"stages/my-stage/agent.md":  "stage-specific wrapper",
	})

	cfg := &InlineAgentConfig{Name: "my-stage", EntryPoint: "system.md", Wrapper: "agent.md"}
	bundle, err := BuildBundleInline(baseDir, cfg, "test", "/tmp", "edit", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(bundle.System, "stage-specific system") {
		t.Error("expected stage-specific system content")
	}
}

func TestBuildBundleInline_WithVars(t *testing.T) {
	t.Parallel()
	baseDir := setupInlineDir(t, map[string]string{
		"system.md": "target: {{.Vars.TARGET}}", "agent.md": "wrap",
	})

	cfg := &InlineAgentConfig{Name: "test-inline", EntryPoint: "system.md", Wrapper: "agent.md"}
	bundle, err := BuildBundleInline(baseDir, cfg, "go", "/tmp", "edit", map[string]string{"TARGET": "85"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(bundle.System, "target: 85") {
		t.Error("expected vars to be expanded in system prompt")
	}
}

func TestBuildBundleInline_MissingEntrypoint(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	cfg := &InlineAgentConfig{Name: "test-inline", EntryPoint: "missing.md", Wrapper: "agent.md"}
	_, err := BuildBundleInline(baseDir, cfg, "go", "/tmp", "edit", nil)
	if err == nil {
		t.Fatal("expected error for missing entrypoint")
	}
}

// fakeDirEntry implements fs.DirEntry for testing.
type fakeDirEntry struct {
	fname string
	dir   bool
}

func (f fakeDirEntry) Name() string { return f.fname }
func (f fakeDirEntry) IsDir() bool  { return f.dir }
func (f fakeDirEntry) Type() fs.FileMode {
	if f.dir {
		return fs.ModeDir
	}
	return 0
}
func (f fakeDirEntry) Info() (fs.FileInfo, error) {
	return fakeFileInfo(f), nil
}

type fakeFileInfo struct {
	fname string
	dir   bool
}

func (f fakeFileInfo) Name() string       { return f.fname }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() fs.FileMode  { return 0644 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.dir }
func (f fakeFileInfo) Sys() interface{}   { return nil }
