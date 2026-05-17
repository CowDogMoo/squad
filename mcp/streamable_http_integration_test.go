package mcp_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	mcppkg "github.com/cowdogmoo/squad/mcp"
	srvmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestStreamableHTTP_EndToEnd starts a real in-process Streamable HTTP MCP
// server, points squad's MCP client at it, and exercises the full lifecycle:
// connect → discover tools → invoke a tool → close. This proves the new
// transport actually speaks the protocol rather than merely compiling.
func TestStreamableHTTP_EndToEnd(t *testing.T) {
	// Bind to a random free port so the test never collides with another
	// process or another parallel test run.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()

	mcpSrv := server.NewMCPServer("squad-it", "0.0.1", server.WithToolCapabilities(true))
	mcpSrv.AddTool(
		srvmcp.NewTool("add",
			srvmcp.WithDescription("Add two numbers"),
			srvmcp.WithNumber("a", srvmcp.Required()),
			srvmcp.WithNumber("b", srvmcp.Required()),
		),
		func(_ context.Context, req srvmcp.CallToolRequest) (*srvmcp.CallToolResult, error) {
			a, _ := req.RequireFloat("a")
			b, _ := req.RequireFloat("b")
			return srvmcp.NewToolResultText(fmt.Sprintf("%g", a+b)), nil
		},
	)

	httpHandler := server.NewStreamableHTTPServer(mcpSrv)
	httpSrv := &http.Server{Handler: httpHandler, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(listener) }()
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := mcppkg.ServerConfig{
		Name:      "demo",
		Transport: "streamable_http",
		URL:       "http://" + addr + "/mcp",
	}

	client, err := mcppkg.Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() {
		if cerr := client.Close(); cerr != nil {
			t.Logf("close: %v", cerr)
		}
	})

	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "add" {
		t.Fatalf("expected tool 'add', got %q", tools[0].Name)
	}

	result, err := client.CallTool(ctx, "add", map[string]any{"a": 7, "b": 35})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("CallTool returned no content")
	}
	text, ok := result.Content[0].(srvmcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "42") {
		t.Fatalf("expected tool result to contain '42', got %q", text.Text)
	}
}

// TestStreamableHTTP_HeadersForwarded proves the headers configured on a
// ServerConfig actually reach the upstream server. Authorization-style
// headers are how hosted MCP endpoints (Google Drive/Calendar) authenticate
// every request, so this is load-bearing for the weekly-planner use case.
func TestStreamableHTTP_HeadersForwarded(t *testing.T) {
	const sentinel = "Bearer test-token-42"

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()

	got := make(chan string, 16)
	mcpSrv := server.NewMCPServer("hdr-test", "0.0.1", server.WithToolCapabilities(true))
	httpHandler := server.NewStreamableHTTPServer(mcpSrv)
	wrap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.Header.Get("Authorization"):
		default:
		}
		httpHandler.ServeHTTP(w, r)
	})
	httpSrv := &http.Server{Handler: wrap, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = httpSrv.Serve(listener) }()
	t.Cleanup(func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mcppkg.Connect(ctx, mcppkg.ServerConfig{
		Name:      "hdr",
		Transport: "streamable_http",
		URL:       "http://" + addr + "/mcp",
		Headers:   []string{"Authorization=" + sentinel},
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case h := <-got:
		if h != sentinel {
			t.Fatalf("Authorization header = %q, want %q", h, sentinel)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never saw the request")
	}
}
