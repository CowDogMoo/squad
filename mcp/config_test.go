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

func TestConnectString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  ServerConfig
		want string
	}{
		{
			name: "stdio without args",
			cfg:  ServerConfig{Name: "test", Command: "/usr/bin/tool"},
			want: "stdio: /usr/bin/tool",
		},
		{
			name: "stdio with args",
			cfg:  ServerConfig{Name: "test", Command: "/usr/bin/tool", Args: []string{"--flag", "value"}},
			want: "stdio: /usr/bin/tool [--flag value]",
		},
		{
			name: "sse transport",
			cfg:  ServerConfig{Name: "burpsuite", Transport: "sse", URL: "http://localhost:9876"},
			want: "sse: http://localhost:9876",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.cfg.ConnectString(); got != tt.want {
				t.Errorf("ConnectString() = %q, want %q", got, tt.want)
			}
		})
	}
}
