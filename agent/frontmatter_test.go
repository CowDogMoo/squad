package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripPromptFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantBody string
		wantOK   bool
	}{
		{
			name:     "frontmatter stripped",
			content:  "---\nname: rust-review\nmodel: opus\n---\n# IDENTITY\n\nBody.\n",
			wantBody: "# IDENTITY\n\nBody.\n",
			wantOK:   true,
		},
		{
			name:     "no frontmatter",
			content:  "# IDENTITY\n\nBody.\n",
			wantBody: "# IDENTITY\n\nBody.\n",
			wantOK:   false,
		},
		{
			name:     "unterminated frontmatter falls back",
			content:  "---\nname: broken\nno closing fence\n",
			wantBody: "---\nname: broken\nno closing fence\n",
			wantOK:   false,
		},
		{
			name:     "crlf fences",
			content:  "---\r\nname: x\r\n---\r\nBody.\n",
			wantBody: "Body.\n",
			wantOK:   true,
		},
		{
			name:     "horizontal rule mid-document is not a fence",
			content:  "Intro\n---\nmore\n---\nend\n",
			wantBody: "Intro\n---\nmore\n---\nend\n",
			wantOK:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := stripPromptFrontmatter(tt.content)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantBody {
				t.Fatalf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

// writePromptAgent lays out a minimal agent dir with the given prompt files.
func writePromptAgent(t *testing.T, system, wrapper, task string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"system.md": system,
		"agent.md":  wrapper,
		"task.md":   task,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// TestLoadAndProcessPromptsClaudeNative verifies that a frontmatter-marked
// entrypoint disables template rendering for every prompt file of the agent:
// the frontmatter is dropped and literal {{...}} text (e.g. Taskfile
// examples) survives verbatim in system, wrapper, and task alike.
func TestLoadAndProcessPromptsClaudeNative(t *testing.T) {
	system := "---\nname: go-taskfile\ntools: \"Bash, Read\"\n---\n" +
		"# IDENTITY\n\nQuote the value: `VAR: '{{.OTHER_VAR}}'`.\n"
	wrapper := "# AGENT MODE\n\nNever template-ify `{{.CMD_PATH}}` literals.\n"
	task := "Fix the Taskfile. Leave `{{.VAR}}` examples alone.\n"
	dir := writePromptAgent(t, system, wrapper, task)

	manifest := &Manifest{EntryPoint: "system.md", Wrapper: "agent.md", Task: "task.md"}
	data := TemplateData{Mode: "readonly", AgentDir: dir}

	gotSystem, gotWrapper, gotTask, err := loadAndProcessPrompts(dir, dir, manifest, data)
	if err != nil {
		t.Fatalf("loadAndProcessPrompts: %v", err)
	}
	if strings.Contains(gotSystem, "name: go-taskfile") {
		t.Errorf("frontmatter not stripped from system prompt: %q", gotSystem)
	}
	if !strings.HasPrefix(gotSystem, "# IDENTITY") {
		t.Errorf("system body should start at first heading, got %q", gotSystem)
	}
	if !strings.Contains(gotSystem, "{{.OTHER_VAR}}") {
		t.Errorf("literal template text mangled in system prompt: %q", gotSystem)
	}
	if !strings.Contains(gotWrapper, "{{.CMD_PATH}}") {
		t.Errorf("literal template text mangled in wrapper: %q", gotWrapper)
	}
	if !strings.Contains(gotTask, "{{.VAR}}") {
		t.Errorf("literal template text mangled in task: %q", gotTask)
	}
}

// TestLoadAndProcessPromptsLegacyTemplates verifies the pre-frontmatter
// format still renders mode conditionals and includes as before.
func TestLoadAndProcessPromptsLegacyTemplates(t *testing.T) {
	system := "# IDENTITY\n{{if eq .Mode \"edit\"}}EDIT-ONLY{{end}}{{if eq .Mode \"readonly\"}}READONLY-ONLY{{end}}\n"
	wrapper := "# AGENT MODE\nMode is {{.Mode}}.\n"
	task := "Do the {{.Mode}} thing.\n"
	dir := writePromptAgent(t, system, wrapper, task)

	manifest := &Manifest{EntryPoint: "system.md", Wrapper: "agent.md", Task: "task.md"}
	data := TemplateData{Mode: "readonly", AgentDir: dir}

	gotSystem, gotWrapper, gotTask, err := loadAndProcessPrompts(dir, dir, manifest, data)
	if err != nil {
		t.Fatalf("loadAndProcessPrompts: %v", err)
	}
	if strings.Contains(gotSystem, "EDIT-ONLY") || !strings.Contains(gotSystem, "READONLY-ONLY") {
		t.Errorf("mode conditionals not rendered: %q", gotSystem)
	}
	if !strings.Contains(gotWrapper, "Mode is readonly.") {
		t.Errorf("wrapper template not rendered: %q", gotWrapper)
	}
	if !strings.Contains(gotTask, "Do the readonly thing.") {
		t.Errorf("task template not rendered: %q", gotTask)
	}
}
