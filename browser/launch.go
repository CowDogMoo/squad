package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// ErrChromeNotFound is returned by Launch when no Chrome/Chromium binary
// can be located on the system.
var ErrChromeNotFound = errors.New("could not locate Google Chrome or Chromium; install one or set SQUAD_BROWSER_BIN")

// chromeCandidates returns the binary paths Launch will try, in order.
// Override entirely via SQUAD_BROWSER_BIN if neither default exists.
func chromeCandidates() []string {
	if v := os.Getenv("SQUAD_BROWSER_BIN"); v != "" {
		return []string{v}
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "linux":
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}
	default:
		return nil
	}
}

// findChrome resolves the first launchable Chrome binary in chromeCandidates.
// Absolute paths are checked with os.Stat; bare names go through exec.LookPath.
func findChrome() (string, error) {
	for _, c := range chromeCandidates() {
		if c == "" {
			continue
		}
		if isAbsExecutable(c) {
			return c, nil
		}
		if path, err := exec.LookPath(c); err == nil {
			return path, nil
		}
	}
	return "", ErrChromeNotFound
}

func isAbsExecutable(path string) bool {
	if !filepathIsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return true
}

// filepathIsAbs is split out so the test for findChrome's fallback path
// stays readable; equivalent to filepath.IsAbs but avoids the import dance
// for a one-line helper.
func filepathIsAbs(p string) bool {
	return len(p) > 0 && (p[0] == '/' || (runtime.GOOS == "windows" && len(p) > 1 && p[1] == ':'))
}

// LaunchOptions controls Launch. The zero value is usable: it opens the
// profile to a blank page in a foreground window and waits for the user
// to quit Chrome.
type LaunchOptions struct {
	// URL is the initial URL to load. Empty defaults to about:blank.
	URL string
	// Wait, when true, blocks Launch until Chrome exits. When false,
	// Chrome is started and Launch returns immediately — useful for
	// scripted setup where the caller doesn't want to wedge a terminal.
	Wait bool
	// Stderr receives diagnostic output (Chrome's own logs). nil discards.
	Stderr *os.File
}

// Launch opens Chrome against the profile dir for name. The profile is
// created lazily by ProfileDir. Caller is responsible for printing
// human-facing instructions (e.g. "log in then close the window");
// Launch is purely the subprocess wrapper.
func Launch(name string, opts LaunchOptions) error {
	dir, err := ProfileDir(name)
	if err != nil {
		return err
	}
	bin, err := findChrome()
	if err != nil {
		return err
	}
	url := opts.URL
	if url == "" {
		url = "about:blank"
	}
	args := []string{
		"--user-data-dir=" + dir,
		"--new-window",
		url,
	}
	cmd := exec.Command(bin, args...)
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch chrome (%s): %w", bin, err)
	}
	if !opts.Wait {
		// Detach: don't wait, but don't leave a zombie either.
		// On macOS the process becomes a child of launchd when Wait()
		// is not called; that's fine — Chrome's own process management
		// handles teardown when the user closes the window.
		go func() { _ = cmd.Wait() }()
		return nil
	}
	if err := cmd.Wait(); err != nil {
		// Chrome returning non-zero on window-close is normal in some
		// versions; treat any wait error as informational.
		return fmt.Errorf("chrome exited with error: %w", err)
	}
	return nil
}
