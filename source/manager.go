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

package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/config"
	"gopkg.in/yaml.v3"
)

// Manager handles agent source operations.

type Manager struct {
	cfg        *config.Config
	configPath string
	gitOps     *GitOperations
}

// NewManager creates a new agent source manager.
func NewManager(cfg *config.Config) (*Manager, error) {
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}

	cacheDir := cfg.Agents.CacheDir
	if cacheDir == "" {
		cacheDir, err = config.AgentsCacheDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get agents cache dir: %w", err)
		}
	}

	return &Manager{
		cfg:        cfg,
		configPath: configPath,
		gitOps:     NewGitOperations(cacheDir),
	}, nil
}

// AddRepository adds a git repository as an agent source.
func (m *Manager) AddRepository(name, gitURL string) error {
	if !IsGitURL(gitURL) {
		return fmt.Errorf("invalid git URL: %s", gitURL)
	}

	if m.cfg.Agents.Repositories == nil {
		m.cfg.Agents.Repositories = make(map[string]string)
	}

	if existing, ok := m.cfg.Agents.Repositories[name]; ok {
		if existing == gitURL {
			return fmt.Errorf("repository %q already configured with URL %s", name, gitURL)
		}
		return fmt.Errorf("repository %q already exists with URL %s (use remove first)", name, existing)
	}

	m.cfg.Agents.Repositories[name] = gitURL
	return m.saveConfig()
}

// AddLocalPath adds a local directory as an agent source.
func (m *Manager) AddLocalPath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %s", absPath)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", absPath)
	}

	for _, existing := range m.cfg.Agents.LocalPaths {
		if existing == absPath {
			return fmt.Errorf("path already configured: %s", absPath)
		}
	}

	m.cfg.Agents.LocalPaths = append(m.cfg.Agents.LocalPaths, absPath)
	return m.saveConfig()
}

// RemoveSource removes a repository by name or a local path.
func (m *Manager) RemoveSource(nameOrPath string) error {
	// Try to remove as repository name first
	if _, ok := m.cfg.Agents.Repositories[nameOrPath]; ok {
		delete(m.cfg.Agents.Repositories, nameOrPath)
		return m.saveConfig()
	}

	// Try to remove as local path
	absPath, _ := filepath.Abs(nameOrPath)
	for i, path := range m.cfg.Agents.LocalPaths {
		if path == nameOrPath || path == absPath {
			m.cfg.Agents.LocalPaths = append(
				m.cfg.Agents.LocalPaths[:i],
				m.cfg.Agents.LocalPaths[i+1:]...,
			)
			return m.saveConfig()
		}
	}

	return fmt.Errorf("source not found: %s", nameOrPath)
}

// UpdateRepositories pulls latest from all configured git repositories.
func (m *Manager) UpdateRepositories() error {
	var errs []string
	for name, url := range m.cfg.Agents.Repositories {
		fmt.Printf("Updating %s (%s)...\n", name, url)
		if _, err := m.gitOps.CloneOrUpdate(url); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to update some repositories:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// GetSearchPaths returns all directories to search for agents in priority order.
func (m *Manager) GetSearchPaths() ([]string, error) {
	var paths []string

	// 1. Local ./agents directory (highest priority for development)
	if stat, err := os.Stat("agents"); err == nil && stat.IsDir() {
		if abs, err := filepath.Abs("agents"); err == nil {
			paths = append(paths, abs)
		}
	}

	// 2. Configured local paths
	for _, path := range m.cfg.Agents.LocalPaths {
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			paths = append(paths, path)
		}
	}

	// 3. Cached git repositories (use local cache only, no network access)
	for _, url := range m.cfg.Agents.Repositories {
		if repoPath, ok := m.gitOps.CachePath(url); ok {
			paths = append(paths, repoPath)
		}
	}

	// 4. User config agents directories from XDG paths (fallback)
	for _, configDir := range config.GetConfigDirs() {
		agentsDir := filepath.Join(configDir, "agents")
		if stat, err := os.Stat(agentsDir); err == nil && stat.IsDir() {
			paths = append(paths, agentsDir)
		}
	}

	return paths, nil
}

// ListAgents returns all available agents across all sources.
func (m *Manager) ListAgents() ([]AgentInfo, error) {
	paths, err := m.GetSearchPaths()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var agents []AgentInfo

	for _, searchPath := range paths {
		entries, err := os.ReadDir(searchPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			agentDir := filepath.Join(searchPath, entry.Name())
			manifestPath := filepath.Join(agentDir, "agent.yaml")
			if _, err := os.Stat(manifestPath); err != nil {
				continue
			}
			if seen[entry.Name()] {
				continue // first occurrence wins
			}
			seen[entry.Name()] = true

			info := AgentInfo{
				Name:   entry.Name(),
				Path:   agentDir,
				Source: searchPath,
			}
			// Try to read manifest for description
			if data, err := os.ReadFile(manifestPath); err == nil {
				var manifest struct {
					Version     string `yaml:"version"`
					Description string `yaml:"description"`
				}
				if err := yaml.Unmarshal(data, &manifest); err == nil {
					info.Version = manifest.Version
					info.Description = manifest.Description
				}
			}
			agents = append(agents, info)
		}
	}

	return agents, nil
}

// FindAgent returns the path to an agent by name.
func (m *Manager) FindAgent(name string) (string, error) {
	paths, err := m.GetSearchPaths()
	if err != nil {
		return "", err
	}

	for _, searchPath := range paths {
		agentDir := filepath.Join(searchPath, name)
		manifestPath := filepath.Join(agentDir, "agent.yaml")
		if _, err := os.Stat(manifestPath); err == nil {
			return agentDir, nil
		}
	}

	return "", fmt.Errorf("agent not found: %s", name)
}

func (m *Manager) saveConfig() error {
	data, err := yaml.Marshal(m.cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(m.configPath), config.DirPermReadWriteExec); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(m.configPath, data, config.FilePermReadWrite); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// AgentInfo contains metadata about an available agent.
type AgentInfo struct {
	Name        string
	Version     string
	Description string
	Path        string
	Source      string
}
