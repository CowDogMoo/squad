package skill

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple lower", "grocery", false},
		{"with hyphen", "grocery-add-to-cart", false},
		{"with digits", "skill-v2", false},
		{"single char", "x", false},

		{"empty", "", true},
		{"uppercase", "Grocery", true},
		{"underscore", "grocery_add", true},
		{"leading hyphen", "-grocery", true},
		{"trailing hyphen", "grocery-", true},
		{"too long", strings.Repeat("a", 65), true},
		{"contains anthropic", "my-anthropic-skill", true},
		{"contains claude", "claude-helper", true},
		{"contains uppercase reserved", "my-Anthropic", true},
		{"contains lt", "skill<x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateName(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

func TestValidateDescription(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "Adds groceries to cart.", false},
		{"max-1", strings.Repeat("a", MaxDescriptionLen), false},

		{"empty", "", true},
		{"too long", strings.Repeat("a", MaxDescriptionLen+1), true},
		{"contains lt", "uses <x>", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDescription(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
		})
	}
}

func TestParseManifestHappyPath(t *testing.T) {
	raw := []byte("---\nname: grocery\ndescription: Add groceries to cart.\n---\n# Body\n\nDo the thing.\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "grocery" {
		t.Errorf("name = %q, want grocery", m.Name)
	}
	if m.Description != "Add groceries to cart." {
		t.Errorf("description = %q", m.Description)
	}
	if !strings.HasPrefix(m.Body, "# Body") {
		t.Errorf("body should start with '# Body', got %q", m.Body)
	}
}

func TestParseManifestCRLF(t *testing.T) {
	raw := []byte("---\r\nname: a\r\ndescription: ok\r\n---\r\nbody\r\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "a" || m.Description != "ok" {
		t.Errorf("got name=%q description=%q", m.Name, m.Description)
	}
}

func TestParseManifestBOM(t *testing.T) {
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte("---\nname: a\ndescription: ok\n---\nbody\n")...)
	if _, err := ParseManifest(raw); err != nil {
		t.Fatal(err)
	}
}

func TestParseManifestMissingFrontmatter(t *testing.T) {
	_, err := ParseManifest([]byte("# just markdown\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "frontmatter") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseManifestUnterminatedFrontmatter(t *testing.T) {
	_, err := ParseManifest([]byte("---\nname: a\ndescription: ok\nbody\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseManifestInvalidName(t *testing.T) {
	_, err := ParseManifest([]byte("---\nname: BadName\ndescription: ok\n---\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseManifestMissingDescription(t *testing.T) {
	_, err := ParseManifest([]byte("---\nname: ok\n---\n"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseManifestBodyOverCap(t *testing.T) {
	body := strings.Repeat("x", MaxBodyBytes+1)
	raw := []byte("---\nname: ok\ndescription: ok\n---\n" + body)
	_, err := ParseManifest(raw)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("---\nname: a\ndescription: ok\n---\nhello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "a" {
		t.Errorf("name = %q", m.Name)
	}
}

func TestLoadManifestMissing(t *testing.T) {
	_, err := LoadManifest(filepath.Join(t.TempDir(), "nope.md"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected not-exist, got: %v", err)
	}
}

func TestIsSkillDir(t *testing.T) {
	dir := t.TempDir()
	if IsSkillDir(dir) {
		t.Fatal("empty dir reported as skill dir")
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("---\nname:a\ndescription:b\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !IsSkillDir(dir) {
		t.Fatal("dir with SKILL.md not reported as skill dir")
	}
}
