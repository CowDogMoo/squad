package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetConfigHome(t *testing.T) {
	baseDir := t.TempDir()
	tests := []struct {
		name string
		xdg  string
		home string
		want string
	}{
		{
			name: "xdg config home",
			xdg:  filepath.Join(baseDir, "xdg"),
			home: filepath.Join(baseDir, "home"),
			want: filepath.Join(baseDir, "xdg"),
		},
		{
			name: "home fallback",
			xdg:  "",
			home: filepath.Join(baseDir, "home"),
			want: filepath.Join(baseDir, "home", ".config"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)
			t.Setenv("USERPROFILE", tt.home)
			if got := getConfigHome(); got != tt.want {
				t.Fatalf("getConfigHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetCacheHome(t *testing.T) {
	baseDir := t.TempDir()
	tests := []struct {
		name string
		xdg  string
		home string
		want string
	}{
		{
			name: "xdg cache home",
			xdg:  filepath.Join(baseDir, "xdg"),
			home: filepath.Join(baseDir, "home"),
			want: filepath.Join(baseDir, "xdg"),
		},
		{
			name: "home fallback",
			xdg:  "",
			home: filepath.Join(baseDir, "home"),
			want: filepath.Join(baseDir, "home", ".cache"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)
			t.Setenv("USERPROFILE", tt.home)
			if got := getCacheHome(); got != tt.want {
				t.Fatalf("getCacheHome() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetConfigDirsXDGConfigDirs(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" && runtime.GOOS != "openbsd" {
		t.Skip("xdg config dirs only applies to unix-like OS")
	}
	baseDir := t.TempDir()
	configHome := filepath.Join(baseDir, "config")
	first := filepath.Join(baseDir, "first")
	second := filepath.Join(baseDir, "second")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CONFIG_DIRS", strings.Join([]string{first, second}, string(filepath.ListSeparator)))
	dirs := GetConfigDirs()
	wantFirst := filepath.Join(configHome, "squad")
	if len(dirs) == 0 || dirs[0] != wantFirst {
		t.Fatalf("expected %q first, got %v", wantFirst, dirs)
	}
	wantDirs := []string{filepath.Join(first, "squad"), filepath.Join(second, "squad")}
	for _, want := range wantDirs {
		found := false
		for _, dir := range dirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in dirs, got %v", want, dirs)
		}
	}
}

func TestGetConfigDirsSkipsEmptyXDGDirs(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "freebsd" && runtime.GOOS != "openbsd" {
		t.Skip("xdg config dirs only applies to unix-like OS")
	}
	baseDir := t.TempDir()
	configHome := filepath.Join(baseDir, "config")
	first := filepath.Join(baseDir, "first")
	second := filepath.Join(baseDir, "second")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CONFIG_DIRS", strings.Join([]string{first, "", second}, string(filepath.ListSeparator)))

	dirs := GetConfigDirs()
	unexpected := filepath.Join("", "squad")
	for _, dir := range dirs {
		if dir == unexpected {
			t.Fatalf("unexpected empty dir entry: %v", dirs)
		}
	}
	wantDirs := []string{filepath.Join(first, "squad"), filepath.Join(second, "squad")}
	for _, want := range wantDirs {
		found := false
		for _, dir := range dirs {
			if dir == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q in dirs, got %v", want, dirs)
		}
	}
}

func TestAgentsCacheDirCreatesDir(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	cacheDir, err := AgentsCacheDir()
	if err != nil {
		t.Fatalf("AgentsCacheDir() error = %v", err)
	}
	if !strings.HasSuffix(cacheDir, filepath.Join("squad", "agents")) {
		t.Fatalf("unexpected cache dir: %s", cacheDir)
	}
	stat, statErr := os.Stat(cacheDir)
	if statErr != nil {
		t.Fatalf("AgentsCacheDir() stat error = %v", statErr)
	}
	if !stat.IsDir() {
		t.Fatalf("cache path is not a dir: %s", cacheDir)
	}
}

func TestConfigCachePathErrors(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		call   func() (string, error)
	}{
		{
			name:   "config file mkdir error",
			envKey: "XDG_CONFIG_HOME",
			call: func() (string, error) {
				return ConfigFile("config.yaml")
			},
		},
		{
			name:   "cache file mkdir error",
			envKey: "XDG_CACHE_HOME",
			call: func() (string, error) {
				return CacheFile("cache.json")
			},
		},
		{
			name:   "agents cache dir mkdir error",
			envKey: "XDG_CACHE_HOME",
			call: func() (string, error) {
				return AgentsCacheDir()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(t.TempDir(), "home")
			if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
				t.Fatalf("write file: %v", err)
			}
			t.Setenv(tt.envKey, filePath)

			if _, err := tt.call(); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestSkillsCacheDir(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	got, err := SkillsCacheDir()
	if err != nil {
		t.Fatalf("SkillsCacheDir() error = %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("squad", "skills")) {
		t.Fatalf("unexpected cache dir: %s", got)
	}
	stat, statErr := os.Stat(got)
	if statErr != nil {
		t.Fatalf("stat error = %v", statErr)
	}
	if !stat.IsDir() {
		t.Fatalf("not a directory: %s", got)
	}
}

func TestSkillsCacheDirNoEnv(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := SkillsCacheDir(); err == nil {
		t.Fatal("expected error when no cache home resolvable")
	}
}

func TestCacheDirEmptyEnv(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if got := CacheDir(); got != "" {
		t.Fatalf("expected empty cache dir, got %q", got)
	}
}

// New tests to cover success paths for ConfigFile/CacheFile and CacheDir with env.
func TestConfigAndCacheFileCreatePaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))

	cfgPath, err := ConfigFile("app.yaml")
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	if want := filepath.Join(base, "cfg", "squad", "app.yaml"); cfgPath != want {
		t.Fatalf("ConfigFile path=%q, want %q", cfgPath, want)
	}
	if _, err := os.Stat(filepath.Dir(cfgPath)); err != nil {
		t.Fatalf("config dir not created: %v", err)
	}

	cachePath, err := CacheFile("cache.json")
	if err != nil {
		t.Fatalf("CacheFile: %v", err)
	}
	if want := filepath.Join(base, "cache", "squad", "cache.json"); cachePath != want {
		t.Fatalf("CacheFile path=%q, want %q", cachePath, want)
	}
	if _, err := os.Stat(filepath.Dir(cachePath)); err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
}

func TestCacheDirWithEnv(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	if got := CacheDir(); got != filepath.Join(base, "cache", "squad") {
		t.Fatalf("CacheDir=%q, want %q", got, filepath.Join(base, "cache", "squad"))
	}
}
