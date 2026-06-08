package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequiresValidate_NilOK(t *testing.T) {
	var r *RequiresConfig
	if err := r.Validate("any"); err != nil {
		t.Fatalf("nil RequiresConfig should validate, got: %v", err)
	}
}

func TestRequiresValidate_EmptyOK(t *testing.T) {
	r := &RequiresConfig{}
	if err := r.Validate("any"); err != nil {
		t.Fatalf("empty RequiresConfig should validate, got: %v", err)
	}
}

func TestRequiresValidate_RejectsMissingName(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{{Name: ""}}}
	err := r.Validate("go-security-audit")
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("expected missing-name error, got: %v", err)
	}
}

func TestRequiresValidate_RejectsPathInName(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{{Name: "/usr/bin/gosec"}}}
	err := r.Validate("a")
	if err == nil || !strings.Contains(err.Error(), "bare binary name") {
		t.Fatalf("expected bare-name error, got: %v", err)
	}
}

func TestRequiresValidate_RejectsDuplicates(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{Name: "gosec"},
		{Name: "gosec"},
	}}
	err := r.Validate("a")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got: %v", err)
	}
}

func TestRequiresPreflight_NilOK(t *testing.T) {
	var r *RequiresConfig
	if err := r.Preflight(); err != nil {
		t.Fatalf("nil RequiresConfig should preflight clean, got: %v", err)
	}
}

func TestRequiresPreflight_EmptyOK(t *testing.T) {
	r := &RequiresConfig{}
	if err := r.Preflight(); err != nil {
		t.Fatalf("empty RequiresConfig should preflight clean, got: %v", err)
	}
}

// TestRequiresPreflight_PresentOK uses a tool that exists on every platform
// (go itself, since the test runs under `go test`).
func TestRequiresPreflight_PresentOK(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{{Name: "go"}}}
	if err := r.Preflight(); err != nil {
		t.Fatalf("preflight for 'go' should pass under `go test`, got: %v", err)
	}
}

func TestRequiresPreflight_MissingFails(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{
			Name:    "definitely-not-a-real-binary-xyz123",
			Install: map[string]string{"brew": "fake-tool", "pipx": "fake-tool"},
		},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail for missing tool")
	}
	msg := err.Error()
	for _, want := range []string{
		"preflight",
		"1 required tool missing",
		"definitely-not-a-real-binary-xyz123",
		"brew install fake-tool",
		"pipx install fake-tool",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("preflight error missing %q\nfull error:\n%s", want, msg)
		}
	}
}

func TestRequiresPreflight_MultipleMissingPluralizes(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{Name: "definitely-not-a-real-binary-xyz123"},
		{Name: "also-not-a-real-binary-xyz123"},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail")
	}
	if !strings.Contains(err.Error(), "2 required tools missing") {
		t.Fatalf("expected plural 'tools', got: %s", err.Error())
	}
}

func TestRequiresPreflight_PartialMissingOnlyReportsMissing(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{Name: "go"},
		{Name: "definitely-not-a-real-binary-xyz123"},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail")
	}
	if strings.Contains(err.Error(), "\n  go\n") {
		t.Errorf("present tool 'go' should not appear in missing report:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz123") {
		t.Errorf("missing tool not reported:\n%s", err.Error())
	}
}

func TestRequiresPreflight_NoInstallHint(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{Name: "definitely-not-a-real-binary-xyz123"},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail")
	}
	if !strings.Contains(err.Error(), "(no hint provided)") {
		t.Fatalf("expected placeholder hint, got: %s", err.Error())
	}
}

func TestRequiresPreflight_InstallHintOrdering(t *testing.T) {
	// brew should appear before pipx (priority 0 vs 1), pipx before url.
	r := &RequiresConfig{Commands: []RequiredCommand{
		{
			Name: "definitely-not-a-real-binary-xyz123",
			Install: map[string]string{
				"url":  "https://example.test",
				"pipx": "foo",
				"brew": "foo",
			},
		},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail")
	}
	msg := err.Error()
	brewIdx := strings.Index(msg, "brew install foo")
	pipxIdx := strings.Index(msg, "pipx install foo")
	urlIdx := strings.Index(msg, "https://example.test")
	if brewIdx < 0 || pipxIdx <= brewIdx || urlIdx <= pipxIdx {
		t.Fatalf("expected install hint order brew < pipx < url, got:\n%s", msg)
	}
}

func TestRequiresPreflight_UnknownManagerKeyPassedThrough(t *testing.T) {
	r := &RequiresConfig{Commands: []RequiredCommand{
		{
			Name:    "definitely-not-a-real-binary-xyz123",
			Install: map[string]string{"snap": "fake-tool"},
		},
	}}
	err := r.Preflight()
	if err == nil {
		t.Fatal("expected preflight to fail")
	}
	if !strings.Contains(err.Error(), "snap: fake-tool") {
		t.Fatalf("expected unknown manager 'snap' to pass through verbatim, got:\n%s", err.Error())
	}
}

// TestManifestValidate_InvokesRequires ensures the top-level Validate()
// surfaces RequiresConfig errors so bad manifests fail at LoadManifest time.
func TestManifestValidate_InvokesRequires(t *testing.T) {
	m := &Manifest{
		Name:       "leaf",
		EntryPoint: "system.md",
		Wrapper:    "agent.md",
		Requires: &RequiresConfig{Commands: []RequiredCommand{
			{Name: ""},
		}},
	}
	err := m.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires.commands") {
		t.Fatalf("expected Validate to surface requires error, got: %v", err)
	}
}

// TestLoadManifest_ParsesRequires writes a minimal agent.yaml with a
// requires block and confirms LoadManifest round-trips it.
func TestLoadManifest_ParsesRequires(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: test-agent
version: 0.1.0
entrypoint: system.md
wrapper: agent.md
requires:
  commands:
    - name: gosec
      install:
        brew: gosec
        go: github.com/securego/gosec/v2/cmd/gosec@latest
    - name: govulncheck
      install:
        go: golang.org/x/vuln/cmd/govulncheck@latest
`
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Requires == nil || len(m.Requires.Commands) != 2 {
		t.Fatalf("expected 2 commands, got: %+v", m.Requires)
	}
	if m.Requires.Commands[0].Name != "gosec" {
		t.Errorf("expected first command 'gosec', got %q", m.Requires.Commands[0].Name)
	}
	if got := m.Requires.Commands[0].Install["brew"]; got != "gosec" {
		t.Errorf("expected brew install hint 'gosec', got %q", got)
	}
}
