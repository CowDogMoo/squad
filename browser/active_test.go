package browser

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivePort_InvalidName(t *testing.T) {
	_, err := ActivePort("invalid/name")
	if err == nil || !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestActivePort_MissingProfile(t *testing.T) {
	_, err := ActivePort("missing-profile")
	if err == nil || !strings.Contains(err.Error(), "no active browser session found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func setupActivePortTest(t *testing.T) string {
	root := withRoot(t)
	profileDir := filepath.Join(root, "myprofile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return filepath.Join(profileDir, "DevToolsActivePort")
}

func TestActivePort_DevToolsIsDir(t *testing.T) {
	portDir := setupActivePortTest(t)
	if err := os.MkdirAll(portDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(portDir)
	_, err := ActivePort("myprofile")
	if err == nil || !strings.Contains(err.Error(), "read DevToolsActivePort") {
		t.Fatalf("expected read error when DevToolsActivePort is a dir, got %v", err)
	}
}

func TestActivePort_InvalidFormatShort(t *testing.T) {
	portDir := setupActivePortTest(t)
	if err := os.WriteFile(portDir, []byte("9222\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer os.RemoveAll(portDir)
	_, err := ActivePort("myprofile")
	if err == nil || !strings.Contains(err.Error(), "invalid DevToolsActivePort format") {
		t.Fatalf("expected invalid format error, got %v", err)
	}
}

func TestActivePort_InvalidPort(t *testing.T) {
	portDir := setupActivePortTest(t)
	if err := os.WriteFile(portDir, []byte("not-a-port\n/devtools/browser/abc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer os.RemoveAll(portDir)
	_, err := ActivePort("myprofile")
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected invalid port error, got %v", err)
	}
}

func TestActivePort_Valid(t *testing.T) {
	portDir := setupActivePortTest(t)
	if err := os.WriteFile(portDir, []byte("9222\n/devtools/browser/abc-123\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	defer os.RemoveAll(portDir)
	got, err := ActivePort("myprofile")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "ws://127.0.0.1:9222/devtools/browser/abc-123"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
