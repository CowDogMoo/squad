package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/browser"
)

// withBrowserRoot redirects browser.Root() to a temp dir for the test.
func withBrowserRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	return filepath.Join(tmp, "squad", "browser-profiles")
}

func TestNewBrowserCmdHasSubcommands(t *testing.T) {
	cmd := newBrowserCmd()
	wantSubs := []string{"open", "list", "delete", "path"}
	for _, name := range wantSubs {
		if _, _, err := cmd.Find([]string{name}); err != nil {
			t.Errorf("subcommand %q missing from `browser`: %v", name, err)
		}
	}
}

func TestBrowserListEmpty(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserListCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "No browser profiles") {
		t.Errorf("expected empty-state message, got: %s", out)
	}
}

func TestBrowserListPopulated(t *testing.T) {
	withBrowserRoot(t)
	for _, n := range []string{"amazon", "github"} {
		if _, err := browser.ProfileDir(n); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	cmd := newBrowserListCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "amazon") || !strings.Contains(out, "github") {
		t.Errorf("expected list of profiles, got: %s", out)
	}
}

func TestBrowserPathPrintsAndCreates(t *testing.T) {
	root := withBrowserRoot(t)
	cmd := newBrowserPathCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{"amazon"}); err != nil {
		t.Fatalf("path: %v", err)
	}
	got := strings.TrimSpace(stdout.String())
	want := filepath.Join(root, "amazon")
	if got != want {
		t.Errorf("path output = %q, want %q", got, want)
	}
	if !browser.Exists("amazon") {
		t.Error("path should have created the profile dir")
	}
}

func TestBrowserPathInvalidName(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserPathCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"Bad Name"})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestBrowserDeleteRequiresForce(t *testing.T) {
	withBrowserRoot(t)
	if _, err := browser.ProfileDir("amazon"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newBrowserDeleteCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"amazon"})
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("delete without --force should error, got: %v", err)
	}
	if !browser.Exists("amazon") {
		t.Error("profile should still exist when --force is missing")
	}
}

func TestBrowserDeleteWithForce(t *testing.T) {
	withBrowserRoot(t)
	if _, err := browser.ProfileDir("amazon"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cmd := newBrowserDeleteCmd()
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set --force: %v", err)
	}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.RunE(cmd, []string{"amazon"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if browser.Exists("amazon") {
		t.Error("profile should be gone after --force delete")
	}
	if !strings.Contains(stdout.String(), "deleted") {
		t.Errorf("expected confirmation message, got: %s", stdout.String())
	}
}

func TestBrowserDeleteMissingProfile(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserDeleteCmd()
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("set --force: %v", err)
	}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"never-existed"})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %v", err)
	}
}

func TestBrowserDeleteInvalidName(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserDeleteCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"Bad Name"})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !errors.Is(err, browser.ErrInvalidName) {
		t.Errorf("err = %v, want errors.Is ErrInvalidName", err)
	}
}

func TestBrowserOpenRejectsInvalidName(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserOpenCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"Bad Name"})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !errors.Is(err, browser.ErrInvalidName) {
		t.Errorf("err = %v, want errors.Is ErrInvalidName", err)
	}
}

func TestBrowserOpenMissingBinary(t *testing.T) {
	withBrowserRoot(t)
	t.Setenv("SQUAD_BROWSER_BIN", filepath.Join(t.TempDir(), "does-not-exist"))
	cmd := newBrowserOpenCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"amazon", "https://example.com"})
	if !errors.Is(err, browser.ErrChromeNotFound) {
		t.Fatalf("err = %v, want errors.Is ErrChromeNotFound", err)
	}
}
