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

// assertContains checks that s contains substr, failing with a descriptive message.
func assertContains(t *testing.T, s, substr, context string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected %s to contain %q", context, substr)
	}
}

// assertNotContains checks that s does not contain substr.
func assertNotContains(t *testing.T, s, substr, context string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected %s to NOT contain %q", context, substr)
	}
}

// conditionalTestFiles returns test files with Go template conditionals.
func conditionalTestFiles() map[string]string {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
references:
  - ref.md
task: task.md
`
	systemContent := `common system content
{{if eq .Mode "edit"}}
edit mode system content
{{end}}
{{if eq .Mode "readonly"}}
readonly mode system content
{{end}}
`
	wrapperContent := `common wrapper content
{{if eq .Mode "edit"}}
edit mode wrapper
{{end}}
{{if eq .Mode "readonly"}}
readonly mode wrapper
{{end}}
`
	taskContent := `common task instructions
{{if eq .Mode "edit"}}
edit mode task instructions
{{end}}
{{if eq .Mode "readonly"}}
readonly mode task instructions
{{end}}
`
	return map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": wrapperContent,
		"ref.md":      "reference content",
		"task.md":     taskContent,
	}
}

func TestBuildBundle_ReadonlyMode(t *testing.T) {
	dir, _ := setupTestAgent(t, "demo", conditionalTestFiles())

	bundle, err := BuildBundle(dir, "demo", "do the thing", "/work", "readonly", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	assertContains(t, bundle.System, "readonly mode system content", "system")
	assertContains(t, bundle.System, "readonly mode wrapper", "wrapper")
	assertContains(t, bundle.System, "readonly mode task instructions", "task")
	assertContains(t, bundle.System, "Mode: readonly", "header")
	assertNotContains(t, bundle.System, "edit mode", "system")
}

func TestBuildBundle_EditModeDefault(t *testing.T) {
	dir, _ := setupTestAgent(t, "demo", conditionalTestFiles())

	bundle, err := BuildBundle(dir, "demo", "do the thing", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	assertContains(t, bundle.System, "edit mode system content", "system")
	assertContains(t, bundle.System, "edit mode wrapper", "wrapper")
	assertContains(t, bundle.System, "edit mode task instructions", "task")
	assertContains(t, bundle.System, "Mode: edit", "header")
	assertNotContains(t, bundle.System, "readonly mode", "system")
}

func TestBuildBundle_CommonContentPreserved(t *testing.T) {
	dir, _ := setupTestAgent(t, "demo", conditionalTestFiles())

	bundle, err := BuildBundle(dir, "demo", "do the thing", "/work", "readonly", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	assertContains(t, bundle.System, "common system content", "system")
	assertContains(t, bundle.System, "common wrapper content", "wrapper")
	assertContains(t, bundle.System, "common task instructions", "task")
}

func TestBuildBundle_UserMessageIncluded(t *testing.T) {
	dir, _ := setupTestAgent(t, "demo", conditionalTestFiles())

	bundle, err := BuildBundle(dir, "demo", "do the thing", "/work", "readonly", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	combined := string(bundle.Combined)
	assertContains(t, combined, "## User Message", "combined")
	assertContains(t, combined, "do the thing", "combined")
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

	bundle, err := BuildBundle(dir, "demo", "", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	if bundle.User != "Begin." {
		t.Fatalf("expected default user message 'Begin.', got %q", bundle.User)
	}
	assertContains(t, string(bundle.Combined), "Begin.", "combined")
}

func TestBuildBundle_IncludeFunction(t *testing.T) {
	// Create agents directory with _templates
	dir := t.TempDir()
	templatesDir := filepath.Join(dir, "_templates", "hard-rules")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "test-rules.md"), []byte("Included rule content"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	// Create agent with include directive
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
`
	systemContent := `System prompt
{{include "hard-rules/test-rules.md"}}
End of system`
	writeTestFiles(t, agentDir, map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	assertContains(t, bundle.System, "Included rule content", "system")
	assertContains(t, bundle.System, "System prompt", "system")
	assertContains(t, bundle.System, "End of system", "system")
}

func TestBuildBundle_IncludePathTraversal(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
`
	systemContent := `{{include "../../../etc/passwd"}}`
	writeTestFiles(t, agentDir, map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": "wrapper",
	})

	_, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err == nil {
		t.Fatalf("expected error for path traversal")
	}
	// os.OpenInRoot (Go 1.24+) handles path traversal; just verify it fails
	assertContains(t, err.Error(), "failed to include template", "error")
}

func TestBuildBundle_IncludeAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
`
	systemContent := `{{include "/etc/passwd"}}`
	writeTestFiles(t, agentDir, map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": "wrapper",
	})

	_, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err == nil {
		t.Fatalf("expected error for absolute path")
	}
	// os.OpenInRoot (Go 1.24+) handles absolute paths; just verify it fails
	assertContains(t, err.Error(), "failed to include template", "error")
}

func TestBuildBundle_IncludeMissingTemplate(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
`
	systemContent := `{{include "nonexistent.md"}}`
	writeTestFiles(t, agentDir, map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": "wrapper",
	})

	_, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err == nil {
		t.Fatalf("expected error for missing template")
	}
	assertContains(t, err.Error(), "failed to include template", "error")
}

func TestBuildBundle_TemplateVars(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "demo")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir agent: %v", err)
	}

	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
task: task.txt
`
	// Test .Var and .Default template functions
	systemContent := `Target: {{.Default "COVERAGE_TARGET" "75"}}%`
	wrapperContent := `wrapper`
	taskContent := `Coverage target is {{.Var "COVERAGE_TARGET"}} percent`
	writeTestFiles(t, agentDir, map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  systemContent,
		"wrapper.txt": wrapperContent,
		"task.txt":    taskContent,
	})

	// Test with no vars - should use default
	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err != nil {
		t.Fatalf("BuildBundle with nil vars: %v", err)
	}
	assertContains(t, bundle.System, "Target: 75%", "system with default")
	assertContains(t, bundle.System, "Coverage target is  percent", "task with empty var")

	// Test with vars provided
	vars := map[string]string{"COVERAGE_TARGET": "85"}
	bundle2, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", vars)
	if err != nil {
		t.Fatalf("BuildBundle with vars: %v", err)
	}
	assertContains(t, bundle2.System, "Target: 85%", "system with var override")
	assertContains(t, bundle2.System, "Coverage target is 85 percent", "task with var")
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
			_, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantPart) {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantPart)
			}
		})
	}
}
