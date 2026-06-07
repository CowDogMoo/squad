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
