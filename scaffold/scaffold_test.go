package scaffold_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/scaffold"
)

func TestIsValidAgentName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"valid-hyphenated", "go-agent", true},
		{"valid-simple", "myagent", true},
		{"valid-alnum", "a1", true},
		{"invalid-single-char", "a", false},
		{"invalid-start-hyphen", "-bad", false},
		{"invalid-trailing-hyphen", "name-", false},
		{"invalid-uppercase", "BadUpper", false},
		{"invalid-space", "with space", false},
		{"invalid-underscore", "a_b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaffold.IsValidAgentName(tt.in)
			if got != tt.want {
				t.Fatalf("IsValidAgentName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestToTitleCase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"hyphenated", "my-agent", "My Agent"},
		{"single", "agent", "Agent"},
		{"multi", "multi-word-name", "Multi Word Name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scaffold.ToTitleCase(tt.in)
			if got != tt.want {
				t.Fatalf("ToTitleCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCreateAgent_InvalidInputs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	// Invalid name rejected first
	err := scaffold.CreateAgent(ctx, scaffold.CreateOptions{
		Name:      "INVALID",
		Lang:      "go",
		AgentsDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid agent name") {
		t.Fatalf("expected invalid agent name error, got %v", err)
	}
	// Unknown language
	err = scaffold.CreateAgent(ctx, scaffold.CreateOptions{
		Name:      "valid-name",
		Lang:      "unknown",
		AgentsDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown language") {
		t.Fatalf("expected unknown language error, got %v", err)
	}
}

func TestCopyAgent_CopiesAndUpdates(t *testing.T) {
	// Uses filesystem; avoid t.Parallel() due to temp dir mutation per test.
	ctx := context.Background()
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	if err := os.MkdirAll(filepath.Join(from, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "agent.yaml"), []byte("name: from\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "README.md"), []byte("docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffold.CopyAgent(ctx, dir, "from", "to", false); err != nil {
		t.Fatalf("CopyAgent error: %v", err)
	}
	dst := filepath.Join(dir, "to")
	// agent.yaml should be updated
	b, err := os.ReadFile(filepath.Join(dst, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "name: to") {
		t.Fatalf("agent.yaml not updated: %q", string(b))
	}
	// other files copied
	if _, err := os.Stat(filepath.Join(dst, "README.md")); err != nil {
		t.Fatalf("expected README.md copied: %v", err)
	}
}

func TestCreatePipeline_CreateAndOverwrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	// Invalid name
	err := scaffold.CreatePipeline(ctx, scaffold.CreatePipelineOptions{
		Name:      "INVALID",
		OutputDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid pipeline name") {
		t.Fatalf("expected invalid name error, got %v", err)
	}
	// Create valid pipeline
	err = scaffold.CreatePipeline(ctx, scaffold.CreatePipelineOptions{
		Name:      "sec-pipeline",
		OutputDir: dir,
	})
	if err != nil {
		t.Fatalf("CreatePipeline error: %v", err)
	}
	// Second create without force should error
	err = scaffold.CreatePipeline(ctx, scaffold.CreatePipelineOptions{
		Name:      "sec-pipeline",
		OutputDir: dir,
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already exists error, got %v", err)
	}
	// With force should overwrite
	err = scaffold.CreatePipeline(ctx, scaffold.CreatePipelineOptions{
		Name:      "sec-pipeline",
		OutputDir: dir,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline(force) error: %v", err)
	}
}
