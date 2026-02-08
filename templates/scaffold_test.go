package templates

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testHelper provides common test utilities
type testHelper struct {
	t      *testing.T
	tmpDir string
	ctx    context.Context
}

func newTestHelper(t *testing.T) *testHelper {
	return &testHelper{
		t:      t,
		tmpDir: t.TempDir(),
		ctx:    context.Background(),
	}
}

func (h *testHelper) createAgent(name, lang string, force bool) error {
	return CreateAgent(h.ctx, CreateOptions{
		Name:      name,
		Lang:      lang,
		AgentsDir: h.tmpDir,
		Force:     force,
	})
}

func (h *testHelper) mustMkdir(path string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Join(h.tmpDir, path), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *testHelper) mustWriteFile(path, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.tmpDir, path), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *testHelper) readFile(path string) string {
	h.t.Helper()
	content, err := os.ReadFile(filepath.Join(h.tmpDir, path))
	if err != nil {
		h.t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(content)
}

func (h *testHelper) fileExists(path string) bool {
	_, err := os.Stat(filepath.Join(h.tmpDir, path))
	return err == nil
}

func TestIsValidAgentName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid simple", "myagent", true},
		{"valid with hyphen", "my-agent", true},
		{"valid with numbers", "agent123", true},
		{"valid complex", "go-review-v2", true},
		{"valid two chars", "ab", true},
		{"invalid uppercase", "MyAgent", false},
		{"invalid starts with number", "123agent", false},
		{"invalid starts with hyphen", "-agent", false},
		{"invalid ends with hyphen", "agent-", false},
		{"invalid underscore", "my_agent", false},
		{"invalid single char", "a", false},
		{"invalid empty", "", false},
		{"invalid spaces", "my agent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidAgentName(tt.input)
			if got != tt.want {
				t.Errorf("IsValidAgentName(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestToTitleCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "agent", "Agent"},
		{"hyphenated", "go-review", "Go Review"},
		{"multiple hyphens", "xss-security-audit", "Xss Security Audit"},
		{"with numbers", "agent-v2", "Agent V2"},
		{"single char segments", "a-b-c", "A B C"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToTitleCase(tt.input)
			if got != tt.want {
				t.Errorf("ToTitleCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateDescription(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		lang      string
		wantSub   string // substring to check
	}{
		{"go agent", "code-review", "go", "Go codebases"},
		{"python agent", "lint-check", "python", "Python codebases"},
		{"bash agent", "script-audit", "bash", "Bash scripts"},
		{"ansible agent", "playbook-test", "ansible", "Ansible playbooks"},
		{"generic agent", "custom-task", "generic", "Autonomous Custom Task agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generateDescription(tt.agentName, tt.lang)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("generateDescription(%q, %q) = %q, want to contain %q",
					tt.agentName, tt.lang, got, tt.wantSub)
			}
		})
	}
}

func TestCreateAgent(t *testing.T) {
	t.Run("creates agent directory structure", func(t *testing.T) {
		h := newTestHelper(t)
		if err := h.createAgent("test-agent", "go", false); err != nil {
			t.Fatalf("CreateAgent() error = %v", err)
		}
		expectedFiles := []string{"agent.yaml", "system.md", "agent.md", "task.md", "README.md", "references/test-agent-guide.md"}
		for _, f := range expectedFiles {
			if !h.fileExists("test-agent/" + f) {
				t.Errorf("expected file %s to exist", f)
			}
		}
	})

	t.Run("agent.yaml contains correct name", func(t *testing.T) {
		h := newTestHelper(t)
		if err := h.createAgent("my-agent", "python", false); err != nil {
			t.Fatalf("CreateAgent() error = %v", err)
		}
		content := h.readFile("my-agent/agent.yaml")
		if !strings.Contains(content, "name: my-agent") {
			t.Errorf("agent.yaml missing 'name: my-agent', got:\n%s", content)
		}
	})

	t.Run("rejects invalid agent name", func(t *testing.T) {
		h := newTestHelper(t)
		err := h.createAgent("Invalid-Name", "go", false)
		if err == nil || !strings.Contains(err.Error(), "invalid agent name") {
			t.Errorf("expected 'invalid agent name' error, got: %v", err)
		}
	})

	t.Run("rejects unknown language", func(t *testing.T) {
		h := newTestHelper(t)
		err := h.createAgent("test-agent", "rust", false)
		if err == nil || !strings.Contains(err.Error(), "unknown language") {
			t.Errorf("expected 'unknown language' error, got: %v", err)
		}
	})

	t.Run("fails without force when agent exists", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("existing-agent")
		err := h.createAgent("existing-agent", "go", false)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("expected 'already exists' error, got: %v", err)
		}
	})

	t.Run("overwrites with force flag", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("force-agent")
		if err := h.createAgent("force-agent", "go", true); err != nil {
			t.Fatalf("CreateAgent() with force error = %v", err)
		}
		if !h.fileExists("force-agent/agent.yaml") {
			t.Error("expected agent.yaml to exist after force create")
		}
	})
}

func TestCopyAgent(t *testing.T) {
	t.Run("copies agent successfully", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("source-agent/references")
		h.mustWriteFile("source-agent/agent.yaml", "name: source-agent\nversion: 1.0.0")
		h.mustWriteFile("source-agent/system.md", "# System")

		if err := CopyAgent(h.ctx, h.tmpDir, "source-agent", "dest-agent", false); err != nil {
			t.Fatalf("CopyAgent() error = %v", err)
		}
		if !h.fileExists("dest-agent") {
			t.Fatal("destination agent directory not created")
		}
		content := h.readFile("dest-agent/agent.yaml")
		if !strings.Contains(content, "name: dest-agent") {
			t.Errorf("manifest not updated, got:\n%s", content)
		}
	})

	t.Run("fails for nonexistent source", func(t *testing.T) {
		h := newTestHelper(t)
		err := CopyAgent(h.ctx, h.tmpDir, "nonexistent", "dest", false)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got: %v", err)
		}
	})

	t.Run("fails without force when destination exists", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("src")
		h.mustWriteFile("src/agent.yaml", "name: src")
		h.mustMkdir("dst")

		err := CopyAgent(h.ctx, h.tmpDir, "src", "dst", false)
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Errorf("expected 'already exists' error, got: %v", err)
		}
	})

	t.Run("rejects invalid destination name", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("src")
		h.mustWriteFile("src/agent.yaml", "name: src")

		err := CopyAgent(h.ctx, h.tmpDir, "src", "Invalid-Name", false)
		if err == nil || !strings.Contains(err.Error(), "invalid agent name") {
			t.Errorf("expected 'invalid agent name' error, got: %v", err)
		}
	})
}

func TestLangMaps(t *testing.T) {
	// Verify all languages have both verify commands and file patterns
	for lang := range LangVerifyCommands {
		if _, ok := LangFilePatterns[lang]; !ok {
			t.Errorf("language %q has verify command but no file pattern", lang)
		}
	}
	for lang := range LangFilePatterns {
		if _, ok := LangVerifyCommands[lang]; !ok {
			t.Errorf("language %q has file pattern but no verify command", lang)
		}
	}

	// Verify expected languages exist
	expectedLangs := []string{"go", "python", "bash", "ansible", "generic"}
	for _, lang := range expectedLangs {
		if _, ok := LangVerifyCommands[lang]; !ok {
			t.Errorf("expected language %q not found in LangVerifyCommands", lang)
		}
	}
}

func TestListTemplates(t *testing.T) {
	templates, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates() error = %v", err)
	}

	if len(templates) == 0 {
		t.Fatal("ListTemplates() returned no templates")
	}

	// Check that expected templates exist
	expectedTemplates := []string{
		"agent.yaml.tmpl",
		"system.md.tmpl",
		"agent.md.tmpl",
		"task.md.tmpl",
		"README.md.tmpl",
		"reference.md.tmpl",
	}

	for _, expected := range expectedTemplates {
		found := false
		for _, tmpl := range templates {
			if tmpl == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected template %q not found in ListTemplates() output", expected)
		}
	}

	// Verify all returned files end with .tmpl
	for _, tmpl := range templates {
		if !strings.HasSuffix(tmpl, ".tmpl") {
			t.Errorf("template %q doesn't end with .tmpl", tmpl)
		}
	}
}

func TestRender(t *testing.T) {
	t.Run("renders valid template", func(t *testing.T) {
		data := AgentData{
			Name:        "test-agent",
			NameTitle:   "Test Agent",
			Description: "A test agent",
			Lang:        "go",
			Version:     "1.0.0",
		}

		content, err := Render("agent.yaml.tmpl", data)
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		if !strings.Contains(content, "name: test-agent") {
			t.Error("rendered content missing agent name")
		}
		if !strings.Contains(content, "1.0.0") {
			t.Error("rendered content missing version")
		}
	})

	t.Run("returns error for nonexistent template", func(t *testing.T) {
		data := AgentData{Name: "test"}

		_, err := Render("nonexistent.tmpl", data)
		if err == nil {
			t.Fatal("expected error for nonexistent template, got nil")
		}
		if !strings.Contains(err.Error(), "failed to read template") {
			t.Errorf("error = %q, want to contain 'failed to read template'", err.Error())
		}
	})

	t.Run("uses template functions", func(t *testing.T) {
		data := AgentData{
			Name:        "my-agent",
			NameTitle:   "My Agent",
			Description: "Test",
			Lang:        "go",
			Version:     "1.0.0",
		}

		// Test that the template functions are available by rendering a template
		content, err := Render("README.md.tmpl", data)
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}

		// README should contain the title-cased name
		if !strings.Contains(content, "My Agent") {
			t.Error("rendered README missing title-cased name")
		}
	})
}

func TestCopyAgentWithForce(t *testing.T) {
	t.Run("overwrites existing destination with force", func(t *testing.T) {
		h := newTestHelper(t)
		h.mustMkdir("src/references")
		h.mustWriteFile("src/agent.yaml", "name: src\nversion: 1.0.0")
		h.mustWriteFile("src/system.md", "# Source System")
		h.mustMkdir("dst")
		h.mustWriteFile("dst/old-file.txt", "old content")

		if err := CopyAgent(h.ctx, h.tmpDir, "src", "dst", true); err != nil {
			t.Fatalf("CopyAgent() with force error = %v", err)
		}
		if h.fileExists("dst/old-file.txt") {
			t.Error("old file should have been removed")
		}
		content := h.readFile("dst/agent.yaml")
		if !strings.Contains(content, "name: dst") {
			t.Errorf("manifest not updated correctly, got:\n%s", content)
		}
		sysContent := h.readFile("dst/system.md")
		if !strings.Contains(sysContent, "# Source System") {
			t.Error("system.md content not copied correctly")
		}
	})
}

func TestToTitleCaseEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"empty segments", "a--b", "A  B"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToTitleCase(tt.input)
			if got != tt.want {
				t.Errorf("ToTitleCase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
