package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
)

// TestTemplateBrowserProfile exercises the {{.BrowserProfile "name"}}
// helper end-to-end through processTemplate, since that's the path
// agent.yaml inline prompts and stdio args go through.
func TestTemplateBrowserProfile(t *testing.T) {
	// Redirect the profile root to a per-test temp dir so we don't
	// touch real ~/.local/share/squad.
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	wantRoot := filepath.Join(tmp, "squad", "browser-profiles")

	out, err := processTemplate("inline", `path={{.BrowserProfile "amazon"}}`, t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	wantSuffix := filepath.Join(wantRoot, "amazon")
	if !strings.HasSuffix(out, wantSuffix) {
		t.Fatalf("output %q does not end with %q", out, wantSuffix)
	}
	// Side effect: dir created on disk.
	if info, err := os.Stat(wantSuffix); err != nil || !info.IsDir() {
		t.Fatalf("template helper should create profile dir at %s: %v", wantSuffix, err)
	}
}

func TestTemplateBrowserProfileRejectsInvalid(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	// text/template wraps the helper's returned error in its own; the
	// underlying message must propagate so the user sees what's wrong.
	tmpl, err := template.New("t").Funcs(template.FuncMap{}).Parse(`{{.BrowserProfile "Bad Name"}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sb strings.Builder
	err = tmpl.Execute(&sb, TemplateData{})
	if err == nil {
		t.Fatal("expected error for invalid profile name, got nil")
	}
	if !strings.Contains(err.Error(), "browser profile name") {
		t.Fatalf("error %q should mention the validation rule", err.Error())
	}
}
