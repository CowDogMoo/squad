package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRepoMapTool_BasicTree(t *testing.T) {
	// Create a temp directory with a small project structure.
	root := t.TempDir()

	// Create some source files.
	dirs := []string{
		"src",
		"src/core",
		"src/cli",
		"tests",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := map[string]string{
		"src/core/lib.rs":  "fn main() {\n    println!(\"hello\");\n}\n",
		"src/core/util.rs": "pub fn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n",
		"src/cli/main.rs":  "fn main() {}\n",
		"tests/test.rs":    "fn test() {}\n",
		"README.md":        "# Test\n",
	}
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if rm.Summary.TotalFiles != 5 {
		t.Errorf("expected 5 files, got %d", rm.Summary.TotalFiles)
	}
	if rm.Language != "rust" {
		t.Errorf("expected primary language rust, got %q", rm.Language)
	}
	if rm.Summary.LanguageBreakdown["rust"] != 4 {
		t.Errorf("expected 4 rust files, got %d", rm.Summary.LanguageBreakdown["rust"])
	}
	if rm.Summary.LanguageBreakdown["markdown"] != 1 {
		t.Errorf("expected 1 markdown file, got %d", rm.Summary.LanguageBreakdown["markdown"])
	}
	if rm.Summary.TotalLines <= 0 {
		t.Error("expected positive line count")
	}
}

// createCargoWorkspace sets up a Cargo workspace with the given crate
// directories, root Cargo.toml, member Cargo.toml files, and source files.
func createCargoWorkspace(t *testing.T, root string, dirs []string, rootCargo string, memberCargos, srcFiles map[string]string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(rootCargo), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestFiles(t, root, memberCargos)
	writeTestFiles(t, root, srcFiles)
}

// writeTestFiles writes a map of relative paths to file contents under root.
func writeTestFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runRepoMapTool invokes the RepoMap tool and unmarshals the result.
func runRepoMapTool(t *testing.T, root string) repoMapResult {
	t.Helper()
	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	return rm
}

// assertModuleDependency checks that the named module depends on the given dependency.
func assertModuleDependency(t *testing.T, modules []repoModule, moduleName, depName string) {
	t.Helper()
	for _, m := range modules {
		if m.Name == moduleName {
			if len(m.Dependencies) == 0 {
				t.Errorf("expected %s to have dependencies", moduleName)
			}
			for _, dep := range m.Dependencies {
				if dep == depName {
					return
				}
			}
			t.Errorf("expected %s to depend on %s, got %v", moduleName, depName, m.Dependencies)
			return
		}
	}
}

func TestRepoMapTool_CargoWorkspace(t *testing.T) {
	root := t.TempDir()

	rootCargo := `[workspace]
members = [
    "crates/core",
    "crates/cli",
    "crates/lib",
]
`
	createCargoWorkspace(t, root,
		[]string{"crates/core", "crates/cli", "crates/lib"},
		rootCargo,
		map[string]string{
			"crates/core/Cargo.toml": "[package]\nname = \"ares-core\"\nversion = \"0.1.0\"\n",
			"crates/cli/Cargo.toml":  "[package]\nname = \"ares-cli\"\nversion = \"0.1.0\"\n\n[dependencies]\nares-core = { path = \"../core\" }\n",
			"crates/lib/Cargo.toml":  "[package]\nname = \"ares-lib\"\nversion = \"0.1.0\"\n",
		},
		map[string]string{
			"crates/core/src/lib.rs": "pub fn core_fn() {}\n",
			"crates/cli/src/main.rs": "fn main() {}\n",
			"crates/lib/src/lib.rs":  "pub fn lib_fn() {}\n",
		},
	)

	rm := runRepoMapTool(t, root)

	// Should detect 3 workspace members.
	if len(rm.Modules) != 3 {
		t.Errorf("expected 3 modules, got %d: %+v", len(rm.Modules), rm.Modules)
	}

	// Check module types.
	for _, m := range rm.Modules {
		if m.Type != "cargo-workspace-member" {
			t.Errorf("expected cargo-workspace-member type, got %q for %s", m.Type, m.Name)
		}
	}

	// Check that ares-cli has a dependency on core.
	assertModuleDependency(t, rm.Modules, "ares-cli", "core")
}

func TestRepoMapTool_CargoWorkspaceGlob(t *testing.T) {
	root := t.TempDir()

	dirs := []string{
		"crates/alpha",
		"crates/beta",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	rootCargo := "[workspace]\nmembers = [\"crates/*\"]\n"
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte(rootCargo), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"alpha", "beta"} {
		cargo := fmt.Sprintf("[package]\nname = \"%s\"\n", name)
		if err := os.WriteFile(filepath.Join(root, "crates", name, "Cargo.toml"), []byte(cargo), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "crates", name, "src", "lib.rs"), []byte("fn f() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatal(err)
	}

	if len(rm.Modules) != 2 {
		t.Errorf("expected 2 modules from glob, got %d: %+v", len(rm.Modules), rm.Modules)
	}
}

func TestRepoMapTool_GoModule(t *testing.T) {
	root := t.TempDir()

	goMod := "module github.com/example/myproject\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatal(err)
	}

	if len(rm.Modules) != 1 {
		t.Errorf("expected 1 module, got %d", len(rm.Modules))
	}
	if rm.Modules[0].Type != "go-module" {
		t.Errorf("expected go-module, got %q", rm.Modules[0].Type)
	}
	if rm.Modules[0].Name != "github.com/example/myproject" {
		t.Errorf("expected github.com/example/myproject, got %q", rm.Modules[0].Name)
	}
}

func TestRepoMapTool_NPMWorkspace(t *testing.T) {
	root := t.TempDir()

	rootPkg := `{"name": "monorepo", "workspaces": ["packages/*"]}`
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(rootPkg), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"web", "api"} {
		pkgDir := filepath.Join(root, "packages", name)
		if err := os.MkdirAll(pkgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		pkg := fmt.Sprintf(`{"name": "@monorepo/%s"}`, name)
		if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(pkg), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "index.ts"), []byte("export {};\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatal(err)
	}

	if len(rm.Modules) != 2 {
		t.Errorf("expected 2 npm workspace members, got %d: %+v", len(rm.Modules), rm.Modules)
	}
	for _, m := range rm.Modules {
		if m.Type != "npm-workspace-member" {
			t.Errorf("expected npm-workspace-member, got %q", m.Type)
		}
	}
}

func TestRepoMapTool_MaxDepth(t *testing.T) {
	root := t.TempDir()

	// Create a deep nesting.
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f", "g")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "deep.go"), []byte("package deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "top.go"), []byte("package top\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// With max_depth=3, should not reach g.
	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{"max_depth": 3}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatal(err)
	}

	// deep.go at depth 7 should be excluded.
	for _, entry := range rm.Tree {
		if entry.Path == "a/b/c/d/e/f/g" {
			t.Error("directory at depth 7 should not appear with max_depth=3")
		}
	}
}

func TestRepoMapTool_SkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "objects", "abc"), []byte("git object"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "index.js"), []byte("module.exports = {};\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := repoMapTool(root)
	result, err := tool(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}

	var rm repoMapResult
	if err := json.Unmarshal([]byte(result), &rm); err != nil {
		t.Fatal(err)
	}

	// Should only have main.go, not files in .git or node_modules.
	if rm.Summary.TotalFiles != 1 {
		t.Errorf("expected 1 file (main.go), got %d", rm.Summary.TotalFiles)
	}
}

func TestCountLines(t *testing.T) {
	tmp := t.TempDir()

	// Normal file with 3 lines.
	f := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countLines(f); got != 3 {
		t.Errorf("expected 3 lines, got %d", got)
	}

	// File without trailing newline.
	f2 := filepath.Join(tmp, "test2.txt")
	if err := os.WriteFile(f2, []byte("line1\nline2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countLines(f2); got != 2 {
		t.Errorf("expected 2 lines, got %d", got)
	}

	// Empty file.
	f3 := filepath.Join(tmp, "empty.txt")
	if err := os.WriteFile(f3, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countLines(f3); got != 0 {
		t.Errorf("expected 0 lines, got %d", got)
	}

	// Binary file (contains null byte).
	f4 := filepath.Join(tmp, "binary")
	if err := os.WriteFile(f4, []byte("hello\x00world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countLines(f4); got != 0 {
		t.Errorf("expected 0 lines for binary, got %d", got)
	}
}

func TestPrimaryLanguage(t *testing.T) {
	breakdown := map[string]int{
		"rust":     10,
		"yaml":     20,
		"markdown": 15,
		"toml":     5,
	}
	if got := primaryLanguage(breakdown); got != "rust" {
		t.Errorf("expected rust, got %q", got)
	}

	breakdown2 := map[string]int{
		"go":     5,
		"python": 8,
	}
	if got := primaryLanguage(breakdown2); got != "python" {
		t.Errorf("expected python, got %q", got)
	}
}

func TestNewBackgroundTaskRegistry_CustomConcurrency(t *testing.T) {
	r := NewBackgroundTaskRegistry(8)
	if cap(r.semaphore) != 8 {
		t.Errorf("expected semaphore capacity 8, got %d", cap(r.semaphore))
	}

	r2 := NewBackgroundTaskRegistry(0)
	if cap(r2.semaphore) != DefaultConcurrentTasks {
		t.Errorf("expected default capacity %d, got %d", DefaultConcurrentTasks, cap(r2.semaphore))
	}
}
