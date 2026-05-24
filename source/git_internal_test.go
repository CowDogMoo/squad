package source

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
)

func TestHttpAuthFromEnvNonGitHubURL(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret")
	if got := httpAuthFromEnv("https://gitlab.com/foo/bar.git"); got != nil {
		t.Fatalf("expected nil for non-github URL, got %v", got)
	}
}

func TestHttpAuthFromEnvNoToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	if got := httpAuthFromEnv("https://github.com/foo/bar.git"); got != nil {
		t.Fatalf("expected nil with no token, got %v", got)
	}
}

func TestHttpAuthFromEnvGHToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "from-gh-token")
	t.Setenv("GITHUB_TOKEN", "ignored")
	auth := httpAuthFromEnv("https://github.com/foo/bar.git")
	if auth == nil {
		t.Fatal("expected auth, got nil")
	}
	// BasicAuth.String() formats as "http-basic-auth - <user>:*******".
	if !strings.Contains(auth.String(), "x-access-token") {
		t.Fatalf("expected x-access-token in auth string, got %q", auth.String())
	}
	if name := auth.Name(); name != "http-basic-auth" {
		t.Fatalf("auth.Name() = %q, want http-basic-auth", name)
	}
}

func TestHttpAuthFromEnvGitHubTokenFallback(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "from-github-token")
	auth := httpAuthFromEnv("https://github.com/foo/bar.git")
	if auth == nil {
		t.Fatal("expected auth from GITHUB_TOKEN fallback, got nil")
	}
	if !strings.Contains(auth.String(), "x-access-token") {
		t.Fatalf("expected x-access-token in auth string, got %q", auth.String())
	}
}

func TestHttpAuthFromEnvWWWPrefix(t *testing.T) {
	t.Setenv("GH_TOKEN", "tok")
	if got := httpAuthFromEnv("https://www.github.com/foo/bar.git"); got == nil {
		t.Fatal("expected auth for www.github.com URL, got nil")
	}
}

func TestRemoteURLNil(t *testing.T) {
	if got := remoteURL(nil); got != "" {
		t.Fatalf("remoteURL(nil) = %q, want empty", got)
	}
}

func TestRemoteURLNoOrigin(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	if got := remoteURL(repo); got != "" {
		t.Fatalf("remoteURL on repo without origin = %q, want empty", got)
	}
}

func TestRemoteURLReturnsOrigin(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wantURL := "https://github.com/foo/bar.git"
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{wantURL},
	}); err != nil {
		t.Fatalf("CreateRemote: %v", err)
	}
	if got := remoteURL(repo); got != wantURL {
		t.Fatalf("remoteURL = %q, want %q", got, wantURL)
	}
}
