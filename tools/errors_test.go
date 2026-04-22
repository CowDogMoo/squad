package tools

import (
	"errors"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrToolExecutionFailed", ErrToolExecutionFailed},
		{"ErrIterationLimitReached", ErrIterationLimitReached},
		{"ErrLoopDetected", ErrLoopDetected},
		{"ErrEditDeadlineReached", ErrEditDeadlineReached},
		{"ErrBlockedCommand", ErrBlockedCommand},
		{"ErrFileNotRead", ErrFileNotRead},
		{"ErrStaleRead", ErrStaleRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("sentinel error %s is nil", tt.name)
			}
			if tt.err.Error() == "" {
				t.Errorf("sentinel error %s has empty message", tt.name)
			}
		})
	}
}

func TestErrorsIsWrapped(t *testing.T) {
	tests := []struct {
		name   string
		target error
		extra  string
	}{
		{"ErrBlockedCommand", ErrBlockedCommand, "extra context"},
		{"ErrFileNotRead", ErrFileNotRead, "path info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := errors.Join(tt.target, errors.New(tt.extra))
			if !errors.Is(wrapped, tt.target) {
				t.Errorf("errors.Is should find %v in wrapped error", tt.target)
			}
		})
	}
}
