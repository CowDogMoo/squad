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
