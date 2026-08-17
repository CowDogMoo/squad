package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPServerCmd(t *testing.T) {
	cmd := newMCPServerCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "server" {
		t.Errorf("expected Use 'server', got %q", cmd.Use)
	}
}

func TestMCPServerBrowserCmd(t *testing.T) {
	cmd := newMCPServerBrowserCmd()
	if cmd == nil {
		t.Fatal("expected command, got nil")
	}
	if cmd.Use != "browser" {
		t.Errorf("expected Use 'browser', got %q", cmd.Use)
	}
	if cmd.Flags().Lookup("user-data-dir") == nil {
		t.Error("expected user-data-dir flag")
	}
	if cmd.Flags().Lookup("headless") == nil {
		t.Error("expected headless flag")
	}
}

func TestMCPServerRunBrowserServerInvalidPath(t *testing.T) {
	t.Setenv("SQUAD_NO_SANDBOX", "true")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runBrowserServer(ctx, "/dev/null/invalid_dir_path", true)
	if err == nil {
		t.Fatal("expected error from invalid user data dir, got nil")
	}
}

func TestMCPServerRunBrowserServerIO_Success(t *testing.T) {
	t.Setenv("SQUAD_NO_SANDBOX", "true")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	stdin := strings.NewReader("")

	// Providing empty reader will cause stdioServer.Listen to finish on EOF.
	err := runBrowserServerIO(ctx, "", true, stdin, &stdout)
	if err != nil {
		t.Fatalf("runBrowserServerIO error: %v", err)
	}
}

func TestMCPServerBrowserCmd_Execute(t *testing.T) {
	t.Setenv("SQUAD_NO_SANDBOX", "true")

	cmd := newMCPServerBrowserCmd()
	cmd.SetArgs([]string{"--headless=true"})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute error: %v", err)
	}
}

func TestMCPServerRegisterBrowserTools_ErrorCases(t *testing.T) {
	s := server.NewMCPServer("test-browser", "1.0.0")
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromedp.Flag("headless", true), chromedp.Flag("no-sandbox", true))
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	cancel() // cancel immediately to trigger execution errors

	registerBrowserTools(s, ctx)

	tools := s.ListTools()
	if len(tools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(tools))
	}

	testCases := []struct {
		name string
		args map[string]interface{}
	}{
		{"navigate", map[string]interface{}{"url": "https://example.com"}},
		{"navigate", map[string]interface{}{"url": 123}},
		{"navigate", nil},
		{"read_page", map[string]interface{}{}},
		{"evaluate_js", map[string]interface{}{"script": "1+1"}},
		{"evaluate_js", map[string]interface{}{"script": 123}},
		{"evaluate_js", nil},
		{"click", map[string]interface{}{"selector": "body"}},
		{"click", map[string]interface{}{"selector": 123}},
		{"click", nil},
	}

	for _, tc := range testCases {
		req := mcp.CallToolRequest{}
		if tc.args != nil {
			req.Params.Arguments = tc.args
		}

		tool := s.GetTool(tc.name)
		if tool == nil {
			t.Fatalf("tool %s not found", tc.name)
		}

		res, err := tool.Handler(ctx, req)
		if err != nil {
			t.Logf("handler returned err: %v", err)
		}
		if res == nil || !res.IsError {
			t.Errorf("expected tool %s to return error result, got: %v", tc.name, res)
		}
	}
}

func callTool(t *testing.T, s *server.MCPServer, ctx context.Context, name string, args map[string]any) string {
	t.Helper()
	tool := s.GetTool(name)
	if tool == nil {
		t.Fatalf("tool %s not found", name)
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	res, err := tool.Handler(ctx, req)
	if err != nil || res == nil || res.IsError {
		t.Fatalf("tool %s failed: res=%v, err=%v", name, res, err)
	}
	if len(res.Content) > 0 {
		if text, ok := res.Content[0].(mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func TestMCPServerRegisterBrowserTools_LiveExecution(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h1>Hello MCP Browser</h1><button id="test-btn" onclick="document.body.innerText='button clicked'">Click Me</button></body></html>`)
	}))
	defer ts.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromedp.Flag("headless", true), chromedp.Flag("no-sandbox", true))
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	s := server.NewMCPServer("test-live-browser", "1.0.0")
	registerBrowserTools(s, ctx)

	callTool(t, s, ctx, "navigate", map[string]any{"url": ts.URL})
	callTool(t, s, ctx, "read_page", nil)
	callTool(t, s, ctx, "evaluate_js", map[string]any{"script": "10 * 5"})
	callTool(t, s, ctx, "click", map[string]any{"selector": "#test-btn"})
	callTool(t, s, ctx, "read_page", nil)
}
