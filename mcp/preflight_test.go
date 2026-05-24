package mcp

import "testing"

func TestIsChromeDevToolsMCP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  ServerConfig
		want bool
	}{
		{
			name: "npx with versioned package",
			cfg:  ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp@latest", "--autoConnect"}},
			want: true,
		},
		{
			name: "npx with -y and package",
			cfg:  ServerConfig{Command: "npx", Args: []string{"-y", "chrome-devtools-mcp", "--autoConnect"}},
			want: true,
		},
		{
			name: "direct command",
			cfg:  ServerConfig{Command: "/usr/local/bin/chrome-devtools-mcp", Args: []string{"--autoConnect"}},
			want: true,
		},
		{
			name: "unrelated stdio server",
			cfg:  ServerConfig{Command: "npx", Args: []string{"some-other-mcp@latest"}},
			want: false,
		},
		{
			name: "empty config",
			cfg:  ServerConfig{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isChromeDevToolsMCP(tt.cfg); got != tt.want {
				t.Errorf("isChromeDevToolsMCP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChromeUsesAutoConnect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare flag", args: []string{"chrome-devtools-mcp@latest", "--autoConnect"}, want: true},
		{name: "flag with value", args: []string{"chrome-devtools-mcp@latest", "--autoConnect=true"}, want: true},
		{name: "no autoConnect", args: []string{"chrome-devtools-mcp@latest"}, want: false},
		{name: "substring only does not match", args: []string{"--no-autoConnect"}, want: false},
		{name: "empty args", args: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := ServerConfig{Command: "npx", Args: tt.args}
			if got := chromeUsesAutoConnect(cfg); got != tt.want {
				t.Errorf("chromeUsesAutoConnect() = %v, want %v", got, tt.want)
			}
		})
	}
}
