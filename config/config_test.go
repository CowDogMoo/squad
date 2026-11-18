package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
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
	if len(cfg.Model.ReasoningPrefixes) != 1 || cfg.Model.ReasoningPrefixes[0] != "gpt-5" {
		t.Fatalf("unexpected reasoning prefixes: %+v", cfg.Model.ReasoningPrefixes)
	}
	if cfg.Run.MaxRetries != 3 {
		t.Fatalf("expected run.max_retries default = 3, got %d", cfg.Run.MaxRetries)
	}
	if cfg.Provider.OllamaTimeout != 0 {
		t.Fatalf("expected provider.ollama_timeout default = 0 (the ollama package default), got %s", cfg.Provider.OllamaTimeout)
	}
}

// TestLoadFromPathOllamaTimeout checks that provider.ollama_timeout parses as
// a duration from the config file and that SQUAD_PROVIDER_OLLAMA_TIMEOUT
// overrides it.
func TestLoadFromPathOllamaTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("provider:\n  ollama_timeout: 45m\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, v, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Provider.OllamaTimeout != 45*time.Minute {
		t.Fatalf("ollama_timeout from file = %s, want 45m0s", cfg.Provider.OllamaTimeout)
	}
	if got := v.GetDuration("provider.ollama_timeout"); got != 45*time.Minute {
		t.Fatalf("viper ollama_timeout = %s, want 45m0s", got)
	}

	t.Setenv("SQUAD_PROVIDER_OLLAMA_TIMEOUT", "2h")
	cfg, v, err = LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Provider.OllamaTimeout != 2*time.Hour {
		t.Fatalf("ollama_timeout from env = %s, want 2h0m0s", cfg.Provider.OllamaTimeout)
	}
	if got := v.GetDuration("provider.ollama_timeout"); got != 2*time.Hour {
		t.Fatalf("viper ollama_timeout from env = %s, want 2h0m0s", got)
	}
}

// TestLoadFromPathRejectsOllamaTimeoutUnderASecond checks that a
// provider.ollama_timeout under 1s fails the load with an error that asks
// for a unit, from the config file or the environment. A bare 3600 meant as
// seconds decodes as 3.6µs, which would fail every Ollama request.
func TestLoadFromPathRejectsOllamaTimeoutUnderASecond(t *testing.T) {
	tests := []struct {
		name string
		file string
		env  string
	}{
		{name: "bare integer in file", file: "provider:\n  ollama_timeout: 3600\n"},
		{name: "sub-second duration in file", file: "provider:\n  ollama_timeout: 500ms\n"},
		{name: "negative duration in file", file: "provider:\n  ollama_timeout: -5m\n"},
		{name: "bare integer in env", file: "provider:\n  ollama_timeout: 45m\n", env: "3600"},
		{name: "sub-second duration in env", file: "provider:\n  ollama_timeout: 45m\n", env: "500ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tt.file), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			if tt.env != "" {
				t.Setenv("SQUAD_PROVIDER_OLLAMA_TIMEOUT", tt.env)
			}
			_, _, err := LoadFromPath(path)
			if err == nil {
				t.Fatal("LoadFromPath accepted an ollama_timeout under 1s")
			}
			for _, want := range []string{"provider.ollama_timeout", "under 1s", "unit", "30m"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// TestLoadFromPathAcceptsOllamaTimeoutZeroAndOneSecond checks the edges the
// under-1s check must let through: an explicit 0, which keeps the built-in
// default, and exactly 1s.
func TestLoadFromPathAcceptsOllamaTimeoutZeroAndOneSecond(t *testing.T) {
	for raw, want := range map[string]time.Duration{"0": 0, "1s": time.Second} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("provider:\n  ollama_timeout: "+raw+"\n"), 0o644); err != nil {
				t.Fatalf("write config: %v", err)
			}
			cfg, _, err := LoadFromPath(path)
			if err != nil {
				t.Fatalf("LoadFromPath: %v", err)
			}
			if cfg.Provider.OllamaTimeout != want {
				t.Fatalf("ollama_timeout = %s, want %s", cfg.Provider.OllamaTimeout, want)
			}
		})
	}
}

func TestLoadFromPathMaxRetriesEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("run:\n  max_retries: 6\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Run.MaxRetries != 6 {
		t.Fatalf("expected run.max_retries = 6 from file, got %d", cfg.Run.MaxRetries)
	}

	t.Setenv("SQUAD_RUN_MAX_RETRIES", "9")
	cfg, _, err = LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}
	if cfg.Run.MaxRetries != 9 {
		t.Fatalf("expected env override max_retries = 9, got %d", cfg.Run.MaxRetries)
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

	cfg, _, err := LoadFromPath(path)
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

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "text" {
		t.Fatalf("unexpected defaults: %+v", cfg.Log)
	}
}

func TestLoadFromPathMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if _, _, err := LoadFromPath(missing); err == nil {
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

func TestLoadInvalidConfigFile(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	configPath := filepath.Join(baseDir, "squad", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), DirPermReadWriteExec); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("log: ["), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := Load()
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "config load failed") {
		t.Fatalf("error = %q, want config load failed", err.Error())
	}
}

func TestLoadFromPathInvalidConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("model: ["), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := LoadFromPath(configPath)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "config load failed") {
		t.Fatalf("error = %q, want config load failed", err.Error())
	}
}

func TestLoadConfigWithViperSetupError(t *testing.T) {
	_, _, err := loadConfigWithViper(func(*viper.Viper) error {
		return fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "config load failed") {
		t.Fatalf("error = %q, want config load failed", err.Error())
	}
}

// TestLoadFromPath_SkillsLocalPathsListForm is a regression test for the
// schema flip that briefly required skills.local_paths to be a map. The
// canonical form is a YAML list — the same shape users have been writing
// since the field shipped — and Load must accept it without error.
func TestLoadFromPath_SkillsLocalPathsListForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "" +
		"skills:\n" +
		"  repositories:\n" +
		"    personal: https://github.com/CowDogMoo/squad-skills.git\n" +
		"  local_paths:\n" +
		"    - /Users/l/cowdogmoo/squad-skills\n" +
		"    - /opt/shared/skills\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := LoadFromPath(path)
	if err != nil {
		t.Fatalf("LoadFromPath: %v", err)
	}

	want := []string{"/Users/l/cowdogmoo/squad-skills", "/opt/shared/skills"}
	if len(cfg.Skills.LocalPaths) != len(want) {
		t.Fatalf("LocalPaths = %v, want %v", cfg.Skills.LocalPaths, want)
	}
	for i, p := range want {
		if cfg.Skills.LocalPaths[i] != p {
			t.Errorf("LocalPaths[%d] = %q, want %q", i, cfg.Skills.LocalPaths[i], p)
		}
	}
	if cfg.Skills.Repositories["personal"].URL != "https://github.com/CowDogMoo/squad-skills.git" {
		t.Errorf("Repositories[personal] = %+v", cfg.Skills.Repositories["personal"])
	}
}

// TestLoadFromPath_TypeMismatchSurfacesError is a regression test for the
// silent-fallback bug that previously hid type mismatches from the user.
// model.max_tokens expects an int; writing a YAML mapping there forces
// mapstructure to fail at decode time. Load must surface that error
// rather than silently falling back to defaults — which would discard
// the user's entire configuration on one bad field.
func TestLoadFromPath_TypeMismatchSurfacesError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := "model:\n  max_tokens:\n    nested: 5\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := LoadFromPath(path)
	if err == nil {
		t.Fatalf("expected error from type-mismatched field, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("error = %q, want one mentioning unmarshal", err.Error())
	}
}
