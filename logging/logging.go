/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// Package logging provides configurable logging utilities for squad.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/fatih/color"
)

var (
	logger   *CustomLogger
	loggerMu sync.RWMutex
)

// LogLevel represents the supported log severity levels.
type LogLevel int

// OutputType defines the available logger output formats.
type OutputType int

const (
	// PlainOutput emits uncolored text output.
	PlainOutput OutputType = iota
	// ColorOutput emits ANSI-colored text output.
	ColorOutput
	// JSONOutput emits structured JSON output.
	JSONOutput
)

const (
	// InfoLevel emits informational log entries.
	InfoLevel LogLevel = iota
	// WarnLevel emits warning log entries.
	WarnLevel
	// DebugLevel emits verbose debug log entries.
	DebugLevel
	// ErrorLevel emits error log entries.
	ErrorLevel
)

// CustomLogger wraps logging functionality with formatting options.
type CustomLogger struct {
	LogLevel      slog.Level
	OutputType    OutputType
	Quiet         bool
	ConsoleWriter io.Writer
	OutputWriter  io.Writer
	Verbose       bool
	Prefix        string // Optional prefix prepended to all log messages (e.g., "[bg-1] ")
}

// WithPrefix returns a shallow copy of the logger with the given prefix.
// The prefix is prepended to every log line, making it easy to distinguish
// output from different child agents or background tasks.
func (l *CustomLogger) WithPrefix(prefix string) *CustomLogger {
	cp := *l
	cp.Prefix = prefix
	return &cp
}

func (l *CustomLogger) formatMessage(level LogLevel, message string, args ...interface{}) string {
	formattedMsg := fmt.Sprintf(message, args...)
	if l.Prefix != "" {
		formattedMsg = l.Prefix + formattedMsg
	}

	if l.OutputType != ColorOutput {
		return formattedMsg
	}

	colorFunc := map[LogLevel]func(format string, a ...interface{}) string{
		InfoLevel:  color.GreenString,
		WarnLevel:  color.YellowString,
		DebugLevel: color.CyanString,
		ErrorLevel: color.RedString,
	}[level]

	if colorFunc == nil {
		return formattedMsg
	}

	return colorFunc("%s", formattedMsg)
}

func (l *CustomLogger) shouldShowOnConsole(level LogLevel) bool {
	if l.Quiet && level != ErrorLevel {
		return false
	}

	var slogLevel slog.Level
	switch level {
	case InfoLevel:
		slogLevel = slog.LevelInfo
	case WarnLevel:
		slogLevel = slog.LevelWarn
	case DebugLevel:
		slogLevel = slog.LevelDebug
	case ErrorLevel:
		slogLevel = slog.LevelError
	}

	if level == ErrorLevel || level == WarnLevel {
		return true
	}

	if level == InfoLevel {
		return l.LogLevel <= slogLevel
	}

	return (l.Verbose || l.LogLevel <= slog.LevelDebug) && l.LogLevel <= slogLevel
}

func (l *CustomLogger) log(level LogLevel, message string, args ...interface{}) {
	formattedMsg := l.formatMessage(level, message, args...)

	if l.shouldShowOnConsole(level) && l.ConsoleWriter != nil {
		_, _ = fmt.Fprintln(l.ConsoleWriter, formattedMsg)
	}
}

// NewCustomLogger creates a logger with the provided minimum slog level.
func NewCustomLogger(level slog.Level) *CustomLogger {
	return &CustomLogger{
		LogLevel:      level,
		Quiet:         false,
		ConsoleWriter: os.Stderr,
		Verbose:       false,
		OutputType:    PlainOutput,
	}
}

func (l *CustomLogger) SetQuiet(quiet bool) {
	l.Quiet = quiet
}

func (l *CustomLogger) SetVerbose(verbose bool) {
	l.Verbose = verbose
}

func (l *CustomLogger) Info(format string, args ...interface{}) {
	l.log(InfoLevel, format, args...)
}

func (l *CustomLogger) outputWriter() io.Writer {
	if l.OutputWriter != nil {
		return l.OutputWriter
	}
	return os.Stdout
}

// Output writes data using the logger's configured output format.
func (l *CustomLogger) Output(data interface{}) {
	w := l.outputWriter()
	switch l.OutputType {
	case JSONOutput:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(data); err != nil {
			l.Error("Failed to encode JSON output: %v", err)
		}
	default:
		if _, err := fmt.Fprintln(w, data); err != nil {
			l.Error("Failed to write output: %v", err)
		}
	}
}

func (l *CustomLogger) Warn(format string, args ...interface{}) {
	l.log(WarnLevel, format, args...)
}

func (l *CustomLogger) Debug(format string, args ...interface{}) {
	l.log(DebugLevel, format, args...)
}

func (l *CustomLogger) Error(firstArg interface{}, args ...interface{}) {
	var format string
	switch v := firstArg.(type) {
	case error:
		// Treat error as a value, not a format string
		format = v.Error()
		if len(args) > 0 {
			format = fmt.Sprintf("%s %v", format, fmt.Sprint(args...))
		}
	case string:
		format = v
	default:
		format = fmt.Sprintf("%v", v)
	}

	l.log(ErrorLevel, "%s", format)
}

// DetermineLogLevel maps a string level to its slog equivalent.
func DetermineLogLevel(levelStr string) slog.Level {
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Initialize sets up the global logger.
func Initialize(logLevelStr, outputFormat string, quiet, verbose bool) error {
	logLevel := DetermineLogLevel(logLevelStr)

	outputType := PlainOutput
	switch outputFormat {
	case "json":
		outputType = JSONOutput
	case "color":
		outputType = ColorOutput
	case "text", "plain":
		outputType = PlainOutput
	}

	if verbose {
		if logLevel > slog.LevelDebug {
			logLevel = slog.LevelDebug
		}
	}

	loggerMu.Lock()
	logger = &CustomLogger{
		LogLevel:      logLevel,
		OutputType:    outputType,
		Quiet:         quiet,
		ConsoleWriter: os.Stderr,
		Verbose:       verbose,
	}
	loggerMu.Unlock()

	return nil
}

func ensureLogger() {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	if logger == nil {
		logger = &CustomLogger{
			LogLevel:      slog.LevelInfo,
			OutputType:    PlainOutput,
			Quiet:         false,
			ConsoleWriter: os.Stderr,
			Verbose:       false,
		}
	}
}

func logGlobal(level LogLevel, message string, args ...interface{}) {
	ensureLogger()
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.log(level, message, args...)
}

func Info(message string, args ...interface{}) {
	logGlobal(InfoLevel, message, args...)
}

// Output writes data using the global logger's output format.
func Output(data interface{}) {
	ensureLogger()
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Output(data)
}

func Warn(message string, args ...interface{}) {
	logGlobal(WarnLevel, message, args...)
}

func Error(firstArg interface{}, args ...interface{}) {
	ensureLogger()
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error(firstArg, args...)
}

func Debug(message string, args ...interface{}) {
	logGlobal(DebugLevel, message, args...)
}

func SetQuiet(quiet bool) {
	ensureLogger()
	loggerMu.Lock()
	defer loggerMu.Unlock()
	logger.SetQuiet(quiet)
}

// IsQuiet reports whether the global logger suppresses non-error output.
func IsQuiet() bool {
	ensureLogger()
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger.Quiet
}

func SetVerbose(verbose bool) {
	ensureLogger()
	loggerMu.Lock()
	defer loggerMu.Unlock()
	logger.SetVerbose(verbose)
}

// Context-based logging support

type loggerKeyType struct{}

var loggerKey = loggerKeyType{}

// WithLogger returns a context that carries the provided logger.
func WithLogger(ctx context.Context, l *CustomLogger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

// FromContext returns the logger stored in ctx or the global logger.
func FromContext(ctx context.Context) *CustomLogger {
	if ctx == nil {
		ensureLogger()
		loggerMu.RLock()
		defer loggerMu.RUnlock()
		return logger
	}

	if l, ok := ctx.Value(loggerKey).(*CustomLogger); ok && l != nil {
		return l
	}

	ensureLogger()
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger
}

func InfoContext(ctx context.Context, message string, args ...interface{}) {
	FromContext(ctx).Info(message, args...)
}

func WarnContext(ctx context.Context, message string, args ...interface{}) {
	FromContext(ctx).Warn(message, args...)
}

func DebugContext(ctx context.Context, message string, args ...interface{}) {
	FromContext(ctx).Debug(message, args...)
}

func ErrorContext(ctx context.Context, firstArg interface{}, args ...interface{}) {
	FromContext(ctx).Error(firstArg, args...)
}

func OutputContext(ctx context.Context, data interface{}) {
	FromContext(ctx).Output(data)
}
