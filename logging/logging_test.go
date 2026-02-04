package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestCustomLoggerFormatAndLog(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &CustomLogger{
		LogLevel:      slog.LevelInfo,
		OutputType:    PlainOutput,
		Quiet:         false,
		ConsoleWriter: buf,
	}

	logger.Info("hello %s", "world")
	output := buf.String()
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected formatted output, got %q", output)
	}

	buf.Reset()
	logger.SetQuiet(true)
	logger.Info("hidden")
	if buf.Len() != 0 {
		t.Fatalf("expected quiet to suppress output, got %q", buf.String())
	}

	logger.SetQuiet(true)
	logger.Error("boom")
	if !strings.Contains(buf.String(), "boom") {
		t.Fatalf("expected error output even when quiet, got %q", buf.String())
	}
}

func TestDetermineLogLevel(t *testing.T) {
	if DetermineLogLevel("debug") != slog.LevelDebug {
		t.Fatalf("expected debug level")
	}
	if DetermineLogLevel("unknown") != slog.LevelInfo {
		t.Fatalf("expected fallback to info")
	}
}

func TestContextLoggerOverride(t *testing.T) {
	buf := &bytes.Buffer{}
	custom := &CustomLogger{
		LogLevel:      slog.LevelInfo,
		OutputType:    PlainOutput,
		ConsoleWriter: buf,
	}

	ctx := WithLogger(context.Background(), custom)
	InfoContext(ctx, "hello")

	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected context logger output, got %q", buf.String())
	}
}

func TestInitializeAndGlobalHelpers(t *testing.T) {
	if err := Initialize("warn", "color", true, true); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	global := FromContext(context.TODO())
	if global.OutputType != ColorOutput {
		t.Fatalf("expected color output")
	}
	if global.LogLevel != slog.LevelDebug {
		t.Fatalf("expected verbose to force debug, got %v", global.LogLevel)
	}

	SetQuiet(false)
	if IsQuiet() {
		t.Fatalf("expected quiet false")
	}

	SetVerbose(true)
	if !global.shouldShowOnConsole(DebugLevel) {
		t.Fatalf("expected debug visible when verbose")
	}
}

func TestFormatMessageColorFallback(t *testing.T) {
	logger := &CustomLogger{OutputType: ColorOutput}
	formatted := logger.formatMessage(InfoLevel, "colored")
	if !strings.Contains(formatted, "colored") {
		t.Fatalf("expected formatted message to contain text")
	}
}

func TestOutputJSON(t *testing.T) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = orig
	})

	logger := &CustomLogger{OutputType: JSONOutput}
	logger.Output(map[string]string{"status": "ok"})
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()
	if !strings.Contains(output, "\"status\"") {
		t.Fatalf("expected JSON output, got %q", output)
	}
}
