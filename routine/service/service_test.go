// Cross-platform tests for the platform-neutral parts of the service
// package — currently the State enum's String() method. Lives outside the
// build-tagged files so coverage counts on every CI runner regardless of
// platform.

package service

import "testing"

func TestStateString(t *testing.T) {
	t.Parallel()
	cases := map[State]string{
		StateNotInstalled:     "not installed",
		StateInstalledStopped: "installed (stopped)",
		StateInstalledRunning: "installed (running)",
		State(99):             "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestErrUnsupportedIsErrorValue(t *testing.T) {
	t.Parallel()
	if ErrUnsupported.Error() == "" {
		t.Error("ErrUnsupported has empty message")
	}
}
