// Package agenticcli drives locally installed agentic coding CLIs — Claude
// Code (`claude`) and Google Antigravity's `agy` — in non-interactive print
// mode as squad model providers. The CLI owns the entire agent loop: its own
// tools, permission handling, and authentication (typically a subscription
// login). Squad hands it the assembled prompt bundle, waits for the final
// report, and never needs a provider API key.
package agenticcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cowdogmoo/squad/logging"
)

// antigravityPrintTimeout overrides agy's 5-minute default print-mode wait,
// which is far too short for a real agent run. Context cancellation still
// kills the subprocess earlier when squad's own deadline fires.
const antigravityPrintTimeout = "4h"

// readOnlyDisallowedTools mirrors squad's readonly gate (Write/Edit/MultiEdit
// rejected, reads and Bash allowed) in Claude Code's tool-permission terms.
const readOnlyDisallowedTools = "Write,Edit,MultiEdit,NotebookEdit"

// Spec describes one supported agentic CLI.
type Spec struct {
	// Provider is the squad provider name ("claude-code", "antigravity").
	Provider string
	// Binary is the executable looked up on PATH.
	Binary string
	// Install is a one-line install-and-login hint for missing-binary errors.
	Install string
	// EnforcesReadOnly reports whether the CLI has a native flag that
	// actually blocks edits. Antigravity does not: its print mode runs with
	// permissions auto-approved and `--mode plan` still writes files
	// (verified against agy 1.1.13), so squad refuses readonly runs rather
	// than silently dropping the guarantee.
	EnforcesReadOnly bool
}

var specs = map[string]Spec{
	"claude-code": {
		Provider:         "claude-code",
		Binary:           "claude",
		Install:          "npm install -g @anthropic-ai/claude-code, then run `claude` once to log in",
		EnforcesReadOnly: true,
	},
	"antigravity": {
		Provider: "antigravity",
		Binary:   "agy",
		Install:  "install the Antigravity CLI from https://antigravity.google, then run `agy` once to log in",
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
	// Provider selects the CLI ("claude-code" or "antigravity").
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
	// restriction flags. Run rejects it for CLIs that cannot enforce it.
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
	if req.ReadOnly && !spec.EnforcesReadOnly {
		return Result{}, fmt.Errorf("provider %q cannot enforce read-only mode: the %s CLI's print mode auto-approves permissions and its plan mode does not block writes; use claude-code or an API provider for readonly runs",
			spec.Provider, spec.Binary)
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
	case "antigravity":
		// agy has no separate system-prompt flag; prepend it to the task.
		parts := make([]string, 0, 3)
		if req.SystemPrompt != "" {
			parts = append(parts, req.SystemPrompt)
		}
		workDir := absWorkDir(req.WorkDir)
		if workDir != "" {
			parts = append(parts, workDirNote(workDir))
		}
		parts = append(parts, req.UserPrompt)
		args = []string{"--print", strings.Join(parts, "\n\n"), "--output-format", "json",
			"--print-timeout", antigravityPrintTimeout, "--dangerously-skip-permissions"}
		if workDir != "" {
			args = append(args, "--add-dir", workDir)
		}
		if req.Model != "" {
			args = append(args, "--model", req.Model)
		}
		return args, ""
	}
	return nil, ""
}

// workDirNote pins the agent to squad's working directory. Without it the
// Antigravity backend roots the session in its own workspace (~/.gemini/
// antigravity-cli/scratch), so relative file paths land there and
// run_command executes in the backend's cwd — not in WorkDir, which the
// child process's cwd does not influence (verified against agy 1.1.13).
func workDirNote(workDir string) string {
	return "## Working directory\n\n" +
		"Your working directory for this task is " + workDir + ". " +
		"Resolve every relative path against it and create or edit files only under it unless the task says otherwise. " +
		"Your shell does NOT start there, so begin every shell command with: cd " + workDir
}

// absWorkDir best-effort resolves dir to an absolute path; the prompt and
// --add-dir must not depend on a cwd the Antigravity backend ignores.
func absWorkDir(dir string) string {
	if dir == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// NormalizeAntigravityModel maps a manifest Gemini model name onto
// Antigravity's tiered model IDs. The CLI serves Gemini models with a
// reasoning-effort suffix (gemini-3.1-pro-low, gemini-3.7-flash-high, …) and
// rejects bare API names ("--model gemini-3.1-pro requires --effort"), so a
// borrowed entry gets the CLI's cheapest tier appended; API-only -preview/
// -latest suffixes are dropped first. Non-Gemini names pass through
// untouched.
func NormalizeAntigravityModel(model string) string {
	if !strings.HasPrefix(strings.ToLower(model), "gemini-") {
		return model
	}
	m := strings.TrimSuffix(model, "-preview")
	m = strings.TrimSuffix(m, "-latest")
	switch {
	case strings.HasSuffix(m, "-low"), strings.HasSuffix(m, "-medium"), strings.HasSuffix(m, "-high"):
		return m
	}
	return m + "-low"
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

// antigravityOutput is the envelope `agy --print --output-format json`
// prints. Failures may carry the diagnostic in `error` with an empty
// `response` (e.g. an invalid --model selection).
type antigravityOutput struct {
	Status   string `json:"status"`
	Response string `json:"response"`
	Error    string `json:"error"`
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
	case "antigravity":
		var out antigravityOutput
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
			detail := out.Error
			if detail == "" {
				detail = out.Response
			}
			return res, fmt.Errorf("antigravity run failed (status=%s): %s", out.Status, truncate(detail, 500))
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
