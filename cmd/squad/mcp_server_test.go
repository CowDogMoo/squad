package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/cowdogmoo/squad/browser"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// chromeExecOpts returns headless allocator options pinned to the same
// binary discovery browser.Launch uses, so tests don't launch whatever
// stray Chromium chromedp's own preference order finds first.
func chromeExecOpts(extra ...chromedp.ExecAllocatorOption) []chromedp.ExecAllocatorOption {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true), chromedp.NoSandbox)
	if bin, err := browser.FindChrome(); err == nil {
		opts = append(opts, chromedp.ExecPath(bin))
	}
	return append(opts, extra...)
}

// scrubProfileDir registers a tolerant cleanup for a Chrome profile dir that
// lives under t.TempDir: Chrome's helper processes can still be flushing
// profile files for a moment after the allocator reports the browser gone,
// which makes t.TempDir's strict RemoveAll flake with "directory not empty".
// Registered after withBrowserRoot, it runs before the TempDir cleanup and
// retries until Chrome's stragglers have quiesced.
func scrubProfileDir(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Logf("profile dir cleanup gave up: %v", err)
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
}

var (
	chromeCheckOnce sync.Once
	chromeCheckErr  error
)

// requireChrome skips the test when no Chrome/Chromium can be launched:
// tests that exercise a real browser must degrade to a skip, not a failure,
// on machines without one. The probe result is cached — launching Chrome is
// too slow to repeat per test.
func requireChrome(t *testing.T) {
	t.Helper()
	chromeCheckOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, chromeExecOpts()...)
		defer cancelAlloc()
		browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
		defer cancelBrowser()
		chromeCheckErr = chromedp.Run(browserCtx, chromedp.Navigate("about:blank"))
	})
	if chromeCheckErr != nil {
		t.Skipf("chrome unavailable: %v", chromeCheckErr)
	}
}

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
	for _, flag := range []string{"profile", "user-data-dir", "headless", "no-sandbox"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected %s flag", flag)
		}
	}
}

func TestMCPServerRunBrowserServerIOCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runBrowserServerIO(ctx, browserServerOptions{Headless: true, NoSandbox: true}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestMCPServerConnectBrowserServerProfileConflicts(t *testing.T) {
	_, _, err := connectBrowserServer(context.Background(), browserServerOptions{
		Profile:     "myprofile",
		UserDataDir: "/tmp/somewhere",
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("err = %v, want profile/user-data-dir conflict", err)
	}
}

func TestMCPServerConnectBrowserServerProfileNoSession(t *testing.T) {
	withBrowserRoot(t)
	_, _, err := connectBrowserServer(context.Background(), browserServerOptions{Profile: "ghost"})
	if err == nil || !strings.Contains(err.Error(), "no active browser session") {
		t.Fatalf("err = %v, want no-active-session error", err)
	}
}

func TestMCPServerRunBrowserServerIO_Success(t *testing.T) {
	requireChrome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stdout bytes.Buffer
	stdin := strings.NewReader("")

	// Providing empty reader will cause stdioServer.Listen to finish on EOF.
	err := runBrowserServerIO(ctx, browserServerOptions{Headless: true, NoSandbox: true}, stdin, &stdout)
	if err != nil {
		t.Fatalf("runBrowserServerIO error: %v", err)
	}
}

func TestMCPServerBrowserCmd_Execute(t *testing.T) {
	requireChrome(t)
	cmd := newMCPServerBrowserCmd()
	cmd.SetArgs([]string{"--headless=true", "--no-sandbox"})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd.SetContext(ctx)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute error: %v", err)
	}
}

func TestMCPServerRegisterBrowserTools_ErrorCases(t *testing.T) {
	s := server.NewMCPServer("test-browser", "1.0.0")
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromeExecOpts()...)
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
	requireChrome(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h1>Hello MCP Browser</h1><button id="test-btn" onclick="document.body.innerText='button clicked'">Click Me</button></body></html>`)
	}))
	defer ts.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), chromeExecOpts()...)
	defer cancelAlloc()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Mirror connectBrowserServer's init: the browser and its tab must be
	// owned by the long-lived context before any tool call derives a
	// short-lived one from it.
	if err := chromedp.Run(ctx, chromedp.Navigate("about:blank")); err != nil {
		t.Skipf("skipping live browser test: %v", err)
	}

	s := server.NewMCPServer("test-live-browser", "1.0.0")
	registerBrowserTools(s, ctx)

	callTool(t, s, ctx, "navigate", map[string]any{"url": ts.URL})
	callTool(t, s, ctx, "read_page", nil)
	callTool(t, s, ctx, "evaluate_js", map[string]any{"script": "10 * 5"})
	callTool(t, s, ctx, "click", map[string]any{"selector": "#test-btn"})
	callTool(t, s, ctx, "read_page", nil)
}

// TestMCPServerAttachMode pins the --profile path: the server attaches to an
// already-running session's page, drives it in place, and its teardown must
// leave that page open — closing the user's tab would defeat the entire
// attach-to-real-Chrome model.
func TestMCPServerAttachMode(t *testing.T) {
	requireChrome(t)
	root := withBrowserRoot(t)
	profileDir := filepath.Join(root, "attachmode")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scrubProfileDir(t, profileDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, chromeExecOpts(
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("remote-debugging-port", "0"),
	)...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,<body>HELLO-ATTACH</body>")); err != nil {
		t.Skipf("skipping live browser test: %v", err)
	}

	tabCtx, cleanup, err := connectBrowserServer(ctx, browserServerOptions{Profile: "attachmode"})
	if err != nil {
		t.Fatalf("connectBrowserServer: %v", err)
	}

	s := server.NewMCPServer("test-attach-browser", "1.0.0")
	registerBrowserTools(s, tabCtx)

	if text := callTool(t, s, ctx, "read_page", nil); !strings.Contains(text, "HELLO-ATTACH") {
		t.Fatalf("read_page = %q, want the running session's page content", text)
	}
	if out := callTool(t, s, ctx, "evaluate_js", map[string]any{"script": "document.body.innerText"}); !strings.Contains(out, "HELLO-ATTACH") {
		t.Fatalf("evaluate_js = %q, want the running session's page content", out)
	}

	// While attached, the page must carry the visible agent-control
	// indicator — and keep it across navigations.
	const indicatorCheck = "document.getElementById('__squad_indicator__') !== null"
	if out := callTool(t, s, ctx, "evaluate_js", map[string]any{"script": indicatorCheck}); out != "true" {
		t.Fatalf("indicator present = %s, want true after attach", out)
	}
	callTool(t, s, ctx, "navigate", map[string]any{"url": "data:text/html,<body>HELLO-NAV</body>"})
	if out := callTool(t, s, ctx, "evaluate_js", map[string]any{"script": indicatorCheck}); out != "true" {
		t.Fatalf("indicator present = %s, want true after navigation", out)
	}

	cleanup()

	// Detach must remove the indicator but leave the page itself alone.
	var indicatorAfter bool
	if err := chromedp.Run(browserCtx, chromedp.Evaluate(indicatorCheck, &indicatorAfter)); err != nil {
		t.Fatalf("evaluate after cleanup: %v", err)
	}
	if indicatorAfter {
		t.Fatal("indicator still present after detach")
	}

	// The user's page must survive the server detaching.
	targets, err := chromedp.Targets(browserCtx)
	if err != nil {
		t.Fatalf("Targets after cleanup: %v", err)
	}
	for _, tgt := range targets {
		if tgt.Type == "page" && strings.Contains(tgt.URL, "HELLO-NAV") {
			return
		}
	}
	t.Fatalf("session page was closed by server teardown; remaining targets: %d", len(targets))
}
