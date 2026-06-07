package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/mcp"
	yamlPkg "gopkg.in/yaml.v3"
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

func TestBuildBundle_InlinePromptRemoteOnly(t *testing.T) {
	manifest := `name: weekly-planner
version: 1
working_dir: none
models:
  - provider: anthropic
    model: claude-sonnet-4-6
prompt: |
  You are a calendar bot.
  Use mcp__cal__create_event to add events.
mcp_servers:
  - name: cal
    transport: streamable_http
    url: https://calendarmcp.googleapis.com/mcp/v1
`
	dir, _ := setupTestAgent(t, "weekly-planner", map[string]string{"agent.yaml": manifest})

	bundle, err := BuildBundle(dir, "weekly-planner", "do it", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if !bundle.RemoteOnly {
		t.Fatal("bundle.RemoteOnly should be true for working_dir: none")
	}
	assertContains(t, bundle.System, "You are a calendar bot.", "system prompt")
	assertContains(t, bundle.System, "mcp__cal__create_event", "system prompt")
	assertNotContains(t, bundle.System, "Working Directory:", "remote-only system message")
	assertNotContains(t, bundle.System, "## Agent Wrapper", "inline-prompt system message")
	assertNotContains(t, bundle.System, "## References", "inline-prompt system message")
	if len(bundle.MCPServers) != 1 || bundle.MCPServers[0].Transport != "streamable_http" {
		t.Fatalf("expected one streamable_http MCP server, got %+v", bundle.MCPServers)
	}
	if bundle.User != "do it" {
		t.Fatalf("user = %q, want %q", bundle.User, "do it")
	}
}

func TestBuildBundle_InlinePromptWithWorkingDir(t *testing.T) {
	manifest := `name: simple
version: 1
models:
  - provider: openai
    model: gpt-4.1-mini
prompt: "just a prompt"
`
	dir, _ := setupTestAgent(t, "simple", map[string]string{"agent.yaml": manifest})
	bundle, err := BuildBundle(dir, "simple", "", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.RemoteOnly {
		t.Fatal("RemoteOnly should be false when working_dir is not 'none'")
	}
	assertContains(t, bundle.System, "Working Directory: /work", "system message")
	assertContains(t, bundle.System, "just a prompt", "system message")
}

func TestBuildBundle_InlinePromptTemplateExpansion(t *testing.T) {
	manifest := `name: tmpl
version: 1
working_dir: none
prompt: |
  Mode is {{.Mode}}. Var is {{.Var "FOO"}}.
`
	dir, _ := setupTestAgent(t, "tmpl", map[string]string{"agent.yaml": manifest})
	bundle, err := BuildBundle(dir, "tmpl", "go", "/work", "readonly", map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	assertContains(t, bundle.System, "Mode is readonly", "templated system prompt")
	assertContains(t, bundle.System, "Var is bar", "templated system prompt")
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
	// Create agents directory with _includes
	dir := t.TempDir()
	includesDir := filepath.Join(dir, "_includes", "hard-rules")
	if err := os.MkdirAll(includesDir, 0o755); err != nil {
		t.Fatalf("mkdir includes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(includesDir, "test-rules.md"), []byte("Included rule content"), 0o644); err != nil {
		t.Fatalf("write include: %v", err)
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

func TestBuildBundle_WithEnvironment(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
environment:
  type: local
  options:
    target: "{{.Mode}}-env"
    static: plain
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.Environment == nil {
		t.Fatalf("expected environment config")
	}
	if bundle.Environment.Options["target"] != "edit-env" {
		t.Fatalf("expected resolved env option, got %q", bundle.Environment.Options["target"])
	}
	if bundle.Environment.Options["static"] != "plain" {
		t.Fatalf("static option changed: %q", bundle.Environment.Options["static"])
	}
}

func TestBuildBundle_EnvironmentTemplateError(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
environment:
  type: local
  options:
    bad: "{{call .BadMethod}}"
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	_, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err == nil {
		t.Fatalf("expected error for bad environment template")
	}
}

func TestBuildBundle_MCPServerTemplateError(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
mcp_servers:
  - name: bad
    transport: sse
    url: "{{call .BadMethod}}"
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	_, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", nil)
	if err == nil {
		t.Fatalf("expected error for bad MCP server template")
	}
}

func TestBuildBundle_MCPServerTemplateResolved(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
mcp_servers:
  - name: test-server
    transport: sse
    url: '{{.Default "SERVER_URL" "http://localhost:8080"}}'
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	vars := map[string]string{"SERVER_URL": "http://remote:9090"}
	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "edit", vars)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if len(bundle.MCPServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(bundle.MCPServers))
	}
	if bundle.MCPServers[0].URL != "http://remote:9090" {
		t.Fatalf("MCP URL = %q, want http://remote:9090", bundle.MCPServers[0].URL)
	}
}

func TestBuildBundle_DependsOn(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
depends_on:
  - go-cobra
  - go-review
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	// Verify it parses without error (depends_on is metadata, not used at build time).
	_, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	// Verify manifest parsing via LoadManifest.
	m, err := LoadManifest(filepath.Join(dir, "demo"))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(m.DependsOn) != 2 || m.DependsOn[0] != "go-cobra" || m.DependsOn[1] != "go-review" {
		t.Fatalf("depends_on = %v, want [go-cobra go-review]", m.DependsOn)
	}
}

func TestBuildBundle_OutputContractJSON(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
output:
  format: json
  schema:
    type: object
    required:
      - status
      - findings
    properties:
      status:
        type: string
      findings:
        type: array
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	assertContains(t, bundle.System, "Output Contract", "system")
	assertContains(t, bundle.System, "single JSON object", "system")
	assertContains(t, bundle.System, "JSON Schema", "system")
	assertContains(t, bundle.System, `"status"`, "system")
}

func TestBuildBundle_OutputContractMarkdown(t *testing.T) {
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
output:
  format: markdown
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	// Markdown format should not inject JSON output contract.
	assertNotContains(t, bundle.System, "Output Contract", "system")
}

func TestBuildBundle_NoOutputConfig(t *testing.T) {
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

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}

	// No output config should not inject contract.
	assertNotContains(t, bundle.System, "Output Contract", "system")
}

func TestResolveEnvironmentTemplates(t *testing.T) {
	t.Parallel()

	t.Run("nil config", func(t *testing.T) {
		t.Parallel()
		if err := resolveEnvironmentTemplates(nil, TemplateData{}); err != nil {
			t.Fatalf("expected nil error for nil config, got: %v", err)
		}
	})

	t.Run("nil options", func(t *testing.T) {
		t.Parallel()
		cfg := &executor.Config{Type: "local"}
		if err := resolveEnvironmentTemplates(cfg, TemplateData{}); err != nil {
			t.Fatalf("expected nil error for nil options, got: %v", err)
		}
	})

	t.Run("resolves templates", func(t *testing.T) {
		t.Parallel()
		cfg := &executor.Config{
			Type: "local",
			Options: map[string]string{
				"mode":   "{{.Mode}}-resolved",
				"static": "no-template-here",
			},
		}
		data := TemplateData{Mode: "edit"}
		if err := resolveEnvironmentTemplates(cfg, data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Options["mode"] != "edit-resolved" {
			t.Fatalf("expected resolved mode, got: %q", cfg.Options["mode"])
		}
		if cfg.Options["static"] != "no-template-here" {
			t.Fatalf("static value changed: %q", cfg.Options["static"])
		}
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		t.Parallel()
		cfg := &executor.Config{
			Type:    "local",
			Options: map[string]string{"bad": "{{.Unclosed"},
		}
		if err := resolveEnvironmentTemplates(cfg, TemplateData{}); err == nil {
			t.Fatalf("expected parse error for invalid template")
		}
	})

	t.Run("execute error", func(t *testing.T) {
		t.Parallel()
		cfg := &executor.Config{
			Type:    "local",
			Options: map[string]string{"bad": `{{call .BadMethod}}`},
		}
		err := resolveEnvironmentTemplates(cfg, TemplateData{})
		if err == nil {
			t.Fatalf("expected execute error")
		}
		if !strings.Contains(err.Error(), "failed to resolve environment option") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResolveMCPServerTemplates(t *testing.T) {
	t.Parallel()

	t.Run("nil servers", func(t *testing.T) {
		t.Parallel()
		resolved, err := resolveMCPServerTemplates(nil, TemplateData{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resolved) != 0 {
			t.Fatalf("expected empty result, got %d", len(resolved))
		}
	})

	t.Run("no templates", func(t *testing.T) {
		t.Parallel()
		servers := []mcp.ServerConfig{
			{Name: "test", Command: "npx", Args: []string{"-y", "some-pkg"}, URL: "http://localhost:9876"},
		}
		resolved, err := resolveMCPServerTemplates(servers, TemplateData{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved[0].Command != "npx" || resolved[0].URL != "http://localhost:9876" {
			t.Fatalf("static values changed: command=%q url=%q", resolved[0].Command, resolved[0].URL)
		}
		if resolved[0].Args[0] != "-y" || resolved[0].Args[1] != "some-pkg" {
			t.Fatalf("static args changed: %v", resolved[0].Args)
		}
	})

	t.Run("uses defaults when vars missing", func(t *testing.T) {
		t.Parallel()
		servers := []mcp.ServerConfig{
			{
				Name: "burp",
				URL:  `{{.Default "BURP_URL" "http://localhost:9876"}}`,
			},
		}
		resolved, err := resolveMCPServerTemplates(servers, TemplateData{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved[0].URL != "http://localhost:9876" {
			t.Fatalf("URL = %q, want default http://localhost:9876", resolved[0].URL)
		}
	})

	t.Run("AgentDir resolves in command", func(t *testing.T) {
		t.Parallel()
		servers := []mcp.ServerConfig{
			{Name: "wrap", Command: `{{.AgentDir}}/wrapper.sh`},
		}
		resolved, err := resolveMCPServerTemplates(servers, TemplateData{AgentDir: "/abs/path/to/agent"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved[0].Command != "/abs/path/to/agent/wrapper.sh" {
			t.Fatalf("Command = %q, want /abs/path/to/agent/wrapper.sh", resolved[0].Command)
		}
	})

	t.Run("invalid template syntax", func(t *testing.T) {
		t.Parallel()
		servers := []mcp.ServerConfig{
			{Name: "bad", URL: "{{.Unclosed"},
		}
		_, err := resolveMCPServerTemplates(servers, TemplateData{})
		if err == nil {
			t.Fatalf("expected error for invalid template")
		}
		if !strings.Contains(err.Error(), "failed to parse MCP server template") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestResolveMCPServerTemplates_ExecuteError(t *testing.T) {
	t.Parallel()
	// {{.BadField}} will fail during Execute because BadField is not in TemplateData.
	// Use a method call that doesn't exist to trigger execute error.
	servers := []mcp.ServerConfig{
		{Name: "bad", URL: `{{call .BadMethod}}`},
	}
	_, err := resolveMCPServerTemplates(servers, TemplateData{})
	if err == nil {
		t.Fatalf("expected execute error")
	}
	if !strings.Contains(err.Error(), "failed to resolve MCP server template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMCPServerTemplates_CommandError(t *testing.T) {
	t.Parallel()
	servers := []mcp.ServerConfig{
		{Name: "bad", Command: `{{call .BadMethod}}`},
	}
	_, err := resolveMCPServerTemplates(servers, TemplateData{})
	if err == nil {
		t.Fatalf("expected error for bad command template")
	}
	if !strings.Contains(err.Error(), "failed to resolve MCP server template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMCPServerTemplates_ArgsError(t *testing.T) {
	t.Parallel()
	servers := []mcp.ServerConfig{
		{Name: "bad", Command: "echo", Args: []string{`{{call .BadMethod}}`}},
	}
	_, err := resolveMCPServerTemplates(servers, TemplateData{})
	if err == nil {
		t.Fatalf("expected error for bad args template")
	}
	if !strings.Contains(err.Error(), "failed to resolve MCP server template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMCPServerTemplates_EnvError(t *testing.T) {
	t.Parallel()
	servers := []mcp.ServerConfig{
		{Name: "bad", Command: "echo", Env: []string{`{{call .BadMethod}}`}},
	}
	_, err := resolveMCPServerTemplates(servers, TemplateData{})
	if err == nil {
		t.Fatalf("expected error for bad env template")
	}
	if !strings.Contains(err.Error(), "failed to resolve MCP server template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMCPServerTemplates_HeadersError(t *testing.T) {
	t.Parallel()
	servers := []mcp.ServerConfig{
		{Name: "bad", Headers: []string{`{{call .BadMethod}}`}},
	}
	_, err := resolveMCPServerTemplates(servers, TemplateData{})
	if err == nil {
		t.Fatalf("expected error for bad headers template")
	}
	if !strings.Contains(err.Error(), "failed to resolve MCP server template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveMCPServerTemplatesAllFields(t *testing.T) {
	t.Parallel()

	servers := []mcp.ServerConfig{
		{
			Name:    "burp",
			URL:     `{{.Default "BURP_URL" "http://localhost:9876"}}`,
			Headers: []string{`Authorization={{.Var "BURP_KEY"}}`},
		},
		{
			Name:    "chrome",
			Command: "npx",
			Args:    []string{"-y", `--wsEndpoint={{.Var "CHROME_WS"}}`},
			Env:     []string{`DEBUG={{.Default "DEBUG" "false"}}`},
		},
	}
	data := TemplateData{Vars: map[string]string{
		"BURP_URL":  "http://remote:8080",
		"BURP_KEY":  "secret123",
		"CHROME_WS": "ws://127.0.0.1:9222/devtools/browser/abc",
	}}
	resolved, err := resolveMCPServerTemplates(servers, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Burp SSE
	if resolved[0].URL != "http://remote:8080" {
		t.Fatalf("burp URL = %q, want http://remote:8080", resolved[0].URL)
	}
	if resolved[0].Headers[0] != "Authorization=secret123" {
		t.Fatalf("burp header = %q, want Authorization=secret123", resolved[0].Headers[0])
	}

	// Chrome stdio
	if resolved[1].Args[1] != "--wsEndpoint=ws://127.0.0.1:9222/devtools/browser/abc" {
		t.Fatalf("chrome arg = %q", resolved[1].Args[1])
	}
	if resolved[1].Env[0] != "DEBUG=false" {
		t.Fatalf("chrome env = %q, want DEBUG=false", resolved[1].Env[0])
	}
}

func TestLoadManifest_DisableTask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		want     bool
	}{
		{
			"disable_task true",
			"name: Demo\nversion: v1\nentrypoint: system.txt\nwrapper: wrapper.txt\ndisable_task: true\n",
			true,
		},
		{
			"disable_task false",
			"name: Demo\nversion: v1\nentrypoint: system.txt\nwrapper: wrapper.txt\ndisable_task: false\n",
			false,
		},
		{
			"disable_task omitted",
			"name: Demo\nversion: v1\nentrypoint: system.txt\nwrapper: wrapper.txt\n",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, _ := setupTestAgent(t, "demo", map[string]string{
				"agent.yaml":  tt.manifest,
				"system.txt":  "system",
				"wrapper.txt": "wrapper",
			})
			m, err := LoadManifest(filepath.Join(dir, "demo"))
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			if m.DisableTask != tt.want {
				t.Fatalf("DisableTask = %v, want %v", m.DisableTask, tt.want)
			}
		})
	}
}

func TestBuildBundle_ModelProviderOverride(t *testing.T) {
	t.Parallel()
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
models:
  - model: claude-haiku-4-5
    provider: anthropic
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.Model != "claude-haiku-4-5" {
		t.Fatalf("Model = %q, want claude-haiku-4-5", bundle.Model)
	}
	if bundle.Provider != "anthropic" {
		t.Fatalf("Provider = %q, want anthropic", bundle.Provider)
	}
}

func TestBuildBundle_ModelProviderEmpty(t *testing.T) {
	t.Parallel()
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

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.Model != "" {
		t.Fatalf("Model = %q, want empty", bundle.Model)
	}
	if bundle.Provider != "" {
		t.Fatalf("Provider = %q, want empty", bundle.Provider)
	}
}

func TestChildBudget_NamesOnly(t *testing.T) {
	t.Parallel()

	var config BudgetConfig
	yaml := `
estimated_iterations: 10
children:
  - name: go-review
  - name: go-tests
`
	if err := yamlPkg.Unmarshal([]byte(yaml), &config); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(config.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(config.Children))
	}
	if config.Children[0].Name != "go-review" || config.Children[0].MaxCost != 0 {
		t.Fatalf("Children[0] = %+v, want {Name:go-review MaxCost:0}", config.Children[0])
	}
	if config.Children[1].Name != "go-tests" || config.Children[1].MaxCost != 0 {
		t.Fatalf("Children[1] = %+v, want {Name:go-tests MaxCost:0}", config.Children[1])
	}
}

func TestChildBudget_UnmarshalYAML_Structured(t *testing.T) {
	t.Parallel()

	var config BudgetConfig
	yaml := `
estimated_iterations: 10
children:
  - name: go-review
    max_cost: 3.50
  - name: go-tests
    max_cost: 5.00
`
	if err := yamlPkg.Unmarshal([]byte(yaml), &config); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(config.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(config.Children))
	}
	if config.Children[0].Name != "go-review" || config.Children[0].MaxCost != 3.50 {
		t.Fatalf("Children[0] = %+v", config.Children[0])
	}
	if config.Children[1].Name != "go-tests" || config.Children[1].MaxCost != 5.00 {
		t.Fatalf("Children[1] = %+v", config.Children[1])
	}
}

func TestChildBudget_StructuredFormat(t *testing.T) {
	t.Parallel()

	var config BudgetConfig
	yaml := `
children:
  - name: go-review
  - name: go-tests
    max_cost: 5.00
`
	if err := yamlPkg.Unmarshal([]byte(yaml), &config); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(config.Children) != 2 {
		t.Fatalf("Children = %d, want 2", len(config.Children))
	}
	if config.Children[0].Name != "go-review" || config.Children[0].MaxCost != 0 {
		t.Fatalf("Children[0] = %+v", config.Children[0])
	}
	if config.Children[1].Name != "go-tests" || config.Children[1].MaxCost != 5.00 {
		t.Fatalf("Children[1] = %+v", config.Children[1])
	}
}

func TestBudgetConfig_ChildNames(t *testing.T) {
	t.Parallel()
	config := &BudgetConfig{
		Children: []ChildBudget{
			{Name: "agent-a"},
			{Name: "agent-b", MaxCost: 2.0},
		},
	}
	names := config.ChildNames()
	if len(names) != 2 || names[0] != "agent-a" || names[1] != "agent-b" {
		t.Fatalf("ChildNames() = %v", names)
	}
}

func TestBudgetConfig_ChildNames_Nil(t *testing.T) {
	t.Parallel()
	var config *BudgetConfig
	names := config.ChildNames()
	if names != nil {
		t.Fatalf("ChildNames() on nil = %v, want nil", names)
	}
}

func TestBudgetConfig_ChildMaxCost(t *testing.T) {
	t.Parallel()
	config := &BudgetConfig{
		Children: []ChildBudget{
			{Name: "cheap-agent", MaxCost: 0.50},
			{Name: "expensive-agent", MaxCost: 10.0},
			{Name: "default-agent"},
		},
	}
	if got := config.ChildMaxCost("cheap-agent"); got != 0.50 {
		t.Fatalf("ChildMaxCost(cheap-agent) = %f, want 0.50", got)
	}
	if got := config.ChildMaxCost("expensive-agent"); got != 10.0 {
		t.Fatalf("ChildMaxCost(expensive-agent) = %f, want 10.0", got)
	}
	if got := config.ChildMaxCost("default-agent"); got != 0 {
		t.Fatalf("ChildMaxCost(default-agent) = %f, want 0", got)
	}
	if got := config.ChildMaxCost("unknown-agent"); got != 0 {
		t.Fatalf("ChildMaxCost(unknown-agent) = %f, want 0", got)
	}
}

func TestBuildBundle_BudgetPropagated(t *testing.T) {
	t.Parallel()
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
budget:
  estimated_iterations: 10
  children:
    - name: go-review
      max_cost: 3.50
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.Budget == nil {
		t.Fatal("expected Budget to be set")
	}
	if bundle.Budget.ChildMaxCost("go-review") != 3.50 {
		t.Fatalf("ChildMaxCost(go-review) = %f, want 3.50", bundle.Budget.ChildMaxCost("go-review"))
	}
}

func TestBuildBundle_DisableTaskPropagated(t *testing.T) {
	t.Parallel()
	manifest := "name: Demo\nversion: v1\nentrypoint: system.txt\nwrapper: wrapper.txt\ndisable_task: true\n"
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if !bundle.DisableTask {
		t.Fatal("expected Bundle.DisableTask to be true")
	}
}

func TestBuildBundle_EditDeadlinePropagated(t *testing.T) {
	t.Parallel()
	manifest := "name: Demo\nversion: v1\nentrypoint: system.txt\nwrapper: wrapper.txt\nedit_deadline: 8\n"
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})
	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	if bundle.EditDeadline != 8 {
		t.Fatalf("expected Bundle.EditDeadline = 8, got %d", bundle.EditDeadline)
	}
}

func TestBuildBundle_ModelsListPropagated(t *testing.T) {
	t.Parallel()
	manifest := `name: Demo
version: v1
entrypoint: system.txt
wrapper: wrapper.txt
models:
  - model: gemini-2.5-flash
    provider: google
  - model: gpt-4.1-mini
    provider: openai
  - model: claude-sonnet-4-6
    provider: anthropic
`
	dir, _ := setupTestAgent(t, "demo", map[string]string{
		"agent.yaml":  manifest,
		"system.txt":  "system",
		"wrapper.txt": "wrapper",
	})

	bundle, err := BuildBundle(dir, "demo", "prompt", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	// Primary model/provider should be first entry
	if bundle.Model != "gemini-2.5-flash" {
		t.Fatalf("Model = %q, want gemini-2.5-flash", bundle.Model)
	}
	if bundle.Provider != "google" {
		t.Fatalf("Provider = %q, want google", bundle.Provider)
	}
	// Full list should be propagated
	if len(bundle.Models) != 3 {
		t.Fatalf("Models len = %d, want 3", len(bundle.Models))
	}
	if bundle.Models[1].Model != "gpt-4.1-mini" {
		t.Fatalf("Models[1].Model = %q, want gpt-4.1-mini", bundle.Models[1].Model)
	}
	if bundle.Models[2].Model != "claude-sonnet-4-6" {
		t.Fatalf("Models[2].Model = %q, want claude-sonnet-4-6", bundle.Models[2].Model)
	}
}

func TestBundle_FindModel(t *testing.T) {
	t.Parallel()
	b := &Bundle{
		Models: []ModelPreference{
			{Model: "gemini-2.5-flash", Provider: "google"},
			{Model: "gpt-4.1-mini", Provider: "openai"},
			{Model: "claude-sonnet-4-6", Provider: "anthropic"},
		},
	}

	tests := []struct {
		name     string
		provider string
		model    string
		wantNil  bool
	}{
		{"match first entry", "google", "gemini-2.5-flash", false},
		{"match middle entry", "openai", "gpt-4.1-mini", false},
		{"match last entry", "anthropic", "claude-sonnet-4-6", false},
		{"unknown provider", "azure", "gpt-4.1-mini", true},
		{"unknown model for known provider", "openai", "gpt-5", true},
		{"empty provider", "", "gpt-4.1-mini", true},
		{"empty model", "openai", "", true},
		{"both empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := b.FindModel(tt.provider, tt.model)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("FindModel(%q, %q) = %+v, want nil", tt.provider, tt.model, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("FindModel(%q, %q) = nil, want match", tt.provider, tt.model)
			}
			if got.Provider != tt.provider || got.Model != tt.model {
				t.Fatalf("FindModel(%q, %q) = {%q, %q}, want {%q, %q}",
					tt.provider, tt.model, got.Provider, got.Model, tt.provider, tt.model)
			}
		})
	}
}

func TestBundle_FindModel_EmptyModelsList(t *testing.T) {
	t.Parallel()
	b := &Bundle{}
	if got := b.FindModel("openai", "gpt-5"); got != nil {
		t.Fatalf("FindModel on empty bundle = %+v, want nil", got)
	}
}

func TestBuildBundle_InlinePromptOutputContractJSON(t *testing.T) {
	t.Parallel()
	manifest := `name: inline-json
version: 1
working_dir: none
prompt: "Emit a structured report."
output:
  format: json
  schema:
    type: object
    required:
      - status
    properties:
      status:
        type: string
`
	dir, _ := setupTestAgent(t, "inline-json", map[string]string{"agent.yaml": manifest})
	bundle, err := BuildBundle(dir, "inline-json", "go", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	assertContains(t, bundle.System, "Output Contract", "inline-json system")
	assertContains(t, bundle.System, "single JSON object", "inline-json system")
	assertContains(t, bundle.System, "JSON Schema", "inline-json system")
	assertContains(t, bundle.System, `"status"`, "inline-json system")
}

// TestBuildBundle_InlinePromptOutputJSONNoSchema covers the json-format branch
// when the schema map is empty — the contract is still emitted, but no schema
// block follows.
func TestBuildBundle_InlinePromptOutputJSONNoSchema(t *testing.T) {
	t.Parallel()
	manifest := `name: inline-json-bare
version: 1
working_dir: none
prompt: "Emit JSON."
output:
  format: json
`
	dir, _ := setupTestAgent(t, "inline-json-bare", map[string]string{"agent.yaml": manifest})
	bundle, err := BuildBundle(dir, "inline-json-bare", "", "/work", "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	assertContains(t, bundle.System, "Output Contract", "system")
	assertNotContains(t, bundle.System, "conform to this JSON Schema", "no schema block")
}

// TestBuildBundle_InlinePromptWithRepoSummary exercises the repo-summary
// branch in buildInlineSystemMessage by pointing working_dir at a populated
// directory and omitting working_dir: none from the manifest.
func TestBuildBundle_InlinePromptWithRepoSummary(t *testing.T) {
	t.Parallel()
	manifest := `name: inline-repo
version: 1
prompt: "Look around."
`
	dir, _ := setupTestAgent(t, "inline-repo", map[string]string{"agent.yaml": manifest})

	// Create a populated working dir so compactRepoSummary returns content.
	workDir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.md"} {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	bundle, err := BuildBundle(dir, "inline-repo", "", workDir, "", nil)
	if err != nil {
		t.Fatalf("BuildBundle: %v", err)
	}
	assertContains(t, bundle.System, "Repository Structure", "repo summary should be present")
	assertContains(t, bundle.System, workDir, "working dir line in repo summary")
}

func TestBuildBundleInline_BaseDirReferenceFallback(t *testing.T) {
	t.Parallel()
	// baseDir holds the shared reference; stage dir has its own system + wrapper.
	baseDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(baseDir, "shared-ref.md"), []byte("shared ref content"), 0o644); err != nil {
		t.Fatalf("write shared ref: %v", err)
	}
	stageDir := filepath.Join(baseDir, "stages", "scan")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	writeTestFiles(t, stageDir, map[string]string{
		"system.md":  "stage system",
		"wrapper.md": "stage wrapper",
	})

	cfg := &InlineAgentConfig{
		Name:       "scan",
		EntryPoint: "system.md",
		Wrapper:    "wrapper.md",
		References: []string{"shared-ref.md"},
	}
	bundle, err := BuildBundleInline(baseDir, cfg, "prompt", t.TempDir(), "", nil)
	if err != nil {
		t.Fatalf("BuildBundleInline: %v", err)
	}
	assertContains(t, bundle.System, "shared ref content", "fallback baseDir reference")
}

// TestBuildBundle_InlinePromptBadTemplate covers BuildBundle's error return
// when the inline prompt contains an invalid template expression.
func TestBuildBundle_InlinePromptBadTemplate(t *testing.T) {
	t.Parallel()
	manifest := `name: bad-tmpl
version: 1
working_dir: none
prompt: "Broken {{.Unclosed"
`
	dir, _ := setupTestAgent(t, "bad-tmpl", map[string]string{"agent.yaml": manifest})
	_, err := BuildBundle(dir, "bad-tmpl", "", "/work", "", nil)
	if err == nil {
		t.Fatal("expected error for invalid template in inline prompt")
	}
}

func TestBuildBundleInline_ReferenceMissingEverywhere(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()
	stageDir := filepath.Join(baseDir, "stages", "scan")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		t.Fatalf("mkdir stage: %v", err)
	}
	writeTestFiles(t, stageDir, map[string]string{
		"system.md":  "stage system",
		"wrapper.md": "stage wrapper",
	})
	cfg := &InlineAgentConfig{
		Name:       "scan",
		EntryPoint: "system.md",
		Wrapper:    "wrapper.md",
		References: []string{"missing.md"},
	}
	_, err := BuildBundleInline(baseDir, cfg, "p", t.TempDir(), "", nil)
	if err == nil {
		t.Fatal("expected error when reference missing in both stage and baseDir")
	}
}
