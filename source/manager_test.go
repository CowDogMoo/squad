package source_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/source"
)

func newTestManager(t *testing.T, cfg *config.Config) *source.Manager {
	t.Helper()

	tempRoot := t.TempDir()
	configHome := filepath.Join(tempRoot, "config")
	cacheHome := filepath.Join(tempRoot, "cache")

	if err := os.MkdirAll(configHome, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create config home: %v", err)
	}
	if err := os.MkdirAll(cacheHome, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create cache home: %v", err)
	}

	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("HOME", tempRoot)

	manager, err := source.NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager
}

func configPath(t *testing.T) string {
	t.Helper()

	path, err := config.ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile() error = %v", err)
	}

	return path
}

func resolvedPath(t *testing.T, value string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}

	return resolved
}

func createAgentDir(t *testing.T, parent, name, manifest string) string {
	t.Helper()

	agentPath := filepath.Join(parent, name)
	if err := os.MkdirAll(agentPath, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create agent dir %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(agentPath, "agent.yaml"),
		[]byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest for %s: %v", name, err)
	}

	return agentPath
}

func withWorkDir(t *testing.T, workDir string) {
	t.Helper()

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore dir: %v", err)
		}
	})
}

func setupAgentsDir(t *testing.T, workDir string) string {
	t.Helper()

	agentsDir := filepath.Join(workDir, "agents")
	if err := os.MkdirAll(agentsDir, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}

	return agentsDir
}

func checkWantError(t *testing.T, err error, wantErr string) bool {
	t.Helper()

	if wantErr != "" {
		if err == nil {
			t.Fatalf("expected error containing %q", wantErr)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("error = %q, want %q", err.Error(), wantErr)
		}
		return true
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return false
}

func checkConfigFileState(t *testing.T, wantErr string) {
	t.Helper()

	configFile := configPath(t)
	_, statErr := os.Stat(configFile)
	hasFile := statErr == nil

	if wantErr != "" && hasFile {
		t.Fatalf("config file created on error: %s", configFile)
	}
	if wantErr == "" && !hasFile {
		t.Fatalf("config file not created: %v", statErr)
	}
}

func TestManagerAddRepository(t *testing.T) {
	tests := []struct {
		name       string
		gitURL     string
		existing   map[string]string
		wantErr    string
		wantStored string
	}{
		{
			name:    "invalid url",
			gitURL:  "not-a-url",
			wantErr: "invalid git URL",
		},
		{
			name:       "new repository",
			gitURL:     "https://github.com/org/repo.git",
			wantStored: "https://github.com/org/repo.git",
		},
		{
			name:    "existing same url",
			gitURL:  "https://github.com/org/repo.git",
			wantErr: "already configured",
			existing: map[string]string{
				"repo": "https://github.com/org/repo.git",
			},
		},
		{
			name:    "existing different url",
			gitURL:  "https://github.com/org/repo.git",
			wantErr: "already exists",
			existing: map[string]string{
				"repo": "https://github.com/other/repo.git",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Agents.Repositories = nil
			if tt.existing != nil {
				cfg.Agents.Repositories = map[string]config.RepoSpec{}
				for key, value := range tt.existing {
					cfg.Agents.Repositories[key] = config.RepoSpec{URL: value}
				}
			}
			cfg.Agents.LocalPaths = []string{}

			manager := newTestManager(t, cfg)
			err := manager.AddRepository("repo", tt.gitURL, "")

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			configFile := configPath(t)
			_, statErr := os.Stat(configFile)
			hasFile := statErr == nil
			if tt.wantErr != "" && hasFile {
				t.Fatalf("config file created on error: %s", configFile)
			}
			if tt.wantErr == "" && !hasFile {
				t.Fatalf("config file not created: %v", statErr)
			}

			if tt.wantStored != "" {
				if cfg.Agents.Repositories["repo"].URL != tt.wantStored {
					t.Fatalf("stored url = %q, want %q",
						cfg.Agents.Repositories["repo"].URL, tt.wantStored)
				}
			}
		})
	}
}

func TestManagerAddLocalPath(t *testing.T) {
	tests := []struct {
		name        string
		setupPath   func(t *testing.T) string
		existing    []string
		wantErr     string
		wantPresent bool
	}{
		{
			name: "missing path",
			setupPath: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "missing")
			},
			wantErr: "does not exist",
		},
		{
			name: "file path",
			setupPath: func(t *testing.T) string {
				dir := t.TempDir()
				filePath := filepath.Join(dir, "file.txt")
				if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
					t.Fatalf("write file: %v", err)
				}
				return filePath
			},
			wantErr: "not a directory",
		},
		{
			name: "valid path",
			setupPath: func(t *testing.T) string {
				return t.TempDir()
			},
			wantPresent: true,
		},
		{
			name: "duplicate path",
			setupPath: func(t *testing.T) string {
				return t.TempDir()
			},
			existing: []string{"DUPLICATE"},
			wantErr:  "already configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.Agents.Repositories = map[string]config.RepoSpec{}
			cfg.Agents.LocalPaths = []string{}

			pathValue := tt.setupPath(t)
			absPath := mustAbs(t, pathValue)

			if len(tt.existing) > 0 {
				cfg.Agents.LocalPaths = []string{absPath}
			}

			manager := newTestManager(t, cfg)
			err := manager.AddLocalPath(pathValue)

			if checkWantError(t, err, tt.wantErr) {
				checkConfigFileState(t, tt.wantErr)
				return
			}
			checkConfigFileState(t, tt.wantErr)
			checkLocalPathPresent(t, cfg, tt.wantPresent, absPath)
		})
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	absPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs() error = %v", err)
	}
	return absPath
}

func checkLocalPathPresent(t *testing.T, cfg *config.Config, wantPresent bool, absPath string) {
	t.Helper()
	if !wantPresent {
		return
	}
	if len(cfg.Agents.LocalPaths) != 1 {
		t.Fatalf("local paths count = %d, want 1", len(cfg.Agents.LocalPaths))
	}
	if cfg.Agents.LocalPaths[0] != absPath {
		t.Fatalf("stored path = %q, want %q", cfg.Agents.LocalPaths[0], absPath)
	}
}

func TestManagerRemoveSource(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (*config.Config, string)
		input     string
		wantErr   string
		wantEmpty bool
	}{
		{
			name: "remove repository",
			setup: func(t *testing.T) (*config.Config, string) {
				cfg := config.Defaults()
				cfg.Agents.Repositories = map[string]config.RepoSpec{
					"repo": {URL: "https://github.com/org/repo.git"},
				}
				cfg.Agents.LocalPaths = []string{}
				return cfg, "repo"
			},
			input:     "repo",
			wantEmpty: true,
		},
		{
			name: "remove local path",
			setup: func(t *testing.T) (*config.Config, string) {
				cfg := config.Defaults()
				cfg.Agents.Repositories = map[string]config.RepoSpec{}
				localPath := t.TempDir()
				cfg.Agents.LocalPaths = []string{localPath}
				return cfg, localPath
			},
			input:     "LOCAL",
			wantEmpty: true,
		},
		{
			name: "not found",
			setup: func(t *testing.T) (*config.Config, string) {
				cfg := config.Defaults()
				cfg.Agents.Repositories = map[string]config.RepoSpec{}
				cfg.Agents.LocalPaths = []string{}
				return cfg, "missing"
			},
			input:   "missing",
			wantErr: "source not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, target := tt.setup(t)
			manager := newTestManager(t, cfg)

			inputValue := tt.input
			if tt.input == "LOCAL" {
				inputValue = target
			}

			err := manager.RemoveSource(inputValue)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			configFile := configPath(t)
			_, statErr := os.Stat(configFile)
			hasFile := statErr == nil
			if tt.wantErr != "" && hasFile {
				t.Fatalf("config file created on error: %s", configFile)
			}
			if tt.wantErr == "" && !hasFile {
				t.Fatalf("config file not created: %v", statErr)
			}

			if tt.wantEmpty {
				if len(cfg.Agents.Repositories) != 0 {
					t.Fatalf("repositories not cleared")
				}
				if len(cfg.Agents.LocalPaths) != 0 {
					t.Fatalf("local paths not cleared")
				}
			}
		})
	}
}

func TestManagerUpdateRepositoriesEmpty(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{}
	cfg.Agents.LocalPaths = []string{}
	manager := newTestManager(t, cfg)

	if err := manager.UpdateRepositories(false); err != nil {
		t.Fatalf("UpdateRepositories() error = %v", err)
	}
}

func TestManagerGetSearchPaths(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{}
	cfg.Agents.LocalPaths = []string{}

	manager := newTestManager(t, cfg)

	localDir := t.TempDir()
	cfg.Agents.LocalPaths = []string{localDir}

	workDir := t.TempDir()
	agentsDir := filepath.Join(workDir, "agents")
	if err := os.MkdirAll(agentsDir, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore dir: %v", err)
		}
	})

	paths, err := manager.GetSearchPaths()
	if err != nil {
		t.Fatalf("GetSearchPaths() error = %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("paths count = %d, want 2", len(paths))
	}

	expectedAgents := resolvedPath(t, agentsDir)
	expectedLocal := resolvedPath(t, localDir)

	if resolvedPath(t, paths[0]) != expectedAgents {
		t.Fatalf("first path = %q, want %q", paths[0], expectedAgents)
	}
	if resolvedPath(t, paths[1]) != expectedLocal {
		t.Fatalf("second path = %q, want %q", paths[1], expectedLocal)
	}
}

func TestManagerListAgents(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{}
	cfg.Agents.LocalPaths = []string{}

	manager := newTestManager(t, cfg)

	workDir := t.TempDir()
	agentsDir := setupAgentsDir(t, workDir)

	localSearch := t.TempDir()
	cfg.Agents.LocalPaths = []string{localSearch}

	manifest := "version: v1\ndescription: primary agent\n"
	createAgentDir(t, agentsDir, "alpha", manifest)
	createAgentDir(t, localSearch, "alpha", manifest)
	createAgentDir(t, agentsDir, "_ignored", manifest)

	withWorkDir(t, workDir)

	agents, err := manager.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("agents count = %d, want 1", len(agents))
	}

	verifyAgent(t, agents[0], "alpha", "v1", "primary agent", agentsDir)
}

func verifyAgent(t *testing.T, agent source.AgentInfo, name, version, description, expectedSource string) {
	t.Helper()
	if agent.Name != name {
		t.Fatalf("agent name = %q, want %s", agent.Name, name)
	}
	if agent.Version != version {
		t.Fatalf("agent version = %q, want %s", agent.Version, version)
	}
	if agent.Description != description {
		t.Fatalf("agent description = %q, want %s", agent.Description, description)
	}
	resolvedExpected := resolvedPath(t, expectedSource)
	if resolvedPath(t, agent.Source) != resolvedExpected {
		t.Fatalf("agent source = %q, want %q", agent.Source, resolvedExpected)
	}
}

func TestManagerFindAgent(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) (*config.Config, string, string)
		agentName string
		wantErr   string
	}{
		{
			name: "found",
			setup: func(t *testing.T) (*config.Config, string, string) {
				cfg := config.Defaults()
				cfg.Agents.Repositories = map[string]config.RepoSpec{}
				cfg.Agents.LocalPaths = []string{}
				workDir := t.TempDir()
				agentsDir := filepath.Join(workDir, "agents")
				if err := os.MkdirAll(agentsDir, config.DirPermReadWriteExec); err != nil {
					t.Fatalf("create agents dir: %v", err)
				}
				agentPath := filepath.Join(agentsDir, "beta")
				if err := os.MkdirAll(agentPath, config.DirPermReadWriteExec); err != nil {
					t.Fatalf("create agent dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(agentPath, "agent.yaml"),
					[]byte("version: v2\n"), 0o600); err != nil {
					t.Fatalf("write manifest: %v", err)
				}
				return cfg, workDir, agentPath
			},
			agentName: "beta",
		},
		{
			name: "missing",
			setup: func(t *testing.T) (*config.Config, string, string) {
				cfg := config.Defaults()
				cfg.Agents.Repositories = map[string]config.RepoSpec{}
				cfg.Agents.LocalPaths = []string{}
				return cfg, t.TempDir(), ""
			},
			agentName: "missing",
			wantErr:   "agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, workDir, expected := tt.setup(t)
			manager := newTestManager(t, cfg)

			previousDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Getwd() error = %v", err)
			}
			if err := os.Chdir(workDir); err != nil {
				t.Fatalf("Chdir() error = %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chdir(previousDir); err != nil {
					t.Fatalf("restore dir: %v", err)
				}
			})

			path, err := manager.FindAgent(tt.agentName)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindAgent() error = %v", err)
			}
			if resolvedPath(t, path) != resolvedPath(t, expected) {
				t.Fatalf("path = %q, want %q", path, expected)
			}
		})
	}
}

func TestNewManagerConfigPathError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.CacheDir = t.TempDir()

	configHome := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(configHome, []byte("data"), 0o600); err != nil {
		t.Fatalf("write config home file: %v", err)
	}
	cacheHome := t.TempDir()

	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	t.Setenv("HOME", t.TempDir())

	if _, err := source.NewManager(cfg); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "failed to get config path") {
		t.Fatalf("error = %q, want failed to get config path", err.Error())
	}
}

func TestManagerUpdateRepositoriesError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{
		"broken": {URL: filepath.Join(t.TempDir(), "missing")},
	}
	cfg.Agents.LocalPaths = []string{}
	manager := newTestManager(t, cfg)

	err := manager.UpdateRepositories(false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to update some repositories") {
		t.Fatalf("error = %q, want update failure", err.Error())
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error = %q, want repository name", err.Error())
	}
}

func TestManagerGetSearchPathsWithRepoAndConfigDir(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{
		"broken": {URL: filepath.Join(t.TempDir(), "missing")},
	}
	cfg.Agents.LocalPaths = []string{}

	manager := newTestManager(t, cfg)

	localPath := t.TempDir()
	cfg.Agents.LocalPaths = []string{localPath}

	workDir := t.TempDir()
	agentsDir := setupAgentsDir(t, workDir)
	withWorkDir(t, workDir)

	configHome := os.Getenv("XDG_CONFIG_HOME")
	configAgentsDir := filepath.Join(configHome, "squad", "agents")
	if err := os.MkdirAll(configAgentsDir, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create config agents dir: %v", err)
	}

	paths, err := manager.GetSearchPaths()
	if err != nil {
		t.Fatalf("GetSearchPaths() error = %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths count = %d, want 3", len(paths))
	}

	if resolvedPath(t, paths[0]) != resolvedPath(t, agentsDir) {
		t.Fatalf("first path = %q, want %q", paths[0], agentsDir)
	}
	if resolvedPath(t, paths[1]) != resolvedPath(t, localPath) {
		t.Fatalf("second path = %q, want %q", paths[1], localPath)
	}
	if resolvedPath(t, paths[2]) != resolvedPath(t, configAgentsDir) {
		t.Fatalf("third path = %q, want %q", paths[2], configAgentsDir)
	}
}

func TestManagerGetSearchPathsCachedRepo(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{
		"test-agents": {URL: "https://example.com/org/test-agents.git"},
	}
	cfg.Agents.LocalPaths = []string{}

	manager := newTestManager(t, cfg)

	// Pre-populate the cache directory so CachePath finds it
	cacheDir, err := config.AgentsCacheDir()
	if err != nil {
		t.Fatalf("AgentsCacheDir() error = %v", err)
	}
	ops := source.NewGitOperations(cacheDir)
	cachedPath, _ := ops.CachePath("https://example.com/org/test-agents.git")
	if cachedPath != "" {
		t.Fatalf("expected no cached path before setup")
	}

	// Create the expected cache directory
	gitURL := "https://example.com/org/test-agents.git"
	expected := expectedCachePath(cacheDir, gitURL)
	if err := os.MkdirAll(expected, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create cached repo dir: %v", err)
	}

	// Use a work dir without a local agents/ directory
	workDir := t.TempDir()
	withWorkDir(t, workDir)

	paths, err := manager.GetSearchPaths()
	if err != nil {
		t.Fatalf("GetSearchPaths() error = %v", err)
	}

	found := false
	for _, p := range paths {
		if resolvedPath(t, p) == resolvedPath(t, expected) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cached repo path %q not found in search paths: %v", expected, paths)
	}
}

func TestManagerSaveConfigWriteError(t *testing.T) {
	cfg := config.Defaults()
	cfg.Agents.Repositories = map[string]config.RepoSpec{}
	cfg.Agents.LocalPaths = []string{}
	manager := newTestManager(t, cfg)

	configFile := configPath(t)
	if err := os.MkdirAll(configFile, config.DirPermReadWriteExec); err != nil {
		t.Fatalf("create config file dir: %v", err)
	}

	err := manager.AddRepository("repo", "https://example.com/org/repo.git", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to write config") {
		t.Fatalf("error = %q, want failed to write config", err.Error())
	}
}
