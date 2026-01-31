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
	logger     *CustomLogger
	loggerOnce sync.Once
	loggerMu   sync.RWMutex
)

type LogLevel int

type OutputType int

const (
	PlainOutput OutputType = iota
	ColorOutput
	JSONOutput
)

const (
	InfoLevel LogLevel = iota
	WarnLevel
	DebugLevel
	ErrorLevel
)

// CustomLogger wraps logging functionality with formatting options.
type CustomLogger struct {
	LogLevel      slog.Level
	OutputType    OutputType
	Quiet         bool
	ConsoleWriter io.Writer
	Verbose       bool
}

func (l *CustomLogger) formatMessage(level LogLevel, message string, args ...interface{}) string {
	formattedMsg := fmt.Sprintf(message, args...)

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

func (l *CustomLogger) Output(data interface{}) {
	switch l.OutputType {
	case JSONOutput:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(data); err != nil {
			l.Error("Failed to encode JSON output: %v", err)
		}
	default:
		if _, err := fmt.Fprintln(os.Stdout, data); err != nil {
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
		if len(args) == 0 {
			format = v.Error()
		} else {
			format = fmt.Sprintf(v.Error(), args...)
		}
	case string:
		format = v
	default:
		format = fmt.Sprintf("%v", v)
	}

	l.log(ErrorLevel, format, args...)
}

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

func ensureLogger() error {
	loggerOnce.Do(func() {
		logger = &CustomLogger{
			LogLevel:      slog.LevelInfo,
			OutputType:    PlainOutput,
			Quiet:         false,
			ConsoleWriter: os.Stderr,
			Verbose:       false,
		}
	})
	return nil
}

func logGlobal(level LogLevel, message string, args ...interface{}) {
	if err := ensureLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return
	}
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.log(level, message, args...)
}

func Info(message string, args ...interface{}) {
	logGlobal(InfoLevel, message, args...)
}

func Output(data interface{}) {
	if err := ensureLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return
	}
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Output(data)
}

func Warn(message string, args ...interface{}) {
	logGlobal(WarnLevel, message, args...)
}

func Error(firstArg interface{}, args ...interface{}) {
	if err := ensureLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return
	}

	loggerMu.RLock()
	defer loggerMu.RUnlock()
	logger.Error(firstArg, args...)
}

func Debug(message string, args ...interface{}) {
	logGlobal(DebugLevel, message, args...)
}

func SetQuiet(quiet bool) {
	if err := ensureLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return
	}
	loggerMu.Lock()
	defer loggerMu.Unlock()
	logger.SetQuiet(quiet)
}

func IsQuiet() bool {
	if err := ensureLogger(); err != nil {
		return false
	}
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger.Quiet
}

func SetVerbose(verbose bool) {
	if err := ensureLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		return
	}
	loggerMu.Lock()
	defer loggerMu.Unlock()
	logger.SetVerbose(verbose)
}

// Context-based logging support

type loggerKeyType struct{}

var loggerKey = loggerKeyType{}

func WithLogger(ctx context.Context, l *CustomLogger) context.Context {
	return context.WithValue(ctx, loggerKey, l)
}

func FromContext(ctx context.Context) *CustomLogger {
	if ctx == nil {
		if err := ensureLogger(); err != nil {
			return NewCustomLogger(slog.LevelInfo)
		}
		loggerMu.RLock()
		defer loggerMu.RUnlock()
		return logger
	}

	if l, ok := ctx.Value(loggerKey).(*CustomLogger); ok && l != nil {
		return l
	}

	if err := ensureLogger(); err != nil {
		return NewCustomLogger(slog.LevelInfo)
	}
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
