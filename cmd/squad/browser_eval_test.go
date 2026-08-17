package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/cowdogmoo/squad/browser"
)

func TestNewBrowserEvalCmd(t *testing.T) {
	cmd := newBrowserEvalCmd()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Use != "eval NAME SCRIPT" {
		t.Errorf("expected Use 'eval NAME SCRIPT', got %q", cmd.Use)
	}
}

func TestBrowserEvalInvalidName(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserEvalCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"Bad Name", "1+1"})
	if err == nil || !errors.Is(err, browser.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestBrowserEvalNoActiveSession(t *testing.T) {
	withBrowserRoot(t)
	cmd := newBrowserEvalCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.RunE(cmd, []string{"amazon", "1+1"})
	if err == nil {
		t.Fatal("expected error for inactive session, got nil")
	}
}

func TestBrowserEvalConnectionError(t *testing.T) {
	root := withBrowserRoot(t)
	profileDir := filepath.Join(root, "amazon")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write invalid port
	portFile := filepath.Join(profileDir, "DevToolsActivePort")
	if err := os.WriteFile(portFile, []byte("9999\n/devtools/browser/xyz\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newBrowserEvalCmd()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd.SetContext(ctx)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.RunE(cmd, []string{"amazon", "1+1"})
	if err == nil {
		t.Fatal("expected error connecting to inactive port")
	}
}

func TestBrowserEvalLiveSuccess(t *testing.T) {
	root := withBrowserRoot(t)
	profileDir := filepath.Join(root, "liveeval")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scrubProfileDir(t, profileDir)

	opts := chromeExecOpts(
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("remote-debugging-port", "0"),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	if err := chromedp.Run(browserCtx, chromedp.Navigate("data:text/html,<body>HELLO-EVAL-PROBE</body>")); err != nil {
		t.Skipf("skipping live browser test: %v", err)
	}

	// DevToolsActivePort should now be present in profileDir
	cmd := newBrowserEvalCmd()
	cmd.SetContext(ctx)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.RunE(cmd, []string{"liveeval", "2 + 2"}); err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "4") {
		t.Fatalf("expected output '4', got: %s", stdout.String())
	}

	// The eval must run in the session's open page, not a fresh blank tab:
	// reading the page body is the command's documented use case.
	stdout.Reset()
	if err := cmd.RunE(cmd, []string{"liveeval", "document.body.innerText"}); err != nil {
		t.Fatalf("eval failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "HELLO-EVAL-PROBE") {
		t.Fatalf("expected the open page's body text, got: %s", stdout.String())
	}
}
