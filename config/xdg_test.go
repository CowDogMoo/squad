package config

import (
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
