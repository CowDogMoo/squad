package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFiles writes multiple files to the given directory.
func writeTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// setupTestAgent creates a test agent directory with the given files.
func setupTestAgent(t *testing.T, name string, files map[string]string) (dir, agentDir string) {
	t.Helper()
	dir = t.TempDir()
	agentDir = filepath.Join(dir, name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestFiles(t, agentDir, files)
	return dir, agentDir
}

func TestBuildBundleWithModeOverride(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
references:
  - ref.md
task: task.md
modes:
  fast:
    entrypoint: system_fast.txt
    wrapper: wrapper_fast.txt
    references:
      - ref_fast.md
    task: task_fast.md
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":       manifest,
		"system.txt":       "system",
		"system_fast.txt":  "system fast",
		"wrapper.txt":      "wrapper",
		"wrapper_fast.txt": "wrapper fast",
		"ref.md":           "reference",
		"ref_fast.md":      "reference fast",
		"task.md":          "default task instructions",
		"task_fast.md":     "fast task instructions",
	})

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
	if !strings.Contains(bundle.System, "fast task instructions") {
		t.Fatalf("expected task content in system bundle")
	}
	if strings.Contains(bundle.System, "default task instructions") {
		t.Fatalf("expected mode override to replace default task")
	}
	if !strings.Contains(string(bundle.Combined), "## User Message") || !strings.Contains(string(bundle.Combined), "do the thing") {
		t.Fatalf("expected combined output to include user message")
	}
}

func TestBuildBundleDefaultUserMessage(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "", "/work", "")
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	if bundle.User != "Begin." {
		t.Fatalf("expected default user message 'Begin.', got %q", bundle.User)
	}
	if !strings.Contains(string(bundle.Combined), "Begin.") {
		t.Fatalf("expected combined output to include default user message")
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
	baseFiles := map[string]string{
		"agent.yaml":  baseManifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
		"ref.md":      "reference",
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
		{
			name: "missing task",
			mutate: func(t *testing.T, _, agentDir string) {
				t.Helper()
				manifestWithTask := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
references:
  - ref.md
task: missing.md
`
				if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifestWithTask), 0o644); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
			},
			wantPart: "failed to read task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, agentDir := setupTestAgent(t, "demo", baseFiles)
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
