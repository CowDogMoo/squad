package mcp

import "testing"

func TestTransportType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cfg      ServerConfig
		wantType string
	}{
		{
			name:     "default is stdio",
			cfg:      ServerConfig{Name: "test", Command: "echo"},
			wantType: "stdio",
		},
		{
			name:     "explicit stdio",
			cfg:      ServerConfig{Name: "test", Transport: "stdio", Command: "echo"},
			wantType: "stdio",
		},
		{
			name:     "sse transport",
			cfg:      ServerConfig{Name: "test", Transport: "sse", URL: "http://localhost:9876"},
			wantType: "sse",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.TransportType(); got != tt.wantType {
				t.Errorf("TransportType() = %q, want %q", got, tt.wantType)
			}
		})
	}
}
