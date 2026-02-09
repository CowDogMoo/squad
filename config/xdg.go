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

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

func getConfigHome() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return configHome
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config")
	}
	return ""
}

func getCacheHome() string {
	if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
		return cacheHome
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache")
	}
	return ""
}

// GetConfigDirs returns all config directories to search (in priority order).
func GetConfigDirs() []string {
	return getConfigDirs()
}

func getConfigDirs() []string {
	dirs := []string{}

	if configHome := getConfigHome(); configHome != "" {
		dirs = append(dirs, filepath.Join(configHome, "squad"))
	}

	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".squad"))
	}

	if runtime.GOOS == "linux" || runtime.GOOS == "freebsd" || runtime.GOOS == "openbsd" {
		if xdgConfigDirs := os.Getenv("XDG_CONFIG_DIRS"); xdgConfigDirs != "" {
			for _, dir := range filepath.SplitList(xdgConfigDirs) {
				if dir != "" {
					dirs = append(dirs, filepath.Join(dir, "squad"))
				}
			}
		} else {
			dirs = append(dirs, filepath.Join("/etc", "xdg", "squad"))
		}
	}

	return dirs
}

// ConfigFile returns the path for creating a new config file.
func ConfigFile(filename string) (string, error) {
	configHome := getConfigHome()
	if configHome == "" {
		return "", os.ErrNotExist
	}

	configPath := filepath.Join(configHome, "squad", filename)
	configDir := filepath.Dir(configPath)

	if err := os.MkdirAll(configDir, DirPermReadWriteExec); err != nil {
		return "", err
	}

	return configPath, nil
}

// CacheFile returns the path for a cache file.
func CacheFile(filename string) (string, error) {
	cacheHome := getCacheHome()
	if cacheHome == "" {
		return "", os.ErrNotExist
	}

	cachePath := filepath.Join(cacheHome, "squad", filename)
	cacheDir := filepath.Dir(cachePath)

	if err := os.MkdirAll(cacheDir, DirPermReadWriteExec); err != nil {
		return "", err
	}

	return cachePath, nil
}

// AgentsCacheDir returns the directory for caching cloned agent repositories.
func AgentsCacheDir() (string, error) {
	cacheHome := getCacheHome()
	if cacheHome == "" {
		return "", os.ErrNotExist
	}

	cacheDir := filepath.Join(cacheHome, "squad", "agents")
	if err := os.MkdirAll(cacheDir, DirPermReadWriteExec); err != nil {
		return "", err
	}

	return cacheDir, nil
}
