package service_test

import (
	"testing"

	"github.com/cowdogmoo/squad/routine/service"
)

func TestState_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   service.State
		want string
	}{
		{"running", service.StateInstalledRunning, "installed (running)"},
		{"stopped", service.StateInstalledStopped, "installed (stopped)"},
		{"not-installed", service.StateNotInstalled, "not installed"},
		{"unknown", service.State(42), "unknown"},
	}
	for _, tt := range tests {
		// Go 1.22+: no need to capture range var
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.String()
			if got != tt.want {
				t.Fatalf("State.String() = %q, want %q", got, tt.want)
			}
		})
	}
}
