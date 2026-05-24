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

func TestTemplateEnvReadsOSEnv(t *testing.T) {
	t.Setenv("SQUAD_TEMPLATE_ENV_TEST", "hello")
	out, err := processTemplate("inline", `value={{.Env "SQUAD_TEMPLATE_ENV_TEST"}}`, t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	if out != "value=hello" {
		t.Fatalf("got %q, want %q", out, "value=hello")
	}
}

func TestTemplateEnvFallback(t *testing.T) {
	t.Setenv("SQUAD_TEMPLATE_ENV_UNSET", "")
	out, err := processTemplate("inline",
		`value={{.Env "SQUAD_TEMPLATE_ENV_UNSET" "fallback"}}`,
		t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	if out != "value=fallback" {
		t.Fatalf("got %q, want %q", out, "value=fallback")
	}
}

func TestTemplateEnvEmptyWhenUnsetAndNoFallback(t *testing.T) {
	t.Setenv("SQUAD_TEMPLATE_ENV_BARE", "")
	out, err := processTemplate("inline",
		`value={{.Env "SQUAD_TEMPLATE_ENV_BARE"}}`,
		t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	if out != "value=" {
		t.Fatalf("got %q, want %q", out, "value=")
	}
}

func TestTemplateNowWithLayout(t *testing.T) {
	out, err := processTemplate("inline", `d={{now "2006"}}`, t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	// 4-digit year — covers the explicit-layout branch.
	if len(out) != len("d=YYYY") {
		t.Fatalf("got %q, want 6-char d=YYYY format", out)
	}
}

func TestTemplateNowDefaultLayout(t *testing.T) {
	// No layout argument → RFC3339 fallback branch.
	out, err := processTemplate("inline", `d={{now}}`, t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	// RFC3339 is "2006-01-02T15:04:05Z07:00" — at minimum contains a 'T'.
	if !strings.Contains(out, "T") {
		t.Fatalf("expected RFC3339 output containing 'T', got %q", out)
	}
}

func TestTemplateToday(t *testing.T) {
	out, err := processTemplate("inline", `d={{today}}`, t.TempDir(), TemplateData{})
	if err != nil {
		t.Fatalf("processTemplate: %v", err)
	}
	got := strings.TrimPrefix(out, "d=")
	weekdays := []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
	matched := false
	for _, day := range weekdays {
		if strings.HasPrefix(got, day+" ") {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatalf("expected output to start with a weekday name, got %q", out)
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
