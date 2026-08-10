package runner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/tools"
)

func TestInvokeAgenticCLIRejectsNonLocalEnvironment(t *testing.T) {
	t.Parallel()
	bundle := &agent.Bundle{
		Environment: &executor.Config{Type: "docker"},
		WorkDir:     t.TempDir(),
	}
	m := metrics.New("claude-code", "")
	_, _, err := invokeAgenticCLI(context.Background(), &RunOptions{}, "claude-code", "", "sys", bundle, m)
	if err == nil || !strings.Contains(err.Error(), `environment type "docker" is not supported`) {
		t.Fatalf("err = %v, want non-local environment rejection", err)
	}
}

func TestInvokeAgenticCLIRejectsEditGates(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		bundle *agent.Bundle
	}{
		{name: "comments_only", bundle: &agent.Bundle{CommentsOnly: true, WorkDir: "."}},
		{name: "ascii_only", bundle: &agent.Bundle{ASCIIOnly: true, WorkDir: "."}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := metrics.New("agy", "")
			_, _, err := invokeAgenticCLI(context.Background(), &RunOptions{}, "agy", "", "sys", tt.bundle, m)
			if err == nil || !strings.Contains(err.Error(), "cannot enforce comments_only/ascii_only") {
				t.Fatalf("err = %v, want edit-gate rejection", err)
			}
		})
	}
}

func TestSkillFileFallback(t *testing.T) {
	t.Parallel()
	if got := skillFileFallback(nil); got != "" {
		t.Fatalf("skillFileFallback(nil) = %q, want empty", got)
	}
	entries := []skill.Entry{
		{Manifest: &skill.Manifest{Name: "zeta"}, Dir: "/skills/zeta"},
		{Manifest: &skill.Manifest{Name: "alpha"}, Dir: "/skills/alpha"},
	}
	got := skillFileFallback(entries)
	if !strings.Contains(got, "## Skill loading override") {
		t.Errorf("missing override header: %s", got)
	}
	alphaIdx := strings.Index(got, filepath.Join("/skills/alpha", skill.FileName))
	zetaIdx := strings.Index(got, filepath.Join("/skills/zeta", skill.FileName))
	if alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("missing SKILL.md paths: %s", got)
	}
	if alphaIdx > zetaIdx {
		t.Errorf("entries not sorted by name: %s", got)
	}
}

// initTestGitRepo creates a git repo with one committed file and returns its path.
func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "commit.gpgsign=false", "commit", "-m", "init")
	return dir
}

// installFakeAgenticCLI puts a fake `claude` on PATH whose behavior is the
// given shell body, followed by printing a canned success envelope.
func installFakeAgenticCLI(t *testing.T, body string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\n" + body + "\n" +
		`printf '%s' '{"is_error":false,"num_turns":4,"result":"done","usage":{"input_tokens":100,"output_tokens":20}}'` + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// git must stay reachable for the worktree snapshot.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestInvokeAgenticCLIMarksEditsOnTreeChange(t *testing.T) {
	repo := initTestGitRepo(t)
	installFakeAgenticCLI(t, `echo changed >> a.txt`)

	ctx := tools.InitEdits(context.Background())
	bundle := &agent.Bundle{WorkDir: repo, User: "edit the file"}
	m := metrics.New("claude-code", "")
	resp, m, err := invokeAgenticCLI(ctx, &RunOptions{}, "claude-code", "", "sys", bundle, m)
	if err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	if resp != "done" {
		t.Errorf("response = %q, want %q", resp, "done")
	}
	if !tools.EditsApplied(ctx) {
		t.Error("EditsApplied = false, want true after working tree changed")
	}
	if m.InputTokens() != 100 || m.OutputTokens() != 20 || m.Iterations() != 4 {
		t.Errorf("metrics = %d/%d tokens, %d iterations; want 100/20, 4",
			m.InputTokens(), m.OutputTokens(), m.Iterations())
	}
}

// TestResolveModelPrecedence_AgenticCLIProvider pins the short-circuit for
// explicitly requested CLI providers: the config default model (which belongs
// to a different API provider) must not be paired with the CLI, the model may
// stay empty so the CLI's own default is used, and a manifest entry for the
// CLI provider pins the model when present.
func TestResolveModelPrecedence_AgenticCLIProvider(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		provider  string
		bundle    *agent.Bundle
		wantModel string
	}{
		{
			name:      "no manifest model stays empty despite config default",
			provider:  "claude-code",
			bundle:    &agent.Bundle{},
			wantModel: "",
		},
		{
			name:     "manifest entry for the CLI provider pins the model",
			provider: "claude-code",
			bundle: &agent.Bundle{Models: []agent.ModelPreference{
				{Provider: "openai", Model: "gpt-4o"},
				{Provider: "claude-code", Model: "sonnet"},
			}},
			wantModel: "sonnet",
		},
		{
			name:      "claude alias resolves",
			provider:  "claude",
			bundle:    &agent.Bundle{Models: []agent.ModelPreference{{Provider: "claude-code", Model: "opus"}}},
			wantModel: "opus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &RunOptions{Provider: tt.provider, ConfigModel: "gpt-4o", ConfigProvider: "openai"}
			warn, err := ResolveModelPrecedence(context.Background(), opts, tt.bundle)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if warn != "" {
				t.Fatalf("unexpected warning: %q", warn)
			}
			if opts.Model != tt.wantModel {
				t.Errorf("Model = %q, want %q", opts.Model, tt.wantModel)
			}
		})
	}
}

func TestInvokeAgenticCLINoEditsNoMark(t *testing.T) {
	repo := initTestGitRepo(t)
	installFakeAgenticCLI(t, ":")

	ctx := tools.InitEdits(context.Background())
	bundle := &agent.Bundle{WorkDir: repo, User: "look around"}
	m := metrics.New("claude-code", "")
	if _, _, err := invokeAgenticCLI(ctx, &RunOptions{}, "claude-code", "", "sys", bundle, m); err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	if tools.EditsApplied(ctx) {
		t.Error("EditsApplied = true, want false for a read-only run")
	}
}
