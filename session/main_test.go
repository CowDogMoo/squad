package session

import (
	"os"
	"testing"
)

// TestMain points XDG_STATE_HOME at a throwaway directory for the whole
// package. Sessions now live under XDG_STATE_HOME (not in-tree), so without
// this every test would write into the developer's real ~/.local/state.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "squad-session-test")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_STATE_HOME", tmp); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}
