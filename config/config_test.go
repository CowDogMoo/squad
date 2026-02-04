package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Log.Level != "info" || cfg.Log.Format != "text" {
		t.Fatalf("unexpected log defaults: %+v", cfg.Log)
	}
	if cfg.Provider.Default != "openai" || cfg.Provider.NumCtx != 32768 {
		t.Fatalf("unexpected provider defaults: %+v", cfg.Provider)
	}
	if cfg.Model.Temperature != 0.2 || cfg.Model.MaxTokens != 1024 {
		t.Fatalf("unexpected model defaults: %+v", cfg.Model)
	}
}

func TestLoadFromPathWithEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "log:\n  level: warn\nmodel:\n  max_tokens: 2048\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("SQUAD_LOG_LEVEL", "debug")

	cfg, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}

	if cfg.Log.Level != "debug" {
		t.Fatalf("expected env override log level, got %q", cfg.Log.Level)
	}
	if cfg.Model.MaxTokens != 2048 {
		t.Fatalf("expected model max tokens from file, got %d", cfg.Model.MaxTokens)
	}
}

func TestConfigFileAndCacheFile(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "cfg")
	cacheHome := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	configPath, err := ConfigFile("config.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if !strings.HasSuffix(configPath, filepath.Join("squad", "config.yaml")) {
		t.Fatalf("unexpected config path: %s", configPath)
	}

	cachePath, err := CacheFile("state.json")
	if err != nil {
		t.Fatalf("CacheFile: %v", err)
	}
	if !strings.HasSuffix(cachePath, filepath.Join("squad", "state.json")) {
		t.Fatalf("unexpected cache path: %s", cachePath)
	}
}

func TestGetConfigDirsIncludesConfigHome(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "cfg")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	dirs := GetConfigDirs()
	if len(dirs) == 0 || dirs[0] != filepath.Join(configHome, "squad") {
		t.Fatalf("expected config home first, got %v", dirs)
	}

	if runtime.GOOS == "linux" && !contains(dirs, filepath.Join("/etc", "xdg", "squad")) {
		t.Fatalf("expected linux default xdg dir present: %v", dirs)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "text" {
		t.Fatalf("unexpected defaults: %+v", cfg.Log)
	}
}

func TestLoadFromPathMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if _, err := LoadFromPath(missing); err == nil {
		t.Fatalf("expected error for missing config file")
	}
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
