package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// repoMapResult is the structured output of the RepoMap tool.
type repoMapResult struct {
	Root     string       `json:"root"`
	Summary  repoSummary  `json:"summary"`
	Modules  []repoModule `json:"modules"`
	Tree     []dirEntry   `json:"tree"`
	Language string       `json:"primary_language"`
}

type repoSummary struct {
	TotalFiles        int            `json:"total_files"`
	TotalDirs         int            `json:"total_dirs"`
	TotalLines        int            `json:"total_lines"`
	LanguageBreakdown map[string]int `json:"languages"` // extension -> file count
}

// repoModule represents a detected module/package boundary.
type repoModule struct {
	Path         string   `json:"path"`
	Type         string   `json:"type"` // "cargo-workspace-member", "cargo-crate", "go-module", "npm-package", "python-package", etc.
	Name         string   `json:"name"`
	Files        int      `json:"files"`
	Lines        int      `json:"lines"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// dirEntry is a summarized directory in the tree.
type dirEntry struct {
	Path  string `json:"path"`
	Files int    `json:"files"`
	Lines int    `json:"lines"`
	Depth int    `json:"depth"`
}

// moduleDetector detects module boundaries by looking for marker files.
type moduleDetector struct {
	// markerFile is the filename to look for (e.g., "Cargo.toml").
	markerFile string
	// detect is called when the marker file is found. Returns modules.
	detect func(dir string, markerPath string) ([]repoModule, error)
}

// knownModuleDetectors lists all supported module boundary detectors.
var knownModuleDetectors = []moduleDetector{
	{markerFile: "Cargo.toml", detect: detectCargoModules},
	{markerFile: "go.mod", detect: detectGoModule},
	{markerFile: "package.json", detect: detectNPMPackage},
	{markerFile: "pyproject.toml", detect: detectPythonPackage},
	{markerFile: "setup.py", detect: detectPythonPackage},
	{markerFile: "pom.xml", detect: detectMavenModule},
	{markerFile: "build.gradle", detect: detectGradleModule},
	{markerFile: "CMakeLists.txt", detect: detectCMakeProject},
}

// langExtensions maps file extensions to language names.
var langExtensions = map[string]string{
	".rs":    "rust",
	".go":    "go",
	".py":    "python",
	".js":    "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".jsx":   "javascript",
	".java":  "java",
	".kt":    "kotlin",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".hpp":   "cpp",
	".cc":    "cpp",
	".cs":    "csharp",
	".rb":    "ruby",
	".php":   "php",
	".swift": "swift",
	".scala": "scala",
	".zig":   "zig",
	".lua":   "lua",
	".sh":    "shell",
	".bash":  "shell",
	".zsh":   "shell",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".json":  "json",
	".md":    "markdown",
	".sql":   "sql",
	".tf":    "terraform",
	".hcl":   "hcl",
	".nix":   "nix",
	".ex":    "elixir",
	".exs":   "elixir",
	".erl":   "erlang",
	".hs":    "haskell",
	".ml":    "ocaml",
	".r":     "r",
	".R":     "r",
}

func definitionRepoMap() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "RepoMap",
			Description: `Analyze repository structure and identify module boundaries for multi-agent task decomposition.
Returns a structured map of the repository including:
- Directory tree with file/line counts
- Detected modules (Cargo workspace members, Go modules, npm packages, Python packages, etc.)
- Language breakdown
- Suggested slices for parallel agent processing

Use this tool to understand a large codebase before dispatching child agents via Task.
Each detected module is a natural slice boundary for parallel processing.`,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory to analyze. Defaults to the working directory.",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum directory depth to traverse (default: 6).",
					},
				},
			},
		},
	}
}

type repoMapArgs struct {
	Path     string `json:"path,omitempty"`
	MaxDepth int    `json:"max_depth,omitempty"`
}

func repoMapTool(workingDir string) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args repoMapArgs
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return "", fmt.Errorf("invalid RepoMap args: %w", err)
			}
		}

		root := workingDir
		if args.Path != "" {
			resolved, err := ResolvePath(workingDir, args.Path)
			if err != nil {
				return "", fmt.Errorf("invalid path: %w", err)
			}
			root = resolved
		}

		maxDepth := args.MaxDepth
		if maxDepth <= 0 {
			maxDepth = 6
		}

		logging.InfoContext(ctx, "RepoMap: analyzing %s (max_depth=%d)", root, maxDepth)

		result, err := analyzeRepo(root, maxDepth)
		if err != nil {
			return "", fmt.Errorf("repo analysis failed: %w", err)
		}

		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}

		logging.InfoContext(ctx, "RepoMap: found %d modules, %d files across %d dirs",
			len(result.Modules), result.Summary.TotalFiles, result.Summary.TotalDirs)

		return string(out), nil
	}
}

// dirStats tracks per-directory file and line counts.
type dirStats struct {
	files int
	lines int
}

// walkVisitor handles a single entry during the directory walk, updating
// the result summary and dirMap accordingly.
func walkVisitor(root string, maxDepth int, result *repoMapResult, dirMap map[string]*dirStats) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		relPath, _ := filepath.Rel(root, path)
		if relPath == "." {
			relPath = ""
		}

		if d.IsDir() {
			return visitDir(relPath, d, maxDepth, result, dirMap)
		}
		return visitFile(relPath, path, d, result, dirMap)
	}
}

// visitDir processes a directory entry during the walk.
func visitDir(relPath string, d fs.DirEntry, maxDepth int, result *repoMapResult, dirMap map[string]*dirStats) error {
	name := d.Name()
	if (strings.HasPrefix(name, ".") && name != ".") || defaultSkipDirs[name] {
		return filepath.SkipDir
	}
	depth := strings.Count(relPath, string(filepath.Separator))
	if relPath != "" {
		depth++
	}
	if depth > maxDepth {
		return filepath.SkipDir
	}
	result.Summary.TotalDirs++
	dirMap[relPath] = &dirStats{}
	return nil
}

// visitFile processes a regular file entry during the walk.
func visitFile(relPath, fullPath string, d fs.DirEntry, result *repoMapResult, dirMap map[string]*dirStats) error {
	if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
		return nil
	}

	result.Summary.TotalFiles++

	ext := strings.ToLower(filepath.Ext(d.Name()))
	if lang, ok := langExtensions[ext]; ok {
		result.Summary.LanguageBreakdown[lang]++
	}

	lines := countLines(fullPath)
	result.Summary.TotalLines += lines

	dir := filepath.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	if ds, ok := dirMap[dir]; ok {
		ds.files++
		ds.lines += lines
	}
	return nil
}

// buildTree converts the dirMap into a sorted slice of dirEntry values.
func buildTree(dirMap map[string]*dirStats) []dirEntry {
	tree := make([]dirEntry, 0, len(dirMap))
	for dir, stats := range dirMap {
		depth := 0
		if dir != "" {
			depth = strings.Count(dir, string(filepath.Separator)) + 1
		}
		displayPath := dir
		if displayPath == "" {
			displayPath = "."
		}
		tree = append(tree, dirEntry{
			Path:  displayPath,
			Files: stats.files,
			Lines: stats.lines,
			Depth: depth,
		})
	}
	sort.Slice(tree, func(i, j int) bool {
		return tree[i].Path < tree[j].Path
	})
	return tree
}

func analyzeRepo(root string, maxDepth int) (*repoMapResult, error) {
	result := &repoMapResult{
		Root: root,
		Summary: repoSummary{
			LanguageBreakdown: make(map[string]int),
		},
	}

	dirMap := make(map[string]*dirStats)

	if err := filepath.WalkDir(root, walkVisitor(root, maxDepth, result, dirMap)); err != nil {
		return nil, fmt.Errorf("walk failed: %w", err)
	}

	result.Tree = buildTree(dirMap)
	result.Modules = detectModules(root)
	result.Language = primaryLanguage(result.Summary.LanguageBreakdown)

	return result, nil
}

// countLines counts newlines in a file. Returns 0 for binary/unreadable files.
func countLines(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	// Skip files larger than 1MB for line counting.
	if info.Size() > 1<<20 {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	// Quick binary check: if there's a null byte in the first 512 bytes, skip.
	checkLen := 512
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return 0
		}
	}
	if len(data) == 0 {
		return 0
	}
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	// Count last line if it doesn't end with newline.
	if data[len(data)-1] != '\n' {
		count++
	}
	return count
}

func primaryLanguage(breakdown map[string]int) string {
	// Exclude non-source languages from primary detection.
	excludeFromPrimary := map[string]bool{
		"yaml": true, "json": true, "markdown": true, "toml": true,
	}
	best := ""
	bestCount := 0
	for lang, count := range breakdown {
		if excludeFromPrimary[lang] {
			continue
		}
		if count > bestCount {
			best = lang
			bestCount = count
		}
	}
	return best
}

// detectModules walks the root looking for module boundary markers.
func detectModules(root string) []repoModule {
	var modules []repoModule
	seen := make(map[string]bool) // deduplicate by path

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if (strings.HasPrefix(name, ".") && name != ".") || defaultSkipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		dir := filepath.Dir(path)
		for _, detector := range knownModuleDetectors {
			if d.Name() == detector.markerFile {
				detected, detErr := detector.detect(dir, path)
				if detErr != nil {
					continue
				}
				for _, m := range detected {
					if !seen[m.Path] {
						seen[m.Path] = true
						modules = append(modules, m)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return modules
	}

	// Sort modules by path for stable output.
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Path < modules[j].Path
	})

	// Fill in file/line counts for each module.
	for i := range modules {
		absPath := modules[i].Path
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(root, absPath)
		}
		files, lines := countDirStats(absPath)
		modules[i].Files = files
		modules[i].Lines = lines
		// Make path relative to root for readability.
		if rel, err := filepath.Rel(root, absPath); err == nil {
			modules[i].Path = rel
		}
	}

	return modules
}

// countDirStats counts files and lines in a directory recursively.
func countDirStats(dir string) (files, lines int) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if (strings.HasPrefix(name, ".") && name != ".") || defaultSkipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		files++
		lines += countLines(path)
		return nil
	})
	return
}

// --- Module detectors ---

// detectCargoModules detects Rust crate and workspace members.
func detectCargoModules(dir, markerPath string) ([]repoModule, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, err
	}
	content := string(data)

	// Extract crate name from [package] section.
	name := extractTOMLValue(content, "name")
	if name == "" {
		name = filepath.Base(dir)
	}

	var modules []repoModule

	// Check for workspace members.
	members := extractCargoWorkspaceMembers(content, dir)
	if len(members) > 0 {
		// This is a workspace root — return each member as a module.
		for _, memberPath := range members {
			memberName := filepath.Base(memberPath)
			// Try to read the member's Cargo.toml for its actual name.
			memberToml := filepath.Join(memberPath, "Cargo.toml")
			if memberData, err := os.ReadFile(memberToml); err == nil {
				if n := extractTOMLValue(string(memberData), "name"); n != "" {
					memberName = n
				}
			}
			deps := extractCargoDependencies(memberPath)
			modules = append(modules, repoModule{
				Path:         memberPath,
				Type:         "cargo-workspace-member",
				Name:         memberName,
				Dependencies: deps,
			})
		}
		return modules, nil
	}

	// Standalone crate.
	deps := extractCargoDependencies(dir)
	modules = append(modules, repoModule{
		Path:         dir,
		Type:         "cargo-crate",
		Name:         name,
		Dependencies: deps,
	})
	return modules, nil
}

// extractCargoWorkspaceMembers extracts workspace member paths from Cargo.toml.
func extractCargoWorkspaceMembers(content, dir string) []string {
	// Look for [workspace] section with members = [...]
	wsIdx := strings.Index(content, "[workspace]")
	if wsIdx < 0 {
		return nil
	}

	membersIdx := strings.Index(content[wsIdx:], "members")
	if membersIdx < 0 {
		return nil
	}

	// Find the array brackets.
	start := strings.Index(content[wsIdx+membersIdx:], "[")
	if start < 0 {
		return nil
	}
	end := strings.Index(content[wsIdx+membersIdx+start:], "]")
	if end < 0 {
		return nil
	}

	arrayContent := content[wsIdx+membersIdx+start+1 : wsIdx+membersIdx+start+end]

	var members []string
	for _, item := range strings.Split(arrayContent, ",") {
		item = strings.TrimSpace(item)
		item = strings.Trim(item, "\"' \n\r\t")
		if item == "" {
			continue
		}

		// Handle glob patterns (e.g., "crates/*").
		if strings.Contains(item, "*") {
			pattern := filepath.Join(dir, item)
			matches, err := filepath.Glob(pattern)
			if err == nil {
				for _, m := range matches {
					// Only include directories that have Cargo.toml.
					if _, err := os.Stat(filepath.Join(m, "Cargo.toml")); err == nil {
						members = append(members, m)
					}
				}
			}
		} else {
			memberPath := filepath.Join(dir, item)
			if _, err := os.Stat(filepath.Join(memberPath, "Cargo.toml")); err == nil {
				members = append(members, memberPath)
			}
		}
	}

	return members
}

// extractCargoDependencies reads a crate's Cargo.toml and extracts local path dependencies.
func extractCargoDependencies(crateDir string) []string {
	data, err := os.ReadFile(filepath.Join(crateDir, "Cargo.toml"))
	if err != nil {
		return nil
	}
	content := string(data)
	var deps []string

	// Track whether we're in a [dependencies] or [dev-dependencies] section.
	inDepSection := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Detect section headers.
		if strings.HasPrefix(trimmed, "[") {
			lower := strings.ToLower(trimmed)
			inDepSection = strings.Contains(lower, "dependencies")
			continue
		}

		if !inDepSection {
			continue
		}

		// Look for path = "..." in dependency lines.
		if !strings.Contains(trimmed, "path") || !strings.Contains(trimmed, "=") {
			continue
		}
		if idx := strings.Index(trimmed, "path"); idx >= 0 {
			rest := trimmed[idx:]
			if qStart := strings.IndexByte(rest, '"'); qStart >= 0 {
				rest = rest[qStart+1:]
				if qEnd := strings.IndexByte(rest, '"'); qEnd >= 0 {
					depPath := rest[:qEnd]
					// Skip paths to source files (e.g., "src/main.rs").
					if strings.HasSuffix(depPath, ".rs") {
						continue
					}
					// Resolve to a crate name.
					resolved := filepath.Join(crateDir, depPath)
					deps = append(deps, filepath.Base(resolved))
				}
			}
		}
	}
	return deps
}

// extractTOMLValue extracts a simple key = "value" from TOML content.
// Only handles simple string values, not nested tables.
func extractTOMLValue(content, key string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			rest := strings.TrimPrefix(trimmed, key)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "=") {
				rest = strings.TrimPrefix(rest, "=")
				rest = strings.TrimSpace(rest)
				rest = strings.Trim(rest, "\"'")
				return rest
			}
		}
	}
	return ""
}

func detectGoModule(dir, markerPath string) ([]repoModule, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, err
	}
	// Extract module name from first line.
	content := string(data)
	name := ""
	if first, _, ok := strings.Cut(content, "\n"); ok {
		if strings.HasPrefix(first, "module ") {
			name = strings.TrimPrefix(first, "module ")
			name = strings.TrimSpace(name)
		}
	}
	if name == "" {
		name = filepath.Base(dir)
	}
	return []repoModule{{
		Path: dir,
		Type: "go-module",
		Name: name,
	}}, nil
}

func detectNPMPackage(dir, markerPath string) ([]repoModule, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, err
	}

	var pkg struct {
		Name       string   `json:"name"`
		Workspaces []string `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	if pkg.Name == "" {
		pkg.Name = filepath.Base(dir)
	}

	var modules []repoModule

	// Handle workspaces.
	if len(pkg.Workspaces) > 0 {
		for _, ws := range pkg.Workspaces {
			pattern := filepath.Join(dir, ws)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				continue
			}
			for _, m := range matches {
				wsName := filepath.Base(m)
				wsPkg := filepath.Join(m, "package.json")
				if pkgData, err := os.ReadFile(wsPkg); err == nil {
					var wsPkgJSON struct {
						Name string `json:"name"`
					}
					if json.Unmarshal(pkgData, &wsPkgJSON) == nil && wsPkgJSON.Name != "" {
						wsName = wsPkgJSON.Name
					}
				}
				modules = append(modules, repoModule{
					Path: m,
					Type: "npm-workspace-member",
					Name: wsName,
				})
			}
		}
		return modules, nil
	}

	return []repoModule{{
		Path: dir,
		Type: "npm-package",
		Name: pkg.Name,
	}}, nil
}

func detectPythonPackage(dir, markerPath string) ([]repoModule, error) {
	name := filepath.Base(dir)

	// Try to extract name from pyproject.toml if available.
	if strings.HasSuffix(markerPath, "pyproject.toml") {
		if data, err := os.ReadFile(markerPath); err == nil {
			if n := extractTOMLValue(string(data), "name"); n != "" {
				name = n
			}
		}
	}

	return []repoModule{{
		Path: dir,
		Type: "python-package",
		Name: name,
	}}, nil
}

func detectMavenModule(dir, markerPath string) ([]repoModule, error) {
	name := filepath.Base(dir)
	return []repoModule{{
		Path: dir,
		Type: "maven-module",
		Name: name,
	}}, nil
}

func detectGradleModule(dir, markerPath string) ([]repoModule, error) {
	name := filepath.Base(dir)
	return []repoModule{{
		Path: dir,
		Type: "gradle-module",
		Name: name,
	}}, nil
}

func detectCMakeProject(dir, markerPath string) ([]repoModule, error) {
	name := filepath.Base(dir)
	return []repoModule{{
		Path: dir,
		Type: "cmake-project",
		Name: name,
	}}, nil
}
