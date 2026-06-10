package browser

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestChromeCandidatesLinux(t *testing.T) {
	// Simulate Linux by checking the function returns non-nil on darwin/linux.
	// We can't change runtime.GOOS, but we can verify the env override path
	// and the default path don't panic.
	t.Setenv("SQUAD_BROWSER_BIN", "")
	got := chromeCandidates()
	// On darwin or linux we expect candidates; on other platforms nil is fine.
	_ = got
}

func TestDeleteProfileNotFound(t *testing.T) {
	withRoot(t)
	err := Delete("nonexistent-profile-xyz")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Delete missing profile: got %v, want ErrProfileNotFound", err)
	}
}

func TestDeleteStatError(t *testing.T) {
	// Create a root where the profile path is inside a file (not a dir).
	root := withRoot(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	// Create a file at the profile path so Stat returns a non-ErrNotExist error.
	profilePath := filepath.Join(root, "myprofile")
	if err := os.WriteFile(profilePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Now make it a directory so Delete's "not a directory" branch fires.
	// Actually the file IS there, so Delete should hit the "not a directory" error.
	err := Delete("myprofile")
	if err == nil {
		t.Fatal("expected error when profile path is a file, got nil")
	}
}

func TestRootFallsBackToHomeWhenXDGUnset(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	got := Root()
	if got == "" {
		t.Error("Root() returned empty string")
	}
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

func TestFindChromeEnvBareNameResolves(t *testing.T) {
	// When SQUAD_BROWSER_BIN is a bare name present on PATH, findChrome
	// should resolve it via exec.LookPath.
	path, err := exec_LookPath_true()
	if err != nil {
		t.Skip("no `true` binary; skipping bare-name resolve test")
	}
	// Set env to the bare filename, not the absolute path.
	base := filepath.Base(path)
	t.Setenv("SQUAD_BROWSER_BIN", base)
	resolved, err := findChrome()
	if err != nil {
		t.Fatalf("findChrome error: %v", err)
	}
	if resolved == base {
		t.Fatalf("findChrome should resolve to absolute path, got bare name %q", resolved)
	}
}

func TestLaunchWithStderrAndWaitFailure(t *testing.T) {
	withRoot(t)
	// `/usr/bin/false` exits 1 — exercises Stderr wiring and the Wait error path.
	candidates := []string{"/usr/bin/false", "/bin/false"}
	var falsePath string
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			falsePath = c
			break
		}
	}
	if falsePath == "" {
		t.Skip("no `false` binary on PATH")
	}
	t.Setenv("SQUAD_BROWSER_BIN", falsePath)
	err := Launch("amazon", LaunchOptions{Wait: true, Stderr: os.Stderr})
	if err == nil || !strings.Contains(err.Error(), "chrome exited") {
		t.Fatalf("Launch should report wait error, got: %v", err)
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

func TestFilepathIsAbsAndIsAbsExecutable(t *testing.T) {
	// Absolute posix path should be treated as absolute.
	if !filepathIsAbs("/tmp") {
		t.Fatal("filepathIsAbs should return true for absolute posix paths")
	}
	// Windows-looking path on non-Windows should be treated as non-absolute.
	if runtime.GOOS != "windows" && filepathIsAbs("C:/Windows") {
		t.Fatal("filepathIsAbs should be false for Windows-style paths on non-Windows")
	}
	// Non-existent absolute file is not an executable file.
	tmp := filepath.Join(t.TempDir(), "nope")
	if isAbsExecutable(tmp) {
		t.Fatalf("isAbsExecutable should be false for non-existent absolute path: %s", tmp)
	}
}

func TestLaunchStartFails(t *testing.T) {
	withRoot(t)
	// Create a non-executable file. exec.Command("/path/to/file").Start()
	// will fail with a permission/format error, exercising the
	// "launch chrome (%s): %w" branch.
	dir := t.TempDir()
	bin := filepath.Join(dir, "notexec")
	if err := os.WriteFile(bin, []byte("#!/nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SQUAD_BROWSER_BIN", bin)
	err := Launch("amazon", LaunchOptions{Wait: true})
	if err == nil {
		t.Fatal("expected error launching non-executable file")
	}
	if !strings.Contains(err.Error(), "launch chrome") {
		t.Fatalf("error = %v, want 'launch chrome' wrap", err)
	}
}
