package plugin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// writeManifest creates <root>/.claude-plugin/plugin.json with content.
func writeManifest(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ManifestDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathListUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "single string", input: `"./skills"`, want: []string{"./skills"}},
		{name: "array", input: `["./a", "./b"]`, want: []string{"./a", "./b"}},
		{name: "empty array", input: `[]`, want: nil},
		{name: "number rejected", input: `42`, wantErr: true},
		{name: "mixed array rejected", input: `["./a", 1]`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p PathList
			err := json.Unmarshal([]byte(tt.input), &p)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(p) != len(tt.want) {
				t.Fatalf("got %v, want %v", p, tt.want)
			}
			for i := range p {
				if p[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", p, tt.want)
				}
			}
		})
	}
}

func TestLoadMissingManifest(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
}

func TestLoadMalformedManifest(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{"name": "broken",`)
	if _, err := Load(root); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadParsesSubset(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{
		"name": "squad-skills",
		"version": "0.1.0",
		"description": "d",
		"skills": ["./cooking", "./music"],
		"unknownField": {"ignored": true}
	}`)
	m, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "squad-skills" || m.Version != "0.1.0" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if len(m.Skills) != 2 {
		t.Fatalf("skills = %v", m.Skills)
	}
}

func TestSkillRoots(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"cooking", "music", DefaultSkillsDir} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := &Manifest{Skills: PathList{"./cooking", "./music", "./cooking", ".", ""}}
	roots, err := m.SkillRoots(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, DefaultSkillsDir), // default dir first, exists
		filepath.Join(root, "cooking"),
		filepath.Join(root, "music"),
	}
	if len(roots) != len(want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
	for i := range roots {
		if roots[i] != want[i] {
			t.Fatalf("roots = %v, want %v", roots, want)
		}
	}
}

func TestSkillRootsRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Skills: PathList{"../outside", "/abs/path", "./nested/../../escape", "./ok"}}
	roots, err := m.SkillRoots(root)
	if err == nil {
		t.Fatal("expected error for escaping roots")
	}
	if len(roots) != 1 || roots[0] != filepath.Join(root, "ok") {
		t.Fatalf("valid roots should survive escapes: %v", roots)
	}
}

func TestSkillRootsNoDefaultDir(t *testing.T) {
	root := t.TempDir() // no skills/ dir on disk
	m := &Manifest{Skills: PathList{"./cooking"}}
	roots, err := m.SkillRoots(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != filepath.Join(root, "cooking") {
		t.Fatalf("roots = %v", roots)
	}
}
