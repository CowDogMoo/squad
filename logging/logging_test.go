package logging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestDetermineLogLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"debug", "debug", slog.LevelDebug},
		{"info", "info", slog.LevelInfo},
		{"warn", "warn", slog.LevelWarn},
		{"error", "error", slog.LevelError},
		{"unknown defaults to info", "unknown", slog.LevelInfo},
		{"empty defaults to info", "", slog.LevelInfo},
		{"mixed case falls through", "Debug", slog.LevelInfo},
		{"whitespace falls through", " info ", slog.LevelInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := DetermineLogLevel(tt.input); got != tt.want {
				t.Fatalf("DetermineLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	tests := []struct {
		name         string
		logLevel     string
		outputFormat string
		quiet        bool
		verbose      bool
		wantOutput   OutputType
	}{
		{"plain format", "info", "plain", false, false, PlainOutput},
		{"text format", "info", "text", false, false, PlainOutput},
		{"json format", "info", "json", false, false, JSONOutput},
		{"color format", "info", "color", false, false, ColorOutput},
		{"verbose overrides log level", "info", "plain", false, true, PlainOutput},
		{"quiet flag", "info", "plain", true, false, PlainOutput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Initialize(tt.logLevel, tt.outputFormat, tt.quiet, tt.verbose); err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}
			loggerMu.RLock()
			got := logger.OutputType
			gotQuiet := logger.Quiet
			gotVerbose := logger.Verbose
			loggerMu.RUnlock()
			if got != tt.wantOutput {
				t.Fatalf("OutputType = %v, want %v", got, tt.wantOutput)
			}
			if gotQuiet != tt.quiet {
				t.Fatalf("Quiet = %v, want %v", gotQuiet, tt.quiet)
			}
			if gotVerbose != tt.verbose {
				t.Fatalf("Verbose = %v, want %v", gotVerbose, tt.verbose)
			}
		})
	}
}

func TestFromContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		wantWarn bool
	}{
		{"nil context returns global", nil, false},
		{"context without logger returns global", context.Background(), false},
		{
			"context with logger returns that logger",
			WithLogger(context.Background(), &CustomLogger{LogLevel: slog.LevelWarn}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromContext(tt.ctx)
			if got == nil {
				t.Fatalf("FromContext() returned nil")
			}
			if tt.wantWarn && got.LogLevel != slog.LevelWarn {
				t.Fatalf("expected warn level logger, got %v", got.LogLevel)
			}
		})
	}
}

func TestGlobalLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	loggerMu.Lock()
	logger = &CustomLogger{
		LogLevel:      slog.LevelDebug,
		OutputType:    PlainOutput,
		ConsoleWriter: buf,
	}
	loggerMu.Unlock()

	Info("hello %s", "world")
	Warn("warn %d", 1)
	Debug("debug msg")
	Error("err %s", "oops")

	output := buf.String()
	for _, want := range []string{"hello world", "warn 1", "debug msg"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got %q", want, output)
		}
	}
}

func TestSetQuietAndVerbose(t *testing.T) {
	if err := Initialize("info", "plain", false, false); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	SetQuiet(true)
	if !IsQuiet() {
		t.Fatalf("expected quiet=true")
	}
	SetQuiet(false)
	if IsQuiet() {
		t.Fatalf("expected quiet=false")
	}
	SetVerbose(true)
	loggerMu.RLock()
	v := logger.Verbose
	loggerMu.RUnlock()
	if !v {
		t.Fatalf("expected verbose=true")
	}
}

func TestContextLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	local := &CustomLogger{
		LogLevel:      slog.LevelDebug,
		OutputType:    PlainOutput,
		ConsoleWriter: buf,
	}
	ctx := WithLogger(context.Background(), local)
	InfoContext(ctx, "info")
	WarnContext(ctx, "warn")
	DebugContext(ctx, "debug")
	ErrorContext(ctx, "error")

	output := buf.String()
	for _, want := range []string{"info", "warn", "debug", "error"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in output, got %q", want, output)
		}
	}
}

func TestOutputContextJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	local := &CustomLogger{
		OutputType:    JSONOutput,
		ConsoleWriter: &bytes.Buffer{},
		OutputWriter:  &buf,
	}
	ctx := WithLogger(context.Background(), local)
	OutputContext(ctx, map[string]string{"status": "ok"})
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected json output, got %q", buf.String())
	}
}

func TestFallbackLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	loggerMu.Lock()
	logger = &CustomLogger{LogLevel: slog.LevelInfo, ConsoleWriter: buf}
	loggerMu.Unlock()

	InfoContext(context.TODO(), "fallback")
	if !strings.Contains(buf.String(), "fallback") {
		t.Fatalf("expected fallback output")
	}
}

func TestNewCustomLoggerAndOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewCustomLogger(slog.LevelWarn)
	if logger.LogLevel != slog.LevelWarn {
		t.Fatalf("LogLevel = %v, want %v", logger.LogLevel, slog.LevelWarn)
	}
	if logger.OutputType != PlainOutput {
		t.Fatalf("OutputType = %v, want %v", logger.OutputType, PlainOutput)
	}
	if logger.ConsoleWriter == nil {
		t.Fatalf("expected ConsoleWriter to be set")
	}

	logger.OutputWriter = buf
	logger.Output("hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected output to contain hello")
	}
}

func TestCustomLoggerFormatMessageColor(t *testing.T) {
	logger := &CustomLogger{OutputType: ColorOutput}
	colored := logger.formatMessage(InfoLevel, "hi %s", "squad")
	if !strings.Contains(colored, "hi squad") {
		t.Fatalf("expected colored output to contain message")
	}
	logger.OutputType = PlainOutput
	plain := logger.formatMessage(InfoLevel, "hi")
	if plain != "hi" {
		t.Fatalf("plain output = %q, want %q", plain, "hi")
	}
}

func TestCustomLoggerOutputJSONError(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	logger := &CustomLogger{
		OutputType:    JSONOutput,
		OutputWriter:  &out,
		ConsoleWriter: &errOut,
		LogLevel:      slog.LevelInfo,
	}
	logger.Output(make(chan int))
	if !strings.Contains(errOut.String(), "Failed to encode JSON output") {
		t.Fatalf("expected json error output, got %q", errOut.String())
	}
}

func TestCustomLoggerErrorFormatsError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := &CustomLogger{ConsoleWriter: buf, LogLevel: slog.LevelDebug}
	logger.Error(fmt.Errorf("boom"), "extra")
	logger.Error(123)
	out := buf.String()
	if !strings.Contains(out, "boom extra") {
		t.Fatalf("expected formatted error, got %q", out)
	}
	if !strings.Contains(out, "123") {
		t.Fatalf("expected numeric error, got %q", out)
	}
}

func TestGlobalOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	loggerMu.Lock()
	logger = &CustomLogger{
		OutputType:    PlainOutput,
		OutputWriter:  buf,
		ConsoleWriter: &bytes.Buffer{},
	}
	loggerMu.Unlock()

	Output("global")
	if !strings.Contains(buf.String(), "global") {
		t.Fatalf("expected global output to be written")
	}
}

func TestOutputWriterDefaultsToStdout(t *testing.T) {
	logger := &CustomLogger{}
	if logger.outputWriter() != os.Stdout {
		t.Fatalf("expected outputWriter to default to stdout")
	}
}
