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
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// GitOperations handles git operations for agent repositories.

type GitOperations struct {
	cacheDir string
}

// NewGitOperations returns a GitOperations that caches cloned repositories under cacheDir.
func NewGitOperations(cacheDir string) *GitOperations {
	return &GitOperations{cacheDir: cacheDir}
}

// CachePath returns the local cache path for a repository URL without
// performing any network operations. It returns the path and true if the
// repository is already cached locally, or empty string and false otherwise.
func (g *GitOperations) CachePath(gitURL string) (string, bool) {
	repoPath := g.getCachePath(gitURL)
	if stat, err := os.Stat(repoPath); err == nil && stat.IsDir() {
		return repoPath, true
	}
	return "", false
}

// CloneOrUpdate clones a repository if it doesn't exist, or updates it if it does.
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
		Auth:     httpAuthFromEnv(gitURL),
	}

	if _, err := git.PlainClone(repoPath, false, cloneOpts); err != nil {
		return "", fmt.Errorf("failed to clone repository: %w", err)
	}

	return repoPath, nil
}

// remoteURL returns the URL of the named-or-first remote on repo, or empty
// string if none can be resolved. Used so pull() can route through the same
// env-token logic as clone().
func remoteURL(repo *git.Repository) string {
	if repo == nil {
		return ""
	}
	remote, err := repo.Remote("origin")
	if err != nil || remote == nil {
		return ""
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

// httpAuthFromEnv returns BasicAuth derived from GH_TOKEN or GITHUB_TOKEN
// for https://github.com URLs, or nil if no token is set or the URL is not
// an HTTPS GitHub URL. go-git does not consult the OS git credential helper,
// so callers must supply Auth explicitly to clone private repositories.
func httpAuthFromEnv(gitURL string) transport.AuthMethod {
	if !strings.HasPrefix(gitURL, "https://github.com/") &&
		!strings.HasPrefix(gitURL, "https://www.github.com/") {
		return nil
	}
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: "x-access-token", Password: token}
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
		Auth:       httpAuthFromEnv(remoteURL(repo)),
	}

	if err := w.Pull(pullOpts); err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("failed to pull updates: %w", err)
	}

	return nil
}

// getCachePath generates a cache path for a repository.
func (g *GitOperations) getCachePath(gitURL string) string {
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

// IsGitURL reports whether s looks like a git URL. file:// URLs are accepted
// for local-repo workflows (testing, on-disk mirrors) — git natively
// understands them and go-git's clone path treats them the same as any
// remote.
func IsGitURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "file://") ||
		strings.HasSuffix(s, ".git")
}
