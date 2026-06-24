package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/cowdogmoo/squad/metrics"
)

func createTestFiles(t *testing.T, dir string, count int, ext string) {
	t.Helper()
	for i := 0; i < count; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file%d%s", i, ext))
		if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}
}

func TestExpandPartition(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createTestFiles(t, dir, 50, ".go")

	partitions, err := ExpandPartition(dir, &Partition{
		By:              "files",
		Glob:            "**/*.go",
		MaxPerPartition: 20,
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}

	// 50 files / 20 per partition = 3 partitions
	if len(partitions) != 3 {
		t.Fatalf("expected 3 partitions, got %d", len(partitions))
	}
	if len(partitions[0]) != 20 {
		t.Fatalf("first partition should have 20 files, got %d", len(partitions[0]))
	}
	if len(partitions[2]) != 10 {
		t.Fatalf("last partition should have 10 files, got %d", len(partitions[2]))
	}
}

func TestExpandPartitionEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// No files matching the glob
	createTestFiles(t, dir, 5, ".py")

	partitions, err := ExpandPartition(dir, &Partition{
		By:   "files",
		Glob: "**/*.go",
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}
	if partitions != nil {
		t.Fatalf("expected nil for no matches, got %d partitions", len(partitions))
	}
}

func TestExpandPartitionInvalidGlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// An unterminated character class is rejected by doublestar.ValidatePattern.
	_, err := ExpandPartition(dir, &Partition{
		By:   "files",
		Glob: "[invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid glob, got nil")
	}
	if !strings.Contains(err.Error(), "invalid partition glob") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExpandPartitionDefaultSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createTestFiles(t, dir, 30, ".go")

	partitions, err := ExpandPartition(dir, &Partition{
		By:   "files",
		Glob: "**/*.go",
		// MaxPerPartition defaults to 25
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}
	if len(partitions) != 2 {
		t.Fatalf("expected 2 partitions with default size 25, got %d", len(partitions))
	}
}

func TestExpandPartitionMaxCap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Create enough files to exceed maxPartitions (10) with small partition size
	createTestFiles(t, dir, 60, ".go")

	partitions, err := ExpandPartition(dir, &Partition{
		By:              "files",
		Glob:            "**/*.go",
		MaxPerPartition: 5,
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}
	// 60/5 = 12, capped at 10. Last partition gets extras.
	if len(partitions) != maxPartitions {
		t.Fatalf("expected %d partitions (capped), got %d", maxPartitions, len(partitions))
	}
	// Total files across all partitions should still be 60
	total := countFiles(partitions)
	if total != 60 {
		t.Fatalf("expected 60 total files, got %d", total)
	}
}

func TestExpandPartitionSubdirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create files in subdirectories
	subdir := filepath.Join(dir, "pkg", "auth")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	createTestFiles(t, subdir, 5, ".go")

	subdir2 := filepath.Join(dir, "pkg", "api")
	if err := os.MkdirAll(subdir2, 0o755); err != nil {
		t.Fatal(err)
	}
	createTestFiles(t, subdir2, 5, ".go")

	partitions, err := ExpandPartition(dir, &Partition{
		By:   "files",
		Glob: "**/*.go",
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}
	total := countFiles(partitions)
	if total != 10 {
		t.Fatalf("expected 10 files from subdirs, got %d", total)
	}
}

func TestExpandPartitionSkipsDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create files in a skipped directory
	vendorDir := filepath.Join(dir, "vendor")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	createTestFiles(t, vendorDir, 10, ".go")

	// Create files in a normal directory
	srcDir := filepath.Join(dir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	createTestFiles(t, srcDir, 5, ".go")

	partitions, err := ExpandPartition(dir, &Partition{
		By:   "files",
		Glob: "**/*.go",
	})
	if err != nil {
		t.Fatalf("ExpandPartition: %v", err)
	}
	total := countFiles(partitions)
	if total != 5 {
		t.Fatalf("expected 5 files (vendor skipped), got %d", total)
	}
}

func TestExpandPartitionNilPartition(t *testing.T) {
	t.Parallel()
	_, err := ExpandPartition(t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for nil partition")
	}
}

func TestFormatPartitionPrompt(t *testing.T) {
	t.Parallel()
	files := []string{"pkg/auth/handler.go", "pkg/auth/middleware.go"}
	prompt := FormatPartitionPrompt(files, 3, 12)

	if !strings.Contains(prompt, "3 of 12") {
		t.Fatal("expected partition index in prompt")
	}
	if !strings.Contains(prompt, "pkg/auth/handler.go") {
		t.Fatal("expected file in prompt")
	}
	if !strings.Contains(prompt, "Do NOT read or analyze files outside") {
		t.Fatal("expected boundary instruction")
	}
}

func TestValidatePartitionErrors(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantPart string
	}{
		{
			name: "partition with agents",
			yaml: `name: p
stages:
  - name: x
    agents: [a, b]
    partition:
      by: files
      glob: "**/*.go"`,
			wantPart: "partition requires a single agent",
		},
		{
			name: "partition invalid by",
			yaml: `name: p
stages:
  - name: x
    agent: a
    partition:
      by: directories
      glob: "**/*.go"`,
			wantPart: "partition.by must be",
		},
		{
			name: "partition missing glob",
			yaml: `name: p
stages:
  - name: x
    agent: a
    partition:
      by: files`,
			wantPart: "partition.glob is required",
		},
		{
			name: "invalid summarize value",
			yaml: `name: p
stages:
  - name: x
    agent: a
    summarize: sometimes`,
			wantPart: "summarize must be",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantPart) {
				t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantPart)
			}
		})
	}
}

func TestRunnerPartitionedStage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createTestFiles(t, dir, 50, ".go")

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				Partition: &Partition{
					By:              "files",
					Glob:            "**/*.go",
					MaxPerPartition: 20,
				},
			},
		},
	}

	var agentCalls int64
	runner := &Runner{
		Pipeline:   p,
		WorkingDir: dir,
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			atomic.AddInt64(&agentCalls, 1)
			// Verify partition prompt is present
			if !strings.Contains(prompt, "Partition Assignment") {
				t.Errorf("expected partition prompt, got: %s", prompt[:min(200, len(prompt))])
			}
			return "ok", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Status != StatusPassed {
		t.Fatalf("status = %s, want passed", report.Status)
	}
	// 50 files / 20 per partition = 3 parallel agents
	if atomic.LoadInt64(&agentCalls) != 3 {
		t.Fatalf("expected 3 agent calls, got %d", agentCalls)
	}
	if len(report.Stages[0].Agents) != 3 {
		t.Fatalf("expected 3 agent results, got %d", len(report.Stages[0].Agents))
	}
}

func TestRunnerPartitionedStageNoFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// No .go files

	p := &Pipeline{
		Name:    "test",
		Version: "v1",
		Stages: []Stage{
			{
				Name:  "review",
				Agent: "go-review",
				Partition: &Partition{
					By:   "files",
					Glob: "**/*.go",
				},
			},
		},
	}

	runner := &Runner{
		Pipeline:   p,
		WorkingDir: dir,
		Prompt:     "Begin.",
		RunAgent: func(ctx context.Context, agentName, prompt, workingDir, mode string, vars map[string]string) (string, *metrics.Metrics, error) {
			t.Fatal("agent should not run when no files match")
			return "", nil, nil
		},
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Stages[0].Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", report.Stages[0].Status)
	}
}

func TestParsePartitionConfig(t *testing.T) {
	t.Parallel()
	yaml := `
name: test
version: v1
stages:
  - name: review
    agent: go-review
    partition:
      by: files
      glob: "**/*.go"
      max_per_partition: 15
    summarize: auto
    summarize_prompt: "Extract findings only."
`
	p, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := p.Stages[0]
	if s.Partition == nil {
		t.Fatal("expected partition config")
	}
	if s.Partition.By != "files" {
		t.Fatalf("partition.by = %q", s.Partition.By)
	}
	if s.Partition.Glob != "**/*.go" {
		t.Fatalf("partition.glob = %q", s.Partition.Glob)
	}
	if s.Partition.MaxPerPartition != 15 {
		t.Fatalf("partition.max_per_partition = %d", s.Partition.MaxPerPartition)
	}
	if s.Summarize != "auto" {
		t.Fatalf("summarize = %q", s.Summarize)
	}
	if s.SummarizePrompt != "Extract findings only." {
		t.Fatalf("summarize_prompt = %q", s.SummarizePrompt)
	}
}

func TestPartitionGlob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern string
		match   []string
		noMatch []string
	}{
		{
			pattern: "*.go",
			match:   []string{"foo.go", "bar.go"},
			noMatch: []string{"foo/bar.go", "foo.txt"},
		},
		{
			pattern: "**/*.go",
			match:   []string{"foo/bar.go", "a/b/c.go"},
			noMatch: []string{"foo.txt"},
		},
		{
			pattern: "cmd/?.go",
			match:   []string{"cmd/a.go"},
			noMatch: []string{"cmd/ab.go"},
		},
		{
			pattern: "path/to/file.go",
			match:   []string{"path/to/file.go"},
			noMatch: []string{"path/to/other.go"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.pattern, func(t *testing.T) {
			t.Parallel()
			if !doublestar.ValidatePattern(tt.pattern) {
				t.Fatalf("ValidatePattern(%q) returned false", tt.pattern)
			}
			for _, m := range tt.match {
				matched, _ := doublestar.Match(tt.pattern, m)
				if !matched {
					t.Errorf("pattern %q should match %q", tt.pattern, m)
				}
			}
			for _, nm := range tt.noMatch {
				matched, _ := doublestar.Match(tt.pattern, nm)
				if matched {
					t.Errorf("pattern %q should NOT match %q", tt.pattern, nm)
				}
			}
		})
	}
}

func TestFormatReport_InvalidFormat(t *testing.T) {
	t.Parallel()
	p := &Pipeline{Name: "test-pipe", Output: &Output{Format: "xml"}}
	runner := &Runner{Pipeline: p}
	report := &Report{Pipeline: "test-pipe", Status: "success"}
	// Unknown format falls back to markdown.
	out, err := runner.FormatReport(report)
	if err != nil {
		t.Fatalf("FormatReport() error: %v", err)
	}
	if out == "" {
		t.Error("FormatReport() returned empty string")
	}
}
