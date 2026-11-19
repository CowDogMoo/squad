package telemetry

import (
	"context"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
)

// ExportCommandsEnv opts in, when set to "1", to exporting full command text
// and the text of errors that may carry tool arguments (see
// RecordContentError) on spans. Otherwise command attributes carry only the
// executable name and those errors a fixed status.
const ExportCommandsEnv = "SQUAD_OTEL_EXPORT_COMMANDS"

// maxExportedCommandBytes caps the opt-in command text.
const maxExportedCommandBytes = 1000

// RedactedMarker replaces values removed by the opt-in redaction.
const RedactedMarker = "[REDACTED]"

// ComplexCommand is exported in place of an executable name when the first
// word of a command is not a plain name or path.
const ComplexCommand = "complex"

// exportFullCommands reports whether ExportCommandsEnv is set to "1".
func exportFullCommands() bool {
	return os.Getenv(ExportCommandsEnv) == "1"
}

// CommandAttributes returns the span attributes describing a shell command:
// <prefix>.command and <prefix>.command_length (the full command's length in
// bytes). By default <prefix>.command holds only CommandName(command). With
// SQUAD_OTEL_EXPORT_COMMANDS=1, it holds the full command after
// RedactCommand, truncated to 1000 bytes.
func CommandAttributes(prefix, command string) []attribute.KeyValue {
	value := CommandName(command)
	if exportFullCommands() {
		value = truncate(RedactCommand(command), maxExportedCommandBytes)
	}
	return []attribute.KeyValue{
		attribute.String(prefix+".command", value),
		attribute.Int(prefix+".command_length", len(command)),
	}
}

type agentCommandKey struct{}

// WithAgentCommand returns a copy of ctx recording command as the agent wrote
// it. Callers that wrap the command before passing it to an executor use it
// so the executor span describes the agent's command, not the wrapper.
func WithAgentCommand(ctx context.Context, command string) context.Context {
	return context.WithValue(ctx, agentCommandKey{}, command)
}

// AgentCommand returns the command recorded on ctx by WithAgentCommand, or
// command when none was recorded.
func AgentCommand(ctx context.Context, command string) string {
	if c, ok := ctx.Value(agentCommandKey{}).(string); ok {
		return c
	}
	return command
}

// commandPathRe matches a first word made only of name and path characters;
// its group is the basename.
var commandPathRe = regexp.MustCompile(`^(?:[A-Za-z0-9._+/-]*/)?([A-Za-z0-9._+-]+)$`)

// CommandName returns the basename of the first whitespace-separated word of
// command when that word is a plain name or path made of letters, digits and
// . _ + - /, and ComplexCommand otherwise. Shell syntax is not parsed, so
// assignments, quotes, expansions and comments in the first word all yield
// ComplexCommand.
func CommandName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ComplexCommand
	}
	m := commandPathRe.FindStringSubmatch(fields[0])
	if m == nil {
		return ComplexCommand
	}
	return m[1]
}

// opt-in redaction rules, applied in order.
var (
	bearerRe   = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9\-._~+/]+=*`)
	jwtRe      = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}(?:\.[A-Za-z0-9_-]+){1,2}`)
	userinfoRe = regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@'"]+):[^/\s@'"]+@`)
	keyValueRe = regexp.MustCompile(`(?i)([\w.-]*(?:password|passwd|token|secret|api[_-]?key)[\w.-]*)(["']?\s*[=:]\s*["']?)[^\s&;,'"]+`)
)

// RedactCommand applies a small, generic, best-effort redaction to command
// text: Bearer values, JWTs, user:pass@ in URLs, and values of
// password/token/secret/api_key style keys written as key=value or
// key: value. It has no tool-specific rules and will miss credentials in
// other forms; it is used only under the SQUAD_OTEL_EXPORT_COMMANDS=1
// opt-in, on command text and on error text (see RecordContentError).
func RedactCommand(s string) string {
	s = bearerRe.ReplaceAllString(s, "$1 "+RedactedMarker)
	s = jwtRe.ReplaceAllString(s, RedactedMarker)
	s = userinfoRe.ReplaceAllString(s, "$1:"+RedactedMarker+"@")
	s = keyValueRe.ReplaceAllString(s, "$1$2"+RedactedMarker)
	return s
}

// truncate cuts s to at most maxLen bytes on a rune boundary, appending "..."
// when it does.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	for maxLen > 0 && !utf8.RuneStart(s[maxLen]) {
		maxLen--
	}
	return s[:maxLen] + "..."
}
