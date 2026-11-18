package telemetry

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// ExportCommandsEnv opts in to exporting full command text on spans. Unset,
// command attributes carry only executable names.
const ExportCommandsEnv = "SQUAD_OTEL_EXPORT_COMMANDS"

// maxExportedCommandBytes caps the opt-in command text and the default
// executable-name summary.
const maxExportedCommandBytes = 1000

// RedactedMarker replaces values removed by the opt-in redaction.
const RedactedMarker = "[REDACTED]"

// exportFullCommands reports whether ExportCommandsEnv is set to a true value.
func exportFullCommands() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(ExportCommandsEnv))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// CommandAttributes returns the span attributes describing a shell command:
// <prefix>.command and <prefix>.command_length (the full command's length in
// bytes). By default <prefix>.command holds only the executable names, never
// arguments, so credentials in a command cannot leave the process. With
// SQUAD_OTEL_EXPORT_COMMANDS set, it holds the full command after
// RedactCommand, truncated to 1000 bytes.
func CommandAttributes(prefix, command string) []attribute.KeyValue {
	value := CommandNames(command)
	if exportFullCommands() {
		value = truncate(RedactCommand(command), maxExportedCommandBytes)
	}
	return []attribute.KeyValue{
		attribute.String(prefix+".command", value),
		attribute.Int(prefix+".command_length", len(command)),
	}
}

// CommandNames returns the executable name of each command in a shell
// command line, space separated: the basename of the first word of every
// segment split on unquoted |, ||, &&, ;, & and newlines. Leading VAR=value
// assignments are skipped, quoting is respected, and heredoc bodies are
// ignored, so no argument text is ever returned.
func CommandNames(command string) string {
	var names []string
	for _, word := range firstWords(command) {
		names = append(names, executableName(word))
	}
	return truncate(strings.Join(names, " "), maxExportedCommandBytes)
}

// maxNameBytes caps a single executable name in the summary.
const maxNameBytes = 64

// executableName reduces a command word to its basename. Command
// substitutions are replaced by a placeholder because their text is
// arbitrary shell, not a name.
func executableName(word string) string {
	if strings.Contains(word, "$(") || strings.Contains(word, "`") {
		return "$(...)"
	}
	name := filepath.Base(strings.Trim(word, `"'`))
	return truncate(name, maxNameBytes)
}

// firstWords scans a shell command line and returns the first non-assignment
// word of each command segment.
func firstWords(s string) []string {
	var (
		words      []string
		word       strings.Builder
		inWord     bool
		quote      byte // active quote character, 0 when unquoted
		haveFirst  bool // first word of the current segment already taken
		heredocs   []string
		pendingTag bool // saw <<, next word is the heredoc delimiter
	)

	endWord := func() {
		if !inWord {
			return
		}
		w := word.String()
		word.Reset()
		inWord = false
		switch {
		case pendingTag:
			heredocs = append(heredocs, strings.Trim(w, `"'`))
			pendingTag = false
		case !haveFirst && !isAssignment(w):
			words = append(words, w)
			haveFirst = true
		}
	}
	newSegment := func() {
		endWord()
		haveFirst = false
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			word.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				word.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			inWord = true
			word.WriteByte(c)
		case '\\':
			if i+1 < len(s) && s[i+1] == '\n' {
				endWord()
				i++
				continue
			}
			inWord = true
			word.WriteByte(c)
			if i+1 < len(s) {
				i++
				word.WriteByte(s[i])
			}
		case ' ', '\t', '\r':
			endWord()
		case '\n':
			newSegment()
			if len(heredocs) > 0 {
				i = skipHeredocs(s, i+1, heredocs) - 1
				heredocs = nil
			}
		case '|', ';', '&':
			newSegment()
		case '(', '{':
			// Subshell and group openers start a new command.
			if !inWord {
				newSegment()
				continue
			}
			word.WriteByte(c)
		case ')', '}':
			newSegment()
		case '<':
			endWord()
			if i+1 < len(s) && s[i+1] == '<' {
				i++
				if i+1 < len(s) && s[i+1] == '<' { // here-string <<<
					i++
					continue
				}
				if i+1 < len(s) && s[i+1] == '-' {
					i++
				}
				pendingTag = true
			}
		case '>':
			endWord()
			if i+1 < len(s) && s[i+1] == '&' { // 2>&1, >&2
				i++
			}
		case '$':
			inWord = true
			word.WriteByte(c)
			if i+1 < len(s) && s[i+1] == '(' {
				// Keep a $(...) substitution, spaces and all, inside one word.
				i = copySubstitution(s, i+1, &word) - 1
			} else if i+1 < len(s) && s[i+1] == '{' {
				// Keep a ${VAR} expansion inside one word.
				end := strings.IndexByte(s[i+1:], '}')
				if end < 0 {
					end = len(s) - i - 2
				}
				word.WriteString(s[i+1 : i+end+2])
				i += end + 1
			}
		case '`':
			inWord = true
			end := strings.IndexByte(s[i+1:], '`')
			if end < 0 {
				word.WriteString(s[i:])
				i = len(s)
				continue
			}
			word.WriteString(s[i : i+end+2])
			i += end + 1
		default:
			inWord = true
			word.WriteByte(c)
		}
	}
	endWord()
	return words
}

// skipHeredocs returns the index just past the heredoc bodies that start at
// s[start], one body per delimiter in order.
func skipHeredocs(s string, start int, delims []string) int {
	i := start
	for _, delim := range delims {
		for i < len(s) {
			end := strings.IndexByte(s[i:], '\n')
			line := s[i:]
			next := len(s)
			if end >= 0 {
				line = s[i : i+end]
				next = i + end + 1
			}
			i = next
			if strings.TrimLeft(strings.TrimRight(line, "\r"), "\t") == delim {
				break
			}
		}
	}
	return i
}

// copySubstitution copies the parenthesised text starting at s[open] (a
// '(') through its matching ')' into w and returns the index just past it.
func copySubstitution(s string, open int, w *strings.Builder) int {
	depth := 0
	var quote byte
	for i := open; i < len(s); i++ {
		c := s[i]
		w.WriteByte(c)
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

var assignmentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// isAssignment reports whether w is a VAR=value prefix assignment.
func isAssignment(w string) bool {
	return assignmentRe.MatchString(w)
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
// other forms; it is used only on the opt-in full-command path.
func RedactCommand(s string) string {
	s = bearerRe.ReplaceAllString(s, "$1 "+RedactedMarker)
	s = jwtRe.ReplaceAllString(s, RedactedMarker)
	s = userinfoRe.ReplaceAllString(s, "$1:"+RedactedMarker+"@")
	s = keyValueRe.ReplaceAllString(s, "$1$2"+RedactedMarker)
	return s
}

// truncate cuts s to maxLen bytes, appending "..." when it does.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
