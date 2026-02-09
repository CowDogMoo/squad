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

// Package source manages agent sources including git repositories and local paths.
package source

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
)

// GitOperations handles git operations for agent repositories.
type GitOperations struct {
	cacheDir string
}

// NewGitOperations returns a GitOperations that caches cloned repositories under cacheDir.
func NewGitOperations(cacheDir string) *GitOperations {
	return &GitOperations{cacheDir: cacheDir}
}

// CloneOrUpdate clones a repository if it doesn't exist, or updates it if it does.
// Returns the local path to the repository.
func (g *GitOperations) CloneOrUpdate(gitURL string) (string, error) {
	repoPath := g.getCachePath(gitURL)

	if stat, err := os.Stat(repoPath); err == nil && stat.IsDir() {
		if err := g.pull(repoPath); err != nil {
			// Pull failed, but we have a cached copy - use it
			return repoPath, nil
		}
		return repoPath, nil
	}

	return g.clone(gitURL, repoPath)
}

// clone clones a repository to the specified path.
func (g *GitOperations) clone(gitURL, repoPath string) (string, error) {
	cloneOpts := &git.CloneOptions{
		URL:      gitURL,
		Progress: os.Stdout,
	}

	if _, err := git.PlainClone(repoPath, false, cloneOpts); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return repoPath, nil
}

// pull pulls the latest changes from the remote.
func (g *GitOperations) pull(repoPath string) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	pullOpts := &git.PullOptions{
		RemoteName: "origin",
		Progress:   os.Stdout,
	}

	if err := w.Pull(pullOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull updates: %w", err)
	}

	return nil
}

// getCachePath generates a cache path for a repository.
func (g *GitOperations) getCachePath(gitURL string) string {
	// Clean the URL for use in path
	cleanURL := strings.TrimPrefix(gitURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	cleanURL = strings.TrimPrefix(cleanURL, "git@")
	cleanURL = strings.ReplaceAll(cleanURL, ":", "/")
	cleanURL = strings.TrimSuffix(cleanURL, ".git")

	// Hash the URL to avoid overly long paths
	hash := sha256.Sum256([]byte(gitURL))
	hashStr := fmt.Sprintf("%x", hash)[:12]

	// Use last component of URL + hash for readability
	parts := strings.Split(cleanURL, "/")
	name := parts[len(parts)-1]
	if name == "" && len(parts) > 1 {
		name = parts[len(parts)-2]
	}

	return filepath.Join(g.cacheDir, fmt.Sprintf("%s-%s", name, hashStr))
}

// IsGitURL returns true if the string looks like a git URL.
func IsGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasSuffix(s, ".git")
}
