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

func TestCreateAgent_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	opts := scaffold.CreateOptions{
		Name:      "my-agent",
		Lang:      "go",
		AgentsDir: dir,
	}
	if err := scaffold.CreateAgent(ctx, opts); err != nil {
		t.Fatalf("CreateAgent() unexpected error: %v", err)
	}
	agentPath := filepath.Join(dir, "my-agent")
	for _, f := range []string{"agent.yaml", "system.md", "agent.md", "task.md", "README.md"} {
		if _, err := os.Stat(filepath.Join(agentPath, f)); err != nil {
			t.Errorf("expected file %s to exist: %v", f, err)
		}
	}
}

func TestCreateAgent_Force(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	opts := scaffold.CreateOptions{
		Name:      "my-agent",
		Lang:      "go",
		AgentsDir: dir,
	}
	if err := scaffold.CreateAgent(ctx, opts); err != nil {
		t.Fatalf("first CreateAgent() error: %v", err)
	}
	opts.Force = true
	if err := scaffold.CreateAgent(ctx, opts); err != nil {
		t.Fatalf("second CreateAgent() with force error: %v", err)
	}
}

func TestCopyAgent_SourceNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	err := scaffold.CopyAgent(ctx, dir, "nonexistent", "new-agent", false)
	if err == nil {
		t.Fatal("expected error for missing source agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

func TestCopyAgent_InvalidDestName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	err := scaffold.CopyAgent(ctx, dir, "src", "INVALID NAME", false)
	if err == nil {
		t.Fatal("expected error for invalid dest name")
	}
}

func TestCreateAgent_GeneratedDescriptionPerLang(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := []struct {
		lang     string
		contains string
	}{
		{"go", "for Go codebases"},
		{"python", "for Python codebases"},
		{"bash", "for Bash scripts"},
		{"ansible", "for Ansible playbooks and roles"},
		{"generic", "Autonomous My Agent agent"},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			opts := scaffold.CreateOptions{
				Name:      "my-agent",
				Lang:      tc.lang,
				AgentsDir: dir,
			}
			if err := scaffold.CreateAgent(ctx, opts); err != nil {
				t.Fatalf("CreateAgent(%s) error: %v", tc.lang, err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "my-agent", "agent.yaml"))
			if err != nil {
				t.Fatalf("read agent.yaml: %v", err)
			}
			if !strings.Contains(string(b), tc.contains) {
				t.Fatalf("agent.yaml for lang %q missing %q:\n%s", tc.lang, tc.contains, string(b))
			}
		})
	}
}

func TestCreateAgent_ExistsWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := context.Background()
	opts := scaffold.CreateOptions{
		Name:      "dup-agent",
		Lang:      "go",
		AgentsDir: dir,
	}
	if err := scaffold.CreateAgent(ctx, opts); err != nil {
		t.Fatalf("first CreateAgent() error: %v", err)
	}
	err := scaffold.CreateAgent(ctx, opts)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestCopyAgent_ForceOverwrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "agent.yaml"), []byte("name: from\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-create dst with a stale file that should disappear after force overwrite.
	dst := filepath.Join(dir, "to")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffold.CopyAgent(ctx, dir, "from", "to", true); err != nil {
		t.Fatalf("CopyAgent(force) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale.txt to be removed by force overwrite, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "agent.yaml")); err != nil {
		t.Fatalf("expected agent.yaml after force overwrite: %v", err)
	}
}

func TestCopyAgent_DestExistsWithoutForce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "agent.yaml"), []byte("name: from\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "to"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := scaffold.CopyAgent(ctx, dir, "from", "to", false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected already-exists error, got %v", err)
	}
}

func TestCopyAgent_MissingManifest(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	from := filepath.Join(dir, "from")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatal(err)
	}
	// Note: no agent.yaml in source — copy succeeds but manifest read fails.
	if err := os.WriteFile(filepath.Join(from, "README.md"), []byte("docs"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := scaffold.CopyAgent(ctx, dir, "from", "to", false)
	if err == nil || !strings.Contains(err.Error(), "failed to read manifest") {
		t.Fatalf("expected manifest read error, got %v", err)
	}
}

func TestCreatePipeline_DefaultDescription(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	if err := scaffold.CreatePipeline(ctx, scaffold.CreatePipelineOptions{
		Name:      "default-desc",
		OutputDir: dir,
	}); err != nil {
		t.Fatalf("CreatePipeline error: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "default-desc.yaml"))
	if err != nil {
		t.Fatalf("read pipeline yaml: %v", err)
	}
	if !strings.Contains(string(b), "Pipeline for Default Desc") {
		t.Fatalf("expected default description in output:\n%s", string(b))
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
