package browser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"amazon", false},
		{"a", false},
		{"amazon-prod", false},
		{"amazon_prod", false},
		{"client-123", false},
		{"Amazon", true},      // uppercase
		{"-amazon", true},     // leading hyphen
		{"amazon-", true},     // trailing hyphen
		{"amazon.com", true},  // dot
		{"amazon/cart", true}, // slash
		{"", true},            // empty
		{".", true},
		{"..", true},
		{"a b", true}, // space
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateName(tc.name)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateName(%q) err=%v, wantErr=%v", tc.name, err, tc.wantErr)
			}
			if tc.wantErr && err != nil && !errors.Is(err, ErrInvalidName) {
				t.Fatalf("ValidateName(%q) returned %v, want errors.Is ErrInvalidName", tc.name, err)
			}
		})
	}
}

// withRoot redirects browser.Root() to a temp dir for the duration of t.
// Uses XDG_DATA_HOME since Root() consults it before falling back to $HOME.
func withRoot(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	want := filepath.Join(tmp, "squad", "browser-profiles")
	if got := Root(); got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
	return want
}

func TestProfileDirCreatesLazily(t *testing.T) {
	root := withRoot(t)
	got, err := ProfileDir("amazon")
	if err != nil {
		t.Fatalf("ProfileDir err: %v", err)
	}
	want := filepath.Join(root, "amazon")
	if got != want {
		t.Fatalf("ProfileDir = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("expected dir created at %s: %v", want, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", want)
	}
	// 0o700 means owner-only on POSIX. Skip the bit check on Windows.
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("profile dir perms = %o, want owner-only (0700)", info.Mode().Perm())
	}
}

func TestProfileDirRejectsInvalidName(t *testing.T) {
	withRoot(t)
	if _, err := ProfileDir("Bad Name"); err == nil {
		t.Fatal("expected error for invalid name, got nil")
	}
}

func TestExists(t *testing.T) {
	withRoot(t)
	if Exists("amazon") {
		t.Fatal("Exists should be false before creation")
	}
	if _, err := ProfileDir("amazon"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !Exists("amazon") {
		t.Fatal("Exists should be true after creation")
	}
	if Exists("Bad-Name-NOPE") {
		t.Fatal("Exists should be false for invalid name even if dir exists")
	}
}

func TestListEmptyAndPopulated(t *testing.T) {
	withRoot(t)
	profiles, err := List()
	if err != nil {
		t.Fatalf("List on empty root: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("List on empty root = %v, want []", profiles)
	}

	for _, n := range []string{"amazon", "github", "calendar"} {
		if _, err := ProfileDir(n); err != nil {
			t.Fatalf("seed %s: %v", n, err)
		}
	}
	// Also drop a non-profile-looking entry that List() must skip.
	if err := os.MkdirAll(filepath.Join(Root(), "Not.Valid"), 0o700); err != nil {
		t.Fatalf("seed invalid: %v", err)
	}

	profiles, err = List()
	if err != nil {
		t.Fatalf("List after seed: %v", err)
	}
	if len(profiles) != 3 {
		t.Fatalf("List len = %d, want 3 (got: %+v)", len(profiles), profiles)
	}
	wantOrder := []string{"amazon", "calendar", "github"}
	for i, p := range profiles {
		if p.Name != wantOrder[i] {
			t.Fatalf("profile[%d].Name = %q, want %q", i, p.Name, wantOrder[i])
		}
		if !strings.HasSuffix(p.Dir, "/"+p.Name) {
			t.Fatalf("profile[%d].Dir = %q, expected to end with /%s", i, p.Dir, p.Name)
		}
	}
}

func TestDeleteHappyPath(t *testing.T) {
	withRoot(t)
	if _, err := ProfileDir("amazon"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := Delete("amazon"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if Exists("amazon") {
		t.Fatal("profile should not exist after Delete")
	}
}

func TestDeleteMissingProfile(t *testing.T) {
	withRoot(t)
	err := Delete("never-existed")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Delete on missing profile: err=%v, want ErrProfileNotFound", err)
	}
}

func TestDeleteRejectsInvalidName(t *testing.T) {
	withRoot(t)
	if err := Delete("Bad Name"); err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestRootFallsBackToHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", tmpHome)
	want := filepath.Join(tmpHome, ".local", "share", "squad", "browser-profiles")
	if got := Root(); got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
}

func TestListReturnsErrorWhenRootIsFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	root := Root()
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := os.WriteFile(root, []byte(""), 0o600); err != nil {
		t.Fatalf("seed root as file: %v", err)
	}
	if _, err := List(); err == nil {
		t.Fatal("List should error when root is a file")
	}
}

func TestDeleteNonDirectory(t *testing.T) {
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	// Put a file where the profile dir would normally be.
	if err := os.WriteFile(filepath.Join(root, "amazon"), []byte(""), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	err := Delete("amazon")
	if err == nil || !strings.Contains(err.Error(), "non-directory") {
		t.Fatalf("Delete on a file should error, got: %v", err)
	}
}

func TestProfileDirMkdirAllFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	// Put a file at the path Root() will try to MkdirAll under.
	root := Root()
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := os.WriteFile(root, []byte(""), 0o600); err != nil {
		t.Fatalf("seed file in root path: %v", err)
	}
	if _, err := ProfileDir("amazon"); err == nil {
		t.Fatal("ProfileDir should error when root path is a file")
	}
}

func TestListSkipsNonDirEntries(t *testing.T) {
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	// File instead of dir — must be skipped.
	if err := os.WriteFile(filepath.Join(root, "stray-file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	profiles, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 0 {
		t.Fatalf("List should ignore non-directory entries, got: %+v", profiles)
	}
}
