package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

func TestConnectValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     ServerConfig
		wantErr string
	}{
		{
			name:    "missing name",
			cfg:     ServerConfig{Command: "echo"},
			wantErr: "mcp server config missing name",
		},
		{
			name:    "missing command for stdio",
			cfg:     ServerConfig{Name: "test"},
			wantErr: `mcp server "test" missing command for stdio transport`,
		},
		{
			name:    "missing url for sse",
			cfg:     ServerConfig{Name: "test", Transport: "sse"},
			wantErr: `mcp server "test" missing url for sse transport`,
		},
		{
			name:    "unsupported transport",
			cfg:     ServerConfig{Name: "test", Transport: "grpc"},
			wantErr: `mcp server "test" has unsupported transport "grpc" (want stdio or sse)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Connect(context.Background(), tt.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); got != tt.wantErr {
				t.Fatalf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestConnectInvalidCommand(t *testing.T) {
	t.Parallel()
	_, err := Connect(context.Background(), ServerConfig{
		Name:    "bad",
		Command: "/nonexistent/binary/that/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
}

func TestClientCallToolNilInner(t *testing.T) {
	t.Parallel()
	c := &Client{name: "disconnected"}
	_, err := c.CallTool(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error for nil inner client")
	}
	if got := err.Error(); got != `MCP client "disconnected" is not connected` {
		t.Fatalf("error = %q, want not connected error", got)
	}
}

func TestClientClose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    *Client
	}{
		{
			name: "nil inner",
			c:    &Client{name: "test"},
		},
		{
			name: "with mock inner",
			c:    NewTestClient("mock-server", nil, &mockMCPClient{}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.c.Close(); err != nil {
				t.Fatalf("Close() error = %v, want nil", err)
			}
		})
	}
}

func TestConnectSSEHandshakeFailure(t *testing.T) {
	t.Parallel()
	// Server returns non-SSE response so the MCP client fails during handshake.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Connect(context.Background(), ServerConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       srv.URL,
		Headers:   []string{"Authorization=Bearer token", "X-Custom=value"},
	})
	if err == nil {
		t.Fatal("expected error from SSE handshake failure")
	}
	if !strings.Contains(err.Error(), "test-sse") {
		t.Fatalf("error = %q, want to contain server name", err.Error())
	}
}

func TestClientCallToolWithMock(t *testing.T) {
	t.Parallel()
	c := NewTestClient("mock-server", nil, &mockMCPClient{
		callResult: &mcptypes.CallToolResult{
			Content: []mcptypes.Content{
				mcptypes.TextContent{Type: "text", Text: "mock result"},
			},
		},
	})
	result, err := c.CallTool(context.Background(), "test_tool", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Content))
	}
}

func TestClientCallToolError(t *testing.T) {
	t.Parallel()
	c := NewTestClient("mock-server", nil, &mockMCPClient{
		callErr: fmt.Errorf("tool execution failed"),
	})
	_, err := c.CallTool(context.Background(), "broken_tool", nil)
	if err == nil {
		t.Fatal("expected error from CallTool")
	}
	if !strings.Contains(err.Error(), "tool execution failed") {
		t.Fatalf("error = %q, want to contain 'tool execution failed'", err.Error())
	}
}

func TestCreateTransportValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     ServerConfig
		wantErr string
	}{
		{
			name:    "stdio missing command",
			cfg:     ServerConfig{Name: "test"},
			wantErr: `mcp server "test" missing command for stdio transport`,
		},
		{
			name:    "sse missing url",
			cfg:     ServerConfig{Name: "test", Transport: "sse"},
			wantErr: `mcp server "test" missing url for sse transport`,
		},
		{
			name:    "unsupported transport",
			cfg:     ServerConfig{Name: "test", Transport: "grpc"},
			wantErr: `mcp server "test" has unsupported transport "grpc"`,
		},
		{
			name:    "invalid stdio command",
			cfg:     ServerConfig{Name: "test", Command: "/nonexistent/binary"},
			wantErr: "failed to start MCP server",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := createTransport(context.Background(), tt.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCloseOnErrorLogsWarning(t *testing.T) {
	t.Parallel()
	// closeOnError should not panic with a working mock client.
	mock := &mockMCPClient{}
	closeOnError(mock, "test-server", "test")
}

func TestCloseOnErrorWithCloseFailure(t *testing.T) {
	t.Parallel()
	// closeOnError with a client that returns error on Close should not panic.
	mock := &errorCloseMCPClient{closeErr: fmt.Errorf("close failed")}
	closeOnError(mock, "test-server", "init")
}

func TestCreateSSETransportStartFailure(t *testing.T) {
	t.Parallel()
	// Server that returns non-SSE response to trigger Start() failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := createSSETransport(context.Background(), ServerConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       srv.URL,
	})
	if err == nil {
		t.Fatal("expected error from SSE start failure")
	}
	if !strings.Contains(err.Error(), "test-sse") {
		t.Fatalf("error = %q, want to contain server name", err.Error())
	}
}

func TestCreateSSETransportInvalidURL(t *testing.T) {
	t.Parallel()
	_, err := createSSETransport(context.Background(), ServerConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       "://invalid-url",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "test-sse") {
		t.Fatalf("error = %q, want to contain server name", err.Error())
	}
}

func TestCreateSSETransportWithHeaders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := createSSETransport(context.Background(), ServerConfig{
		Name:      "test-sse",
		Transport: "sse",
		URL:       srv.URL,
		Headers:   []string{"Authorization=Bearer token", "X-Custom=value"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandshakeSuccess(t *testing.T) {
	t.Parallel()
	mock := &mockMCPClient{}
	c, err := handshake(context.Background(), "test-server", mock)
	if err != nil {
		t.Fatalf("handshake() error = %v", err)
	}
	if c.Name() != "test-server" {
		t.Fatalf("name = %q, want test-server", c.Name())
	}
	if !c.connected {
		t.Fatal("expected connected=true")
	}
}

func TestHandshakeInitFailure(t *testing.T) {
	t.Parallel()
	mock := &failInitMCPClient{initErr: fmt.Errorf("init refused")}
	_, err := handshake(context.Background(), "test-server", mock)
	if err == nil {
		t.Fatal("expected error from init failure")
	}
	if !strings.Contains(err.Error(), "initialization failed") {
		t.Fatalf("error = %q, want initialization failed", err.Error())
	}
}

func TestHandshakeInitFailureWithCloseError(t *testing.T) {
	t.Parallel()
	// Compose: fails init AND fails close — both error paths exercised.
	mock := &failInitAndCloseClient{
		initErr:  fmt.Errorf("init refused"),
		closeErr: fmt.Errorf("close broken"),
	}
	_, err := handshake(context.Background(), "test-server", mock)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "initialization failed") {
		t.Fatalf("error = %q, want initialization failed", err.Error())
	}
}

func TestHandshakeListToolsFailure(t *testing.T) {
	t.Parallel()
	mock := &failListToolsMCPClient{listErr: fmt.Errorf("list denied")}
	_, err := handshake(context.Background(), "test-server", mock)
	if err == nil {
		t.Fatal("expected error from list tools failure")
	}
	if !strings.Contains(err.Error(), "tools/list failed") {
		t.Fatalf("error = %q, want tools/list failed", err.Error())
	}
}

func TestHandshakeListToolsFailureWithCloseError(t *testing.T) {
	t.Parallel()
	mock := &failListToolsAndCloseClient{
		listErr:  fmt.Errorf("list denied"),
		closeErr: fmt.Errorf("close broken"),
	}
	_, err := handshake(context.Background(), "test-server", mock)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tools/list failed") {
		t.Fatalf("error = %q, want tools/list failed", err.Error())
	}
}

func TestCloseTimeout(t *testing.T) {
	t.Parallel()
	c := NewTestClient("hanging-server", nil, &slowCloseMCPClient{
		delay: closeTimeout + 2*time.Second,
	})
	start := time.Now()
	err := c.Close()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error from Close()")
	}
	if !strings.Contains(err.Error(), "did not shut down") {
		t.Fatalf("error = %q, want timeout message", err.Error())
	}
	// Should return after closeTimeout, not after the full delay.
	if elapsed > closeTimeout+time.Second {
		t.Fatalf("Close() took %v, expected ~%v", elapsed, closeTimeout)
	}
}
