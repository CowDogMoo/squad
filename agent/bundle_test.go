package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBundleWithModeOverride(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
references:
  - ref.md
modes:
  fast:
    entrypoint: system_fast.txt
    wrapper: wrapper_fast.txt
    references:
      - ref_fast.md
`

	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system.txt"), []byte("system"), 0o644); err != nil {
		t.Fatalf("write system: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "system_fast.txt"), []byte("system fast"), 0o644); err != nil {
		t.Fatalf("write system fast: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper.txt"), []byte("wrapper"), 0o644); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "wrapper_fast.txt"), []byte("wrapper fast"), 0o644); err != nil {
		t.Fatalf("write wrapper fast: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "ref.md"), []byte("reference"), 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "ref_fast.md"), []byte("reference fast"), 0o644); err != nil {
		t.Fatalf("write ref fast: %v", err)
	}

	bundle, err := BuildBundle(dir, "demo", "do the thing", "/work", "fast")
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	if !strings.Contains(bundle.System, "system fast") || !strings.Contains(bundle.System, "wrapper fast") {
		t.Fatalf("expected mode override content, got %s", bundle.System)
	}
	if !strings.Contains(bundle.System, "reference fast") {
		t.Fatalf("expected reference content")
	}
	if !strings.Contains(string(bundle.Combined), "## User Request") || !strings.Contains(string(bundle.Combined), "do the thing") {
		t.Fatalf("expected combined output to include user prompt")
	}
}
