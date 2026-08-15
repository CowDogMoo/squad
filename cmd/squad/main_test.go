package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain redirects the XDG directories to a throwaway root before any
// test runs. The agents/skill add, remove, and pin commands persist the
// full config through source.Manager.saveConfig, so a test that forgets
// its own t.Setenv isolation must corrupt this temp root — never the
// developer's real ~/.config/squad/config.yaml. That exact leak has
// happened: an early agents-remove test wrote its temporary cache_dir
// into the real user config. Individual tests that set their own XDG
// values via t.Setenv still override these process-level defaults.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "squad-cmd-test-xdg-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create test XDG root: %v\n", err)
		os.Exit(1)
	}
	for dir, sub := range map[string]string{
		"XDG_CONFIG_HOME": "config",
		"XDG_CACHE_HOME":  "cache",
		"XDG_STATE_HOME":  "state",
	} {
		if err := os.Setenv(dir, filepath.Join(root, sub)); err != nil {
			fmt.Fprintf(os.Stderr, "failed to set %s: %v\n", dir, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if err := os.RemoveAll(root); err != nil {
		fmt.Fprintf(os.Stderr, "failed to clean test XDG root: %v\n", err)
	}
	os.Exit(code)
}
