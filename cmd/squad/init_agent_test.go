package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// runInitAgentCmd executes a freshly constructed `init agent` cobra command
// with the given flags + args so each test invocation starts from default
// flag state.
func runInitAgentCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newInitAgentCmd()
	cmd.Flags().VisitAll(func(f *pflag.Flag) { _ = cmd.Flags().Set(f.Name, f.DefValue) })
	cmd.SetContext(context.Background())
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.ParseFlags(args); err != nil {
		return buf.String(), err
	}
	if err := cmd.RunE(cmd, cmd.Flags().Args()); err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

func TestRunInitAgent_createsScaffoldFromTemplates(t *testing.T) {
	tmp := t.TempDir()
	agentsDir := filepath.Join(tmp, "agents")
	if _, err := runInitAgentCmd(t,
		"my-review",
		"--lang", "go",
		"--agents-dir", agentsDir,
	); err != nil {
		t.Fatalf("init agent: %v", err)
	}

	for _, rel := range []string{
		"my-review/agent.yaml",
		"my-review/system.md",
		"my-review/agent.md",
		"my-review/task.md",
		"my-review/README.md",
		"my-review/references/my-review-guide.md",
	} {
		if _, err := os.Stat(filepath.Join(agentsDir, rel)); err != nil {
			t.Errorf("expected %s to be created: %v", rel, err)
		}
	}
}

func TestRunInitAgent_invalidLangErrors(t *testing.T) {
	tmp := t.TempDir()
	_, err := runInitAgentCmd(t,
		"any-name",
		"--lang", "cobol",
		"--agents-dir", filepath.Join(tmp, "agents"),
	)
	if err == nil {
		t.Fatal("expected unknown language to error")
	}
}

func TestRunInitAgent_copyFromExisting(t *testing.T) {
	tmp := t.TempDir()
	agentsDir := filepath.Join(tmp, "agents")

	// Seed an existing agent via the same template path.
	if _, err := runInitAgentCmd(t,
		"seed-agent",
		"--lang", "python",
		"--agents-dir", agentsDir,
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Copy it.
	if _, err := runInitAgentCmd(t,
		"derived-agent",
		"--from", "seed-agent",
		"--agents-dir", agentsDir,
	); err != nil {
		t.Fatalf("copy: %v", err)
	}

	manifestPath := filepath.Join(agentsDir, "derived-agent", "agent.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read copied manifest: %v", err)
	}
	if !strings.Contains(string(data), "name: derived-agent") {
		t.Fatalf("expected manifest name rewritten to derived-agent, got:\n%s", data)
	}
}

func TestRunInitAgent_existingWithoutForceErrors(t *testing.T) {
	tmp := t.TempDir()
	agentsDir := filepath.Join(tmp, "agents")

	if _, err := runInitAgentCmd(t, "dup", "--lang", "generic", "--agents-dir", agentsDir); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := runInitAgentCmd(t, "dup", "--lang", "generic", "--agents-dir", agentsDir)
	if err == nil {
		t.Fatal("expected re-creation without --force to error")
	}
}
