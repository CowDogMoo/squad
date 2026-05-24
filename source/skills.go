package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/config"
	"gopkg.in/yaml.v3"
)

// SkillsManager handles the registration, caching, and update of skill
// catalog sources (git repositories and local directories). Mirrors
// [Manager] for agents but operates on cfg.Skills and the dedicated skills
// cache dir.
type SkillsManager struct {
	cfg        *config.Config
	configPath string
	gitOps     *GitOperations
}

// NewSkillsManager returns a manager bound to cfg. The skills cache dir
// from cfg.Skills.CacheDir wins; otherwise the default XDG location.
func NewSkillsManager(cfg *config.Config) (*SkillsManager, error) {
	configPath, err := config.ConfigFile("config.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to get config path: %w", err)
	}

	cacheDir := cfg.Skills.CacheDir
	if cacheDir == "" {
		cacheDir, err = config.SkillsCacheDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get skills cache dir: %w", err)
		}
	}

	return &SkillsManager{
		cfg:        cfg,
		configPath: configPath,
		gitOps:     NewGitOperations(cacheDir),
	}, nil
}

// AddRepository registers a git URL under name. Cloning happens on the
// next call to UpdateRepositories (or implicitly on first list).
func (m *SkillsManager) AddRepository(name, gitURL string) error {
	if !IsGitURL(gitURL) {
		return fmt.Errorf("invalid git URL: %s", gitURL)
	}
	if m.cfg.Skills.Repositories == nil {
		m.cfg.Skills.Repositories = make(map[string]string)
	}
	if existing, ok := m.cfg.Skills.Repositories[name]; ok {
		if existing == gitURL {
			return fmt.Errorf("skill repository %q already configured with URL %s", name, gitURL)
		}
		return fmt.Errorf("skill repository %q already exists with URL %s (use remove first)", name, existing)
	}
	m.cfg.Skills.Repositories[name] = gitURL
	return m.saveConfig()
}

// AddLocalPath registers an absolute local directory as a skills catalog
// source. Each immediate subdirectory is treated as a skill.
func (m *SkillsManager) AddLocalPath(path string) error {
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
	for _, existing := range m.cfg.Skills.LocalPaths {
		if existing == absPath {
			return fmt.Errorf("skill path already configured: %s", absPath)
		}
	}
	m.cfg.Skills.LocalPaths = append(m.cfg.Skills.LocalPaths, absPath)
	return m.saveConfig()
}

// RemoveSource removes either a registered repository (by name) or a
// registered local path (by absolute or original form).
func (m *SkillsManager) RemoveSource(nameOrPath string) error {
	if _, ok := m.cfg.Skills.Repositories[nameOrPath]; ok {
		delete(m.cfg.Skills.Repositories, nameOrPath)
		return m.saveConfig()
	}
	absPath, _ := filepath.Abs(nameOrPath)
	for i, path := range m.cfg.Skills.LocalPaths {
		if path == nameOrPath || path == absPath {
			m.cfg.Skills.LocalPaths = append(
				m.cfg.Skills.LocalPaths[:i],
				m.cfg.Skills.LocalPaths[i+1:]...,
			)
			return m.saveConfig()
		}
	}
	return fmt.Errorf("skill source not found: %s", nameOrPath)
}

// UpdateRepositories pulls latest from every configured git repository
// and reports any failures collectively without aborting the batch.
func (m *SkillsManager) UpdateRepositories() error {
	var errs []string
	for name, url := range m.cfg.Skills.Repositories {
		fmt.Printf("Updating skill repo %s (%s)...\n", name, url)
		if _, err := m.gitOps.CloneOrUpdate(url); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to update some skill repositories:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// CatalogPaths returns the local-path + cached-repo directories that
// skill.Discover should scan as catalog scope. Repositories that have not
// been cloned yet are silently omitted; callers should run UpdateRepositories
// first if they need fresh content.
func (m *SkillsManager) CatalogPaths() []string {
	var paths []string
	for _, path := range m.cfg.Skills.LocalPaths {
		if stat, err := os.Stat(path); err == nil && stat.IsDir() {
			paths = append(paths, path)
		}
	}
	for _, url := range m.cfg.Skills.Repositories {
		if repoPath, ok := m.gitOps.CachePath(url); ok {
			paths = append(paths, repoPath)
		}
	}
	return paths
}

// EnsureRepositoriesCloned guarantees that every configured repo is
// present in the cache before catalog discovery runs. Failure to clone
// any single repo is reported but does not abort the batch.
func (m *SkillsManager) EnsureRepositoriesCloned() error {
	var errs []string
	for name, url := range m.cfg.Skills.Repositories {
		if _, ok := m.gitOps.CachePath(url); ok {
			continue
		}
		if _, err := m.gitOps.CloneOrUpdate(url); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to clone some skill repositories:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

func (m *SkillsManager) saveConfig() error {
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
