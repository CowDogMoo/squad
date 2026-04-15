package source_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cowdogmoo/squad/source"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "https url",
			input: "https://github.com/org/repo.git",
			want:  true,
		},
		{
			name:  "http url",
			input: "http://github.com/org/repo",
			want:  true,
		},
		{
			name:  "git ssh url",
			input: "git@github.com:org/repo.git",
			want:  true,
		},
		{
			name:  "suffix git",
			input: "example.com/repo.git",
			want:  true,
		},
		{
			name:  "plain path",
			input: "example.com/repo",
			want:  false,
		},
		{
			name:  "ssh scheme",
			input: "ssh://github.com/org/repo",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := source.IsGitURL(tt.input)
			if got != tt.want {
				t.Errorf("IsGitURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGitOperationsCloneOrUpdate(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) (string, string, string)
		wantErr    string
		wantReadme bool
	}{
		{
			name: "clone new repo",
			setup: func(t *testing.T) (string, string, string) {
				repoDir := createLocalRepo(t)
				cacheDir := t.TempDir()
				expected := expectedCachePath(cacheDir, repoDir)
				return repoDir, cacheDir, expected
			},
			wantReadme: true,
		},
		{
			name: "cached non repo",
			setup: func(t *testing.T) (string, string, string) {
				gitURL := "https://example.com/agents/alpha.git"
				cacheDir := t.TempDir()
				expected := expectedCachePath(cacheDir, gitURL)
				if err := os.MkdirAll(expected, 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				return gitURL, cacheDir, expected
			},
		},
		{
			name: "cached trailing slash url",
			setup: func(t *testing.T) (string, string, string) {
				gitURL := "https://example.com/org/"
				cacheDir := t.TempDir()
				expected := expectedCachePath(cacheDir, gitURL)
				if err := os.MkdirAll(expected, 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				return gitURL, cacheDir, expected
			},
		},
		{
			name: "cached git repo",
			setup: func(t *testing.T) (string, string, string) {
				gitURL := "https://example.com/org/repo.git"
				cacheDir := t.TempDir()
				expected := expectedCachePath(cacheDir, gitURL)
				initRepoAt(t, expected)
				return gitURL, cacheDir, expected
			},
			wantReadme: true,
		},
		{
			name: "clone missing repo",
			setup: func(t *testing.T) (string, string, string) {
				gitURL := filepath.Join(t.TempDir(), "missing")
				cacheDir := t.TempDir()
				return gitURL, cacheDir, ""
			},
			wantErr: "failed to clone repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitURL, cacheDir, expectedPath := tt.setup(t)
			ops := source.NewGitOperations(cacheDir)

			path, err := ops.CloneOrUpdate(gitURL)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
				}
				if path != "" {
					t.Fatalf("path = %q, want empty", path)
				}
				return
			}

			if err != nil {
				t.Fatalf("CloneOrUpdate() error = %v", err)
			}
			if path != expectedPath {
				t.Fatalf("path = %q, want %q", path, expectedPath)
			}
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("stat path error = %v", statErr)
			}
			if tt.wantReadme {
				readmePath := filepath.Join(path, "README.md")
				if _, statErr := os.Stat(readmePath); statErr != nil {
					t.Fatalf("readme not cloned: %v", statErr)
				}
			}
		})
	}
}

func TestGitOperationsCachePath(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, cacheDir string) string
		wantOK bool
	}{
		{
			name: "cached directory exists",
			setup: func(t *testing.T, cacheDir string) string {
				gitURL := "https://example.com/org/repo.git"
				expected := expectedCachePath(cacheDir, gitURL)
				if err := os.MkdirAll(expected, 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				return gitURL
			},
			wantOK: true,
		},
		{
			name: "not cached",
			setup: func(t *testing.T, cacheDir string) string {
				return "https://example.com/org/missing.git"
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheDir := t.TempDir()
			gitURL := tt.setup(t, cacheDir)
			ops := source.NewGitOperations(cacheDir)

			path, ok := ops.CachePath(gitURL)
			if ok != tt.wantOK {
				t.Fatalf("CachePath() ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantOK {
				expected := expectedCachePath(cacheDir, gitURL)
				if path != expected {
					t.Fatalf("CachePath() path = %q, want %q", path, expected)
				}
			} else if path != "" {
				t.Fatalf("CachePath() path = %q, want empty", path)
			}
		})
	}
}

func expectedCachePath(cacheDir, gitURL string) string {
	cleanURL := strings.TrimPrefix(gitURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	cleanURL = strings.TrimPrefix(cleanURL, "git@")
	cleanURL = strings.ReplaceAll(cleanURL, ":", "/")
	cleanURL = strings.TrimSuffix(cleanURL, ".git")

	hash := sha256.Sum256([]byte(gitURL))
	hashStr := fmt.Sprintf("%x", hash)[:12]

	parts := strings.Split(cleanURL, "/")
	name := parts[len(parts)-1]
	if name == "" && len(parts) > 1 {
		name = parts[len(parts)-2]
	}

	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s", name, hashStr))
}

func initRepoAt(t *testing.T, repoDir string) {
	t.Helper()

	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit() error = %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree() error = %v", err)
	}

	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, err = worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func createLocalRepo(t *testing.T) string {
	t.Helper()

	repoDir := t.TempDir()
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit() error = %v", err)
	}

	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree() error = %v", err)
	}

	if _, err := worktree.Add("README.md"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	_, err = worktree.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Tester",
			Email: "tester@example.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	return repoDir
}
