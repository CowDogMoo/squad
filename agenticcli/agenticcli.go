// Package agenticcli drives locally installed agentic coding CLIs — Claude
// Code (`claude`) and Augment's `agy` — in non-interactive print mode as
// squad model providers. The CLI owns the entire agent loop: its own tools,
// permission handling, and authentication (typically a subscription login).
// Squad hands it the assembled prompt bundle, waits for the final report,
// and never needs a provider API key.
package agenticcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cowdogmoo/squad/logging"
)

// agyPrintTimeout overrides agy's 5-minute default print-mode wait, which is
// far too short for a real agent run. Context cancellation still kills the
// subprocess earlier when squad's own deadline fires.
const agyPrintTimeout = "4h"

// readOnlyDisallowedTools mirrors squad's readonly gate (Write/Edit/MultiEdit
// rejected, reads and Bash allowed) in Claude Code's tool-permission terms.
const readOnlyDisallowedTools = "Write,Edit,MultiEdit,NotebookEdit"

// Spec describes one supported agentic CLI.
type Spec struct {
	// Provider is the squad provider name ("claude-code", "agy").
	Provider string
	// Binary is the executable looked up on PATH.
	Binary string
	// Install is a one-line install-and-login hint for missing-binary errors.
	Install string
}

var specs = map[string]Spec{
	"claude-code": {
		Provider: "claude-code",
		Binary:   "claude",
		Install:  "npm install -g @anthropic-ai/claude-code, then run `claude` once to log in",
	},
	"agy": {
		Provider: "agy",
		Binary:   "agy",
		Install:  "https://docs.augmentcode.com/cli — then run `agy` once to log in",
	},
}

// Lookup returns the Spec for a squad provider name. Callers pass the
// already-normalized provider string (lowercase, canonical aliases applied).
func Lookup(provider string) (Spec, bool) {
	s, ok := specs[provider]
	return s, ok
}

// Request carries one print-mode invocation.
type Request struct {
	// Provider selects the CLI ("claude-code" or "agy").
	Provider string
	// Model is passed through to the CLI's --model flag; empty uses the
	// CLI's own configured default, which is the common subscription case.
	Model string
	// SystemPrompt is squad's assembled agent system prompt.
	SystemPrompt string
	// UserPrompt is the task prompt.
	UserPrompt string
	// WorkDir is the directory the CLI runs in.
	WorkDir string
	// ReadOnly maps squad's readonly mode onto the CLI's native
	// restriction flags.
	ReadOnly bool
}

// Result is the parsed outcome of a print-mode run.
type Result struct {
	Response     string
	InputTokens  int64
	OutputTokens int64
	Turns        int
}

// Run executes the CLI for req and parses its JSON output. The subprocess
// inherits the parent environment so the CLI's own credential store keeps
// working.
func Run(ctx context.Context, req Request) (Result, error) {
	spec, ok := Lookup(req.Provider)
	if !ok {
		return Result{}, fmt.Errorf("unknown agentic CLI provider: %q", req.Provider)
	}
	path, err := exec.LookPath(spec.Binary)
	if err != nil {
		return Result{}, fmt.Errorf("provider %q requires the %q CLI on PATH (install: %s): %w",
			spec.Provider, spec.Binary, spec.Install, err)
	}

	args, stdin := buildArgs(req)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = req.WorkDir
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("%s CLI failed: %w%s", spec.Binary, err,
			outputTail(stderr.Bytes(), stdout.Bytes()))
	}
	return parseOutput(spec.Provider, stdout.Bytes())
}

// buildArgs maps a Request onto CLI-specific flags and returns the argv tail
// plus the stdin payload. Both CLIs run with permission prompts bypassed:
// squad runs are unattended, matching squad's own tool loop, which executes
// Bash and edits without prompting.
func buildArgs(req Request) (args []string, stdin string) {
	switch req.Provider {
	case "claude-code":
		args = []string{"--print", "--output-format", "json", "--dangerously-skip-permissions"}
		args = append(args, claudeCommonArgs(req)...)
		return args, req.UserPrompt
	case "agy":
		// agy has no separate system-prompt flag; prepend it to the task.
		prompt := req.UserPrompt
		if req.SystemPrompt != "" {
			prompt = req.SystemPrompt + "\n\n" + req.UserPrompt
		}
		args = []string{"--print", prompt, "--output-format", "json", "--print-timeout", agyPrintTimeout,
			"--dangerously-skip-permissions"}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		if req.ReadOnly {
			args = append(args, "--mode", "plan")
		}
		return args, ""
	}
	return nil, ""
}

// claudeCommonArgs returns the claude flags shared by the single-shot and
// live paths: system prompt, model, and the readonly tool restriction.
func claudeCommonArgs(req Request) []string {
	var args []string
	if req.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", req.SystemPrompt)
	}
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}
	if req.ReadOnly {
		args = append(args, "--disallowed-tools", readOnlyDisallowedTools)
	}
	return args
}

// claudeOutput is the envelope `claude -p --output-format json` prints.
type claudeOutput struct {
	IsError  bool   `json:"is_error"`
	Result   string `json:"result"`
	NumTurns int    `json:"num_turns"`
	Usage    struct {
		InputTokens              int64 `json:"input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
	} `json:"usage"`
}

// agyOutput is the envelope `agy --print --output-format json` prints.
type agyOutput struct {
	Status   string `json:"status"`
	Response string `json:"response"`
	NumTurns int    `json:"num_turns"`
	Usage    struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// parseOutput decodes the CLI's JSON envelope. A zero-exit run whose output
// isn't the expected JSON (e.g. an older CLI version) falls back to the raw
// stdout as the response rather than failing the run.
func parseOutput(provider string, stdout []byte) (Result, error) {
	switch provider {
	case "claude-code":
		var out claudeOutput
		if err := json.Unmarshal(stdout, &out); err != nil {
			logging.Warn("claude output is not the expected JSON envelope (%v); using raw output", err)
			return Result{Response: string(stdout)}, nil
		}
		return claudeResult(out)
	case "agy":
		var out agyOutput
		if err := json.Unmarshal(stdout, &out); err != nil {
			logging.Warn("agy output is not the expected JSON envelope (%v); using raw output", err)
			return Result{Response: string(stdout)}, nil
		}
		res := Result{
			Response:     out.Response,
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
			Turns:        out.NumTurns,
		}
		if !strings.EqualFold(out.Status, "SUCCESS") {
			return res, fmt.Errorf("agy run failed (status=%s): %s", out.Status, truncate(out.Response, 500))
		}
		return res, nil
	}
	return Result{}, fmt.Errorf("unknown agentic CLI provider: %q", provider)
}

// claudeResult maps the decoded claude envelope onto a Result. Shared by the
// single-shot path (whole-stdout envelope) and the live path (the terminal
// `result` stream event, whose fields are a superset of the envelope).
func claudeResult(out claudeOutput) (Result, error) {
	res := Result{
		Response:     out.Result,
		InputTokens:  out.Usage.InputTokens + out.Usage.CacheCreationInputTokens + out.Usage.CacheReadInputTokens,
		OutputTokens: out.Usage.OutputTokens,
		Turns:        out.NumTurns,
	}
	if out.IsError {
		return res, fmt.Errorf("claude run failed: %s", strings.TrimSpace(out.Result))
	}
	return res, nil
}

// outputTail formats the tail of stderr and stdout for error messages, so a
// failed CLI run surfaces its own diagnostics without dumping megabytes.
func outputTail(stderr, stdout []byte) string {
	var sb strings.Builder
	if s := strings.TrimSpace(string(stderr)); s != "" {
		sb.WriteString("\nstderr: ")
		sb.WriteString(tail(s, 2000))
	}
	if s := strings.TrimSpace(string(stdout)); s != "" {
		sb.WriteString("\nstdout: ")
		sb.WriteString(tail(s, 2000))
	}
	return sb.String()
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
