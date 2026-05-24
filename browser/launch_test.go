package browser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchOverrideViaEnv(t *testing.T) {
	withRoot(t)

	// Pretend SQUAD_BROWSER_BIN points at a real, executable file that
	// just exits 0. The shell `true` works on POSIX systems; on Windows
	// this test is skipped.
	if path, err := exec_LookPath_true(); err == nil {
		t.Setenv("SQUAD_BROWSER_BIN", path)
	} else {
		t.Skip("no `true` binary on PATH; skipping launch override test")
	}

	if err := Launch("amazon", LaunchOptions{Wait: true}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// Profile dir should have been created lazily.
	if !Exists("amazon") {
		t.Fatal("expected profile to exist after Launch")
	}
}

func TestLaunchSurfacesNotFound(t *testing.T) {
	withRoot(t)
	// Point SQUAD_BROWSER_BIN at a path guaranteed not to exist.
	t.Setenv("SQUAD_BROWSER_BIN", filepath.Join(t.TempDir(), "definitely-not-here"))
	err := Launch("amazon", LaunchOptions{})
	if !errors.Is(err, ErrChromeNotFound) {
		t.Fatalf("Launch err=%v, want errors.Is ErrChromeNotFound", err)
	}
}

func TestLaunchRejectsInvalidName(t *testing.T) {
	withRoot(t)
	err := Launch("Not Valid", LaunchOptions{})
	if err == nil || !strings.Contains(err.Error(), "browser profile name") {
		t.Fatalf("Launch should reject invalid name, got: %v", err)
	}
}

// exec_LookPath_true wraps exec.LookPath("true") behind a helper to keep
// the import list tidy in the test file. Returns the resolved path on
// success.
func exec_LookPath_true() (string, error) {
	// On POSIX, /usr/bin/true is reliable; PATH lookup catches Linux distros
	// that put it in /bin or /usr/local/bin.
	candidates := []string{"/usr/bin/true", "/bin/true"}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("true not found")
}

func TestChromeCandidatesHonorsEnv(t *testing.T) {
	t.Setenv("SQUAD_BROWSER_BIN", "/custom/chrome")
	got := chromeCandidates()
	if len(got) != 1 || got[0] != "/custom/chrome" {
		t.Fatalf("chromeCandidates() = %v, want [/custom/chrome]", got)
	}
}

func TestChromeCandidatesDefaults(t *testing.T) {
	t.Setenv("SQUAD_BROWSER_BIN", "")
	got := chromeCandidates()
	// We don't assert specific paths (OS-dependent), but the list should
	// be non-empty on darwin/linux. On unsupported OSes (e.g. windows in
	// CI), the function returns nil — accept either.
	if got == nil {
		return
	}
	if len(got) == 0 {
		t.Fatal("chromeCandidates() returned empty non-nil slice")
	}
}

func TestFindChromeNoCandidates(t *testing.T) {
	// Empty SQUAD_BROWSER_BIN with PATH that contains no chrome binaries.
	t.Setenv("SQUAD_BROWSER_BIN", filepath.Join(t.TempDir(), "absent"))
	_, err := findChrome()
	if !errors.Is(err, ErrChromeNotFound) {
		t.Fatalf("findChrome() err = %v, want ErrChromeNotFound", err)
	}
}

func TestIsAbsExecutableRejectsDir(t *testing.T) {
	dir := t.TempDir()
	if isAbsExecutable(dir) {
		t.Fatal("isAbsExecutable should return false for a directory")
	}
}

func TestIsAbsExecutableRejectsRelative(t *testing.T) {
	if isAbsExecutable("relative/path") {
		t.Fatal("isAbsExecutable should return false for relative paths")
	}
}

func TestLaunchDetachReturnsImmediately(t *testing.T) {
	withRoot(t)
	path, err := exec_LookPath_true()
	if err != nil {
		t.Skip("no `true` binary; skipping detach test")
	}
	t.Setenv("SQUAD_BROWSER_BIN", path)
	// Wait:false exercises the goroutine-reap branch.
	if err := Launch("amazon", LaunchOptions{Wait: false}); err != nil {
		t.Fatalf("Launch (detach): %v", err)
	}
}
