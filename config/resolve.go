package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const resolveTimeout = 10 * time.Second

// ResolveValue performs variable and command substitution on a config string.
//
// Supported patterns:
//   - $VAR or ${VAR} — replaced by the environment variable value
//   - $(command args) — replaced by the stdout of the command
//
// Unset variables and failed commands return errors rather than empty strings,
// so misconfigurations are caught early.
func ResolveValue(s string) (string, error) {
	if !strings.ContainsAny(s, "$") {
		return s, nil
	}

	// Process command substitutions first.
	resolved, err := resolveCommands(s)
	if err != nil {
		return "", err
	}

	// Then process environment variables.
	return resolveEnvVars(resolved)
}

// resolveCommands replaces all $(command) patterns.
func resolveCommands(s string) (string, error) {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '(' {
			// Find matching close paren, tracking nesting.
			depth := 1
			start := i + 2
			j := start
			for j < len(s) && depth > 0 {
				switch s[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth > 0 {
					j++
				}
			}
			if depth != 0 {
				return "", fmt.Errorf("unmatched $( in config value: %q", s)
			}
			cmd := s[start:j]
			output, err := runCommand(cmd)
			if err != nil {
				return "", fmt.Errorf("command substitution $(%.40s) failed: %w", cmd, err)
			}
			result.WriteString(output)
			i = j + 1
		} else {
			result.WriteByte(s[i])
			i++
		}
	}
	return result.String(), nil
}

// resolveEnvVars replaces $VAR and ${VAR} patterns.
func resolveEnvVars(s string) (string, error) {
	var result strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '$' {
			result.WriteByte(s[i])
			i++
			continue
		}

		// Skip $$ (literal dollar sign).
		if i+1 < len(s) && s[i+1] == '$' {
			result.WriteByte('$')
			i += 2
			continue
		}

		// ${VAR} form.
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", fmt.Errorf("unmatched ${ in config value: %q", s)
			}
			varName := s[i+2 : i+2+end]
			val, ok := os.LookupEnv(varName)
			if !ok {
				return "", fmt.Errorf("unset environment variable ${%s} in config value", varName)
			}
			result.WriteString(val)
			i = i + 2 + end + 1
			continue
		}

		// $VAR form — collect alphanumeric + underscore.
		j := i + 1
		for j < len(s) && isVarChar(s[j]) {
			j++
		}
		if j == i+1 {
			// Lone $ not followed by valid var char — keep literal.
			result.WriteByte('$')
			i++
			continue
		}
		varName := s[i+1 : j]
		val, ok := os.LookupEnv(varName)
		if !ok {
			return "", fmt.Errorf("unset environment variable $%s in config value", varName)
		}
		result.WriteString(val)
		i = j
	}
	return result.String(), nil
}

func isVarChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// runCommand executes a shell command and returns its trimmed stdout.
func runCommand(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
