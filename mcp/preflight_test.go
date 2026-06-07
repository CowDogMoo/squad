package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

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

// withEndpoint temporarily redirects cdpEndpoint for the duration of a test.
// Not t.Parallel safe — the helper mutates a package-level var.
func withEndpoint(t *testing.T, url string) {
	t.Helper()
	prev := cdpEndpoint
	cdpEndpoint = url
	t.Cleanup(func() { cdpEndpoint = prev })
}

func TestCDPReachable(t *testing.T) {
	t.Run("200 response means reachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		withEndpoint(t, srv.URL)
		if !cdpReachable(context.Background()) {
			t.Fatal("expected reachable=true for 200 response")
		}
	})

	t.Run("non-2xx response means unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)
		withEndpoint(t, srv.URL)
		if cdpReachable(context.Background()) {
			t.Fatal("expected reachable=false for 500 response")
		}
	})

	t.Run("connection refused means unreachable", func(t *testing.T) {
		// Bind, capture address, then close — guarantees nothing is listening.
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close()
		withEndpoint(t, url)
		if cdpReachable(context.Background()) {
			t.Fatal("expected reachable=false when nothing is listening")
		}
	})

	t.Run("invalid URL means unreachable", func(t *testing.T) {
		withEndpoint(t, "://not a url")
		if cdpReachable(context.Background()) {
			t.Fatal("expected reachable=false for malformed URL")
		}
	})
}

func TestPreflightServer(t *testing.T) {
	reachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(reachable.Close)

	unreachableSrv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := unreachableSrv.URL
	unreachableSrv.Close()

	savedProfile := t.TempDir()
	if err := os.WriteFile(filepath.Join(savedProfile, "Local State"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed saved profile: %v", err)
	}
	freshProfile := t.TempDir()

	tests := []struct {
		name     string
		endpoint string
		cfg      ServerConfig
	}{
		{
			name:     "not chrome MCP is a no-op",
			endpoint: unreachableURL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"some-other-mcp"}},
		},
		{
			name:     "chrome MCP with reachable CDP logs info",
			endpoint: reachable.URL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp", "--autoConnect"}},
		},
		{
			name:     "chrome MCP with autoConnect and unreachable CDP warns about permission API",
			endpoint: unreachableURL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp", "--autoConnect"}},
		},
		{
			name:     "chrome MCP without autoConnect and unreachable CDP warns about dedicated profile",
			endpoint: unreachableURL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp"}},
		},
		{
			name:     "chrome MCP with saved profile logs info",
			endpoint: unreachableURL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp", "--userDataDir=" + savedProfile}},
		},
		{
			name:     "chrome MCP with fresh profile warns about login",
			endpoint: unreachableURL,
			cfg:      ServerConfig{Command: "npx", Args: []string{"chrome-devtools-mcp", "--userDataDir=" + freshProfile}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withEndpoint(t, tt.endpoint)
			// PreflightServer never returns; assertion is that it doesn't panic
			// and that each branch executes (verified via coverage).
			PreflightServer(context.Background(), tt.cfg)
		})
	}
}
func TestChromeUserDataDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "equals form", args: []string{"chrome-devtools-mcp@latest", "--userDataDir=/tmp/profile"}, want: "/tmp/profile"},
		{name: "space form", args: []string{"chrome-devtools-mcp@latest", "--userDataDir", "/tmp/profile"}, want: "/tmp/profile"},
		{name: "absent", args: []string{"chrome-devtools-mcp@latest", "--autoConnect"}, want: ""},
		{name: "space form with nothing after", args: []string{"chrome-devtools-mcp@latest", "--userDataDir"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := ServerConfig{Command: "npx", Args: tt.args}
			if got := chromeUserDataDir(cfg); got != tt.want {
				t.Errorf("chromeUserDataDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLooksLikeFreshProfile(t *testing.T) {
	t.Parallel()

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()
		if looksLikeFreshProfile("") {
			t.Fatal("empty path should not be considered a fresh profile")
		}
	})

	t.Run("missing dir", func(t *testing.T) {
		t.Parallel()
		if looksLikeFreshProfile(filepath.Join(t.TempDir(), "does-not-exist")) {
			t.Fatal("missing dir should not be considered a fresh profile")
		}
	})

	t.Run("dir with no chrome state", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if !looksLikeFreshProfile(dir) {
			t.Fatal("empty dir should look like a fresh profile")
		}
	})

	t.Run("dir with Local State marker", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if looksLikeFreshProfile(dir) {
			t.Fatal("dir with Local State should NOT look fresh")
		}
	})

	t.Run("dir with Default/Cookies marker", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o700); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Default", "Cookies"), []byte(""), 0o600); err != nil {
			t.Fatalf("seed cookies: %v", err)
		}
		if looksLikeFreshProfile(dir) {
			t.Fatal("dir with Default/Cookies should NOT look fresh")
		}
	})
}
