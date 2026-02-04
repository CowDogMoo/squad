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

func TestBuildBundleErrors(t *testing.T) {
	baseManifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
references:
  - ref.md
`

	setup := func(t *testing.T) (string, string) {
		t.Helper()
		dir := t.TempDir()
		agentDir := filepath.Join(dir, "demo")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(baseManifest), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "system.txt"), []byte("system"), 0o644); err != nil {
			t.Fatalf("write system: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "wrapper.txt"), []byte("wrapper"), 0o644); err != nil {
			t.Fatalf("write wrapper: %v", err)
		}
		if err := os.WriteFile(filepath.Join(agentDir, "ref.md"), []byte("reference"), 0o644); err != nil {
			t.Fatalf("write ref: %v", err)
		}
		return dir, agentDir
	}

	tests := []struct {
		name     string
		mutate   func(t *testing.T, dir, agentDir string)
		wantPart string
	}{
		{
			name: "missing manifest",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(agentDir, "agent.yaml")); err != nil {
					t.Fatalf("remove manifest: %v", err)
				}
			},
			wantPart: "failed to read agent manifest",
		},
		{
			name: "invalid manifest",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte("bad: ["), 0o644); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
			},
			wantPart: "failed to parse agent manifest",
		},
		{
			name: "missing system prompt",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(agentDir, "system.txt")); err != nil {
					t.Fatalf("remove system: %v", err)
				}
			},
			wantPart: "failed to read system prompt",
		},
		{
			name: "missing wrapper",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(agentDir, "wrapper.txt")); err != nil {
					t.Fatalf("remove wrapper: %v", err)
				}
			},
			wantPart: "failed to read agent wrapper",
		},
		{
			name: "missing reference",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				if err := os.Remove(filepath.Join(agentDir, "ref.md")); err != nil {
					t.Fatalf("remove ref: %v", err)
				}
			},
			wantPart: "failed to read reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, agentDir := setup(t)
			if tt.mutate != nil {
				tt.mutate(t, dir, agentDir)
			}
			_, err := BuildBundle(dir, "demo", "prompt", "/work", "")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantPart) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantPart)
			}
		})
	}
}
