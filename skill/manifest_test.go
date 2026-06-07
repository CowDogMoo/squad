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
		// The reserved-substring rule is now a warning emitted by Validate,
		// not a hard error in the name regex — Anthropic's own claude-api
		// skill violates the spec hint, so the name parse must accept it.
		{"contains anthropic", "my-anthropic-skill", false},
		{"contains claude", "claude-helper", false},
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

func TestValidateNameXMLChars(t *testing.T) {
	if err := ValidateName("ok<bad"); err == nil {
		t.Error("expected error for name containing <")
	}
}

func TestValidateDescriptionGreaterThan(t *testing.T) {
	if err := ValidateDescription("uses >x"); err == nil {
		t.Error("expected error for description containing >")
	}
}

func TestLoadManifestParseFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("no frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(path); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseManifestInvalidYAMLFrontmatter(t *testing.T) {
	raw := []byte("---\nname: ok\ndescription: \"unterminated\n---\nbody\n")
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("expected YAML parse error")
	}
}

func TestParseManifestNoNewlineInBody(t *testing.T) {
	// Frontmatter terminated but body has no trailing newline.
	raw := []byte("---\nname: ok\ndescription: ok\n---")
	if _, err := ParseManifest(raw); err != nil {
		t.Fatalf("should accept body without trailing newline: %v", err)
	}
}

func TestReadLineEmpty(t *testing.T) {
	line, rest, ok := readLine(nil)
	if ok || line != nil || rest != nil {
		t.Errorf("empty input should return !ok, got line=%q rest=%q ok=%v", line, rest, ok)
	}
}

func TestReadLineNoNewline(t *testing.T) {
	line, rest, ok := readLine([]byte("abc"))
	if !ok || string(line) != "abc" || rest != nil {
		t.Errorf("got line=%q rest=%q ok=%v", line, rest, ok)
	}
}

func TestIndexClosingFenceFinalLineNoNewline(t *testing.T) {
	// A `---` on the final line with no trailing newline must still match.
	idx := indexClosingFence([]byte("a\nb\n---"))
	if idx != 4 {
		t.Errorf("expected offset 4, got %d", idx)
	}
}

func TestConsumeClosingLineNoNewline(t *testing.T) {
	got := consumeClosingLine([]byte("---"))
	if got != nil {
		t.Errorf("expected nil, got %q", got)
	}
}

func TestParseManifestOptionalFields(t *testing.T) {
	raw := []byte("---\n" +
		"name: ok\n" +
		"description: ok\n" +
		"license: MIT\n" +
		"compatibility: Requires git and bash\n" +
		"allowed-tools: \"Bash(python:*) Read WebFetch\"\n" +
		"metadata:\n" +
		"  author: Acme\n" +
		"  version: 1.2.3\n" +
		"---\nbody\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.License != "MIT" {
		t.Errorf("license = %q, want MIT", m.License)
	}
	if m.Compatibility != "Requires git and bash" {
		t.Errorf("compatibility = %q", m.Compatibility)
	}
	if got, want := []string(m.AllowedTools), []string{"Bash", "Read", "WebFetch"}; !equalSlices(got, want) {
		t.Errorf("allowed-tools = %v, want %v", got, want)
	}
	if m.Metadata["author"] != "Acme" || m.Metadata["version"] != "1.2.3" {
		t.Errorf("metadata = %v", m.Metadata)
	}
}

func TestParseManifestAllowedToolsSequence(t *testing.T) {
	raw := []byte("---\nname: ok\ndescription: ok\nallowed-tools:\n  - Read\n  - Bash(python:*)\n---\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string(m.AllowedTools), []string{"Read", "Bash"}; !equalSlices(got, want) {
		t.Errorf("allowed-tools = %v, want %v", got, want)
	}
}

func TestParseManifestAllowedToolsCommaSeparated(t *testing.T) {
	// Real-world skills (the squad-skills repo and most published Claude Code
	// skills) write the list with commas rather than the bare-space form the
	// guide example uses. Both must parse to the same set or AllowsTool will
	// silently deny access to a comma-suffixed entry like "Read,".
	raw := []byte("---\nname: ok\ndescription: ok\nallowed-tools: Read, Glob, Edit\n---\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []string(m.AllowedTools), []string{"Read", "Glob", "Edit"}; !equalSlices(got, want) {
		t.Errorf("allowed-tools = %v, want %v", got, want)
	}
	for _, n := range []string{"Read", "Glob", "Edit"} {
		if !m.AllowedTools.Allows(n) {
			t.Errorf("%s should be permitted; trailing comma must not leak into the entry", n)
		}
	}
}

func TestAllowedToolsAllows(t *testing.T) {
	empty := AllowedTools{}
	if !empty.Allows("anything") {
		t.Error("empty allow-list should permit any tool")
	}
	a := AllowedTools{"Read", "Bash"}
	if !a.Allows("Read") || !a.Allows("Bash") {
		t.Error("listed tools should be allowed")
	}
	if a.Allows("WebFetch") {
		t.Error("unlisted tool should be denied")
	}
}

func TestParseManifestCompatibilityTooLong(t *testing.T) {
	raw := []byte("---\nname: ok\ndescription: ok\ncompatibility: " + strings.Repeat("x", MaxCompatibilityLen+1) + "\n---\n")
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("expected compatibility-length error")
	}
}

func TestParseManifestAllowedToolsInvalidShape(t *testing.T) {
	raw := []byte("---\nname: ok\ndescription: ok\nallowed-tools:\n  Read: yes\n---\n")
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("expected error for mapping shape")
	}
}

func TestParseManifestTrimsDescriptionWhitespace(t *testing.T) {
	raw := []byte("---\nname: ok\ndescription: \"  padded with spaces   \"\n---\n")
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Description != "padded with spaces" {
		t.Errorf("description = %q, want %q", m.Description, "padded with spaces")
	}
}
