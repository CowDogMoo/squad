package executor

import (
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *Config
		workingDir string
		wantType   string
		wantErr    bool
	}{
		{
			name:       "nil config returns local executor",
			cfg:        nil,
			workingDir: "/tmp",
			wantType:   "local",
		},
		{
			name:       "empty type returns local executor",
			cfg:        &Config{Type: ""},
			workingDir: "/tmp",
			wantType:   "local",
		},
		{
			name:       "explicit local type",
			cfg:        &Config{Type: "local"},
			workingDir: "/tmp",
			wantType:   "local",
		},
		{
			name:    "unknown type returns error",
			cfg:     &Config{Type: "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec, err := New(tt.cfg, tt.workingDir)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exec == nil {
				t.Fatal("New() returned nil executor")
			}
			if exec.Type() != tt.wantType {
				t.Errorf("Type() = %q, want %q", exec.Type(), tt.wantType)
			}
		})
	}
}
