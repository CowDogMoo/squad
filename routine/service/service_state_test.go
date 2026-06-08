package service_test

import (
	"testing"

	"github.com/cowdogmoo/squad/routine/service"
)

func TestStateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state service.State
		want  string
	}{
		{"not installed", service.StateNotInstalled, "not installed"},
		{"installed stopped", service.StateInstalledStopped, "installed (stopped)"},
		{"installed running", service.StateInstalledRunning, "installed (running)"},
		{"unknown", service.State(99), "unknown"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestErrUnsupported(t *testing.T) {
	t.Parallel()
	if service.ErrUnsupported == nil {
		t.Fatal("ErrUnsupported should not be nil")
	}
	if service.ErrUnsupported.Error() == "" {
		t.Error("ErrUnsupported.Error() should not be empty")
	}
}
