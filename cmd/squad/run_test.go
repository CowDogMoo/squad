/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
)

// setupTestAgent creates a temporary agent directory with the given manifest
// YAML and supporting files. Returns the agents dir path.
func setupTestAgent(t *testing.T, name, manifestYAML string, files map[string]string) string {
	t.Helper()
	agentsDir := t.TempDir()
	agentDir := filepath.Join(agentsDir, name)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	for relPath, content := range files {
		full := filepath.Join(agentDir, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return agentsDir
}

func TestBuildAgentBundle_DefaultMode(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
references:
  - refs/criteria.md
modes:
  readonly:
    entrypoint: system-ro.md
    wrapper: agent-ro.md
`
	files := map[string]string{
		"system.md":        "default system prompt",
		"agent.md":         "default wrapper",
		"system-ro.md":     "readonly system prompt",
		"agent-ro.md":      "readonly wrapper",
		"refs/criteria.md": "review criteria content",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "default system prompt") {
		t.Error("expected default system prompt in bundle")
	}
	if !strings.Contains(bundle.System, "default wrapper") {
		t.Error("expected default wrapper in bundle")
	}
	if strings.Contains(bundle.System, "readonly system prompt") {
		t.Error("did not expect readonly system prompt in default mode")
	}
	if !strings.Contains(bundle.System, "review criteria content") {
		t.Error("expected references in bundle")
	}
	if !strings.Contains(string(bundle.Combined), "review this") {
		t.Error("expected user prompt in combined bundle")
	}
}

func TestBuildAgentBundle_ReadonlyMode(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
references:
  - refs/criteria.md
modes:
  readonly:
    entrypoint: system-ro.md
    wrapper: agent-ro.md
`
	files := map[string]string{
		"system.md":        "default system prompt",
		"agent.md":         "default wrapper",
		"system-ro.md":     "readonly system prompt",
		"agent-ro.md":      "readonly wrapper",
		"refs/criteria.md": "review criteria content",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "review this", "/tmp", "readonly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "readonly system prompt") {
		t.Error("expected readonly system prompt in bundle")
	}
	if !strings.Contains(bundle.System, "readonly wrapper") {
		t.Error("expected readonly wrapper in bundle")
	}
	if strings.Contains(bundle.System, "default system prompt") {
		t.Error("did not expect default system prompt in readonly mode")
	}
	// References should still be inherited from defaults.
	if !strings.Contains(bundle.System, "review criteria content") {
		t.Error("expected default references to be inherited in readonly mode")
	}
}

func TestBuildAgentBundle_ModeOverridesReferences(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
references:
  - refs/default-ref.md
modes:
  custom:
    references:
      - refs/custom-ref.md
`
	files := map[string]string{
		"system.md":           "system prompt",
		"agent.md":            "wrapper",
		"refs/default-ref.md": "default reference",
		"refs/custom-ref.md":  "custom reference",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "custom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "custom reference") {
		t.Error("expected custom reference in bundle")
	}
	if strings.Contains(bundle.System, "default reference") {
		t.Error("did not expect default reference when mode overrides references")
	}
}

func TestBuildAgentBundle_NonexistentMode(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
modes:
  readonly:
    entrypoint: system-ro.md
`
	files := map[string]string{
		"system.md":    "system prompt",
		"agent.md":     "wrapper",
		"system-ro.md": "readonly system prompt",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	_, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent mode")
	}
	if !strings.Contains(err.Error(), `has no mode "nonexistent"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildAgentBundle_NoModesField(t *testing.T) {
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
`
	files := map[string]string{
		"system.md": "system prompt",
		"agent.md":  "wrapper",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	_, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "anything")
	if err == nil {
		t.Fatal("expected error when requesting mode on agent with no modes")
	}
	if !strings.Contains(err.Error(), `has no mode "anything"`) {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildAgentBundle_PartialOverride(t *testing.T) {
	// Mode only overrides entrypoint, wrapper stays default.
	manifest := `
name: test-agent
version: "1.0"
entrypoint: system.md
wrapper: agent.md
modes:
  lite:
    entrypoint: system-lite.md
`
	files := map[string]string{
		"system.md":      "default system",
		"agent.md":       "default wrapper",
		"system-lite.md": "lite system",
	}
	agentsDir := setupTestAgent(t, "test-agent", manifest, files)

	bundle, err := agent.BuildBundle(agentsDir, "test-agent", "test", "/tmp", "lite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(bundle.System, "lite system") {
		t.Error("expected lite system prompt")
	}
	if !strings.Contains(bundle.System, "default wrapper") {
		t.Error("expected default wrapper to be preserved")
	}
}
