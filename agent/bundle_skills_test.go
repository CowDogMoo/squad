package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/skill"
)

// writeSkill creates <root>/<name>/SKILL.md with the given metadata + body.
func writeSkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, skill.FileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupSkillFixture builds a leaf agent + a working dir with two repo-scoped
// skills under <workingDir>/.squad/skills/. Returns the agents directory (parent
// of the agent), the working dir, and the agent name. XDG is pointed at a
// throwaway location so the developer's global skills don't leak in.
func setupSkillFixture(t *testing.T) (agentsDir, workingDir, agentName string) {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdg, ".config"))
	t.Setenv("HOME", xdg)

	files := map[string]string{
		"agent.yaml":  "name: demo\nversion: 1\nentrypoint: system.txt\nwrapper: wrapper.txt\n",
		"system.txt":  "Be concise.",
		"wrapper.txt": "You are a demo agent.",
	}
	agentsDir, _ = setupTestAgent(t, "demo", files)
	workingDir = t.TempDir()
	repoSkills := skill.RepoSkillsDir(workingDir)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "alpha", "Adds groceries to the cart.", "alpha body content — should NOT leak")
	writeSkill(t, repoSkills, "beta", "Resolves recipe ambiguities.", "beta body content — should NOT leak")

	return agentsDir, workingDir, "demo"
}

func TestBuildBundle_SkillBlockInjected(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)

	bundle, err := BuildBundle(agentsDir, name, "", workingDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(bundle.System, skill.PromptBlockHeader) {
		t.Fatalf("missing skill block header in system prompt:\n%s", bundle.System)
	}
	for _, want := range []string{
		"- **alpha**: Adds groceries to the cart.",
		"- **beta**: Resolves recipe ambiguities.",
	} {
		if !strings.Contains(bundle.System, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	for _, leak := range []string{"alpha body content", "beta body content"} {
		if strings.Contains(bundle.System, leak) {
			t.Errorf("skill body leaked into system prompt: %q", leak)
		}
	}
}

func TestBuildBundle_SkillBlockDeterministic(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)
	first, err := BuildBundle(agentsDir, name, "", workingDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildBundle(agentsDir, name, "", workingDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.System != second.System {
		t.Errorf("system prompt is not deterministic across calls")
	}
}

func TestBuildBundle_NoSkillsNoBlock(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdg, ".config"))
	t.Setenv("HOME", xdg)

	files := map[string]string{
		"agent.yaml":  "name: demo\nversion: 1\nentrypoint: system.txt\nwrapper: wrapper.txt\n",
		"system.txt":  "Be concise.",
		"wrapper.txt": "You are a demo agent.",
	}
	agentsDir, _ := setupTestAgent(t, "demo", files)
	bundle, err := BuildBundle(agentsDir, "demo", "", t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle.System, skill.PromptBlockHeader) {
		t.Errorf("empty catalog should produce no block, got:\n%s", bundle.System)
	}
}

func TestBuildBundle_SkillsDisabledInManifest(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdg, ".config"))
	t.Setenv("HOME", xdg)

	files := map[string]string{
		"agent.yaml": `name: demo
version: 1
entrypoint: system.txt
wrapper: wrapper.txt
skills:
  enabled: false
`,
		"system.txt":  "Be concise.",
		"wrapper.txt": "You are a demo agent.",
	}
	agentsDir, _ := setupTestAgent(t, "demo", files)
	workingDir := t.TempDir()
	repoSkills := skill.RepoSkillsDir(workingDir)
	if err := os.MkdirAll(repoSkills, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, repoSkills, "alpha", "Should be hidden.", "body")

	bundle, err := BuildBundle(agentsDir, "demo", "", workingDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle.System, skill.PromptBlockHeader) {
		t.Errorf("manifest-disabled skills should produce no block, got:\n%s", bundle.System)
	}
}

func TestBuildBundle_SkillOverrideDisable(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)
	disabled := false
	bundle, err := BuildBundleWithOptions(agentsDir, name, "", workingDir, "", nil, &BundleOptions{
		SkillOverrides: &SkillOverrides{Enabled: &disabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bundle.System, skill.PromptBlockHeader) {
		t.Errorf("--skills-disabled override should suppress block")
	}
}

func TestBuildBundle_SkillOverrideAllow(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)
	bundle, err := BuildBundleWithOptions(agentsDir, name, "", workingDir, "", nil, &BundleOptions{
		SkillOverrides: &SkillOverrides{Allow: []string{"alpha"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.System, "- **alpha**:") {
		t.Errorf("allow-listed alpha should appear")
	}
	if strings.Contains(bundle.System, "- **beta**:") {
		t.Errorf("non-allowed beta should not appear")
	}
}

func TestBuildBundle_SkillOverrideDeny(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)
	bundle, err := BuildBundleWithOptions(agentsDir, name, "", workingDir, "", nil, &BundleOptions{
		SkillOverrides: &SkillOverrides{Deny: []string{"beta"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.System, "- **alpha**:") {
		t.Errorf("non-denied alpha should appear")
	}
	if strings.Contains(bundle.System, "- **beta**:") {
		t.Errorf("denied beta should not appear")
	}
}

func TestBuildBundle_SkillBlockSize(t *testing.T) {
	agentsDir, workingDir, name := setupSkillFixture(t)
	bundle, err := BuildBundle(agentsDir, name, "", workingDir, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Extract the block from the system prompt and ensure it's bounded.
	idx := strings.Index(bundle.System, skill.PromptBlockHeader)
	if idx < 0 {
		t.Fatal("block header not found")
	}
	// Per PLAN.md: bench failure if a skill costs more than 150 tok ≈ ~600 chars.
	// We have 2 short skills, so total chars (header + intro + 2 bullets) should
	// stay well under 1000.
	rest := bundle.System[idx:]
	end := strings.Index(rest, "\n## ") // next markdown section
	if end < 0 {
		end = len(rest)
	}
	blockLen := end
	if blockLen > 1000 {
		t.Errorf("skill block too verbose: %d chars\n%s", blockLen, rest[:end])
	}
}
