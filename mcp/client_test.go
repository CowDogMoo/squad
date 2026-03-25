package mcp

import (
	"context"
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
			name:    "missing command",
			cfg:     ServerConfig{Name: "test"},
			wantErr: `mcp server "test" missing command`,
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
