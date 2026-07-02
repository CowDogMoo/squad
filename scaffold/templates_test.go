package scaffold

import (
	"strings"
	"testing"
)

func TestRenderAndListTemplates(t *testing.T) {
	t.Parallel()

	// List should include several known templates.
	names, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(names) == 0 {
		t.Fatalf("ListTemplates returned no entries")
	}
	has := func(want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}
	if !has("agent.yaml.tmpl") || !has("system.md.tmpl") || !has("task.md.tmpl") {
		t.Fatalf("ListTemplates missing expected names: %v", names)
	}

	// Render a known template and assert key substitutions appear.
	out, err := Render("agent.yaml.tmpl", AgentData{
		Name:        "demo-agent",
		NameTitle:   "Demo Agent",
		Description: "An example agent used in tests",
		Lang:        "go",
		Version:     "0.0.1",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Spot-check a few fields substituted from AgentData and helper funcs.
	if !strings.Contains(out, "name: demo-agent") {
		t.Fatalf("rendered output missing agent name: \n%s", out)
	}
	if !strings.Contains(out, "version: 0.0.1") {
		t.Fatalf("rendered output missing version: \n%s", out)
	}
	if !strings.Contains(out, "references/demo-agent-guide.md") {
		t.Fatalf("rendered output missing references path: \n%s", out)
	}
}

// TestRenderClaudeNativeOutput asserts the scaffolded prompt files use the
// Claude-native format: system.md opens with YAML frontmatter carrying the
// agent name, and no rendered prompt file contains template syntax (the
// runner delivers frontmatter-marked agents verbatim, so a leftover `{{`
// would reach the model as-is — or, under a legacy templated agent, get
// rendered into `<no value>`).
func TestRenderClaudeNativeOutput(t *testing.T) {
	t.Parallel()

	data := AgentData{
		Name:        "demo-agent",
		NameTitle:   "Demo Agent",
		Description: "An example agent used in tests",
		Lang:        "go",
		Version:     "0.0.1",
	}

	system, err := Render("system.md.tmpl", data)
	if err != nil {
		t.Fatalf("Render system.md.tmpl: %v", err)
	}
	if !strings.HasPrefix(system, "---\n") {
		t.Fatalf("system.md must open with a YAML frontmatter fence, got %q", system[:40])
	}
	if !strings.Contains(system, "name: demo-agent") {
		t.Fatalf("system.md frontmatter missing agent name:\n%s", system[:200])
	}

	for _, name := range []string{"system.md.tmpl", "agent.md.tmpl", "task.md.tmpl"} {
		out, err := Render(name, data)
		if err != nil {
			t.Fatalf("Render %s: %v", name, err)
		}
		if strings.Contains(out, "{{") {
			t.Fatalf("%s rendered output contains template syntax:\n%s", name, out)
		}
	}
}

func TestRender_TemplateNotFound(t *testing.T) {
	t.Parallel()
	_, err := Render("nonexistent.tmpl", AgentData{Name: "x", NameTitle: "X"})
	if err == nil {
		t.Fatal("expected error for missing template, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("expected 'failed to read template' in error, got %v", err)
	}
}
