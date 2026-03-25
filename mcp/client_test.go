package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
