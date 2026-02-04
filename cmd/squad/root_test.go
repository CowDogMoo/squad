package main

import (
	"os"
	"testing"
)

func TestExecuteVersion(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", baseDir)
	t.Setenv("HOME", baseDir)
	t.Setenv("USERPROFILE", baseDir)

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"squad", "version"}

	if err := Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}
