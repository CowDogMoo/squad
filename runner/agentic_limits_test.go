package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/skill"
	"github.com/cowdogmoo/squad/tools"
	"github.com/spf13/cobra"
)

func TestClaudeTools(t *testing.T) {
	t.Parallel()
	skills := []skill.Entry{{Dir: "/skills/a"}}
	tests := []struct {
		name     string
		bundle   agent.Bundle
		readOnly bool
		want     []string
	}{
		{"transform gets nothing", agent.Bundle{Transform: true, RemoteOnly: true, DisableTask: true}, true, []string{}},
		{"local agent", agent.Bundle{}, false, []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write", "Task"}},
		{"local agent without Task", agent.Bundle{DisableTask: true}, false, []string{"Bash", "Edit", "Glob", "Grep", "Read", "Write"}},
		{"readonly drops edit tools", agent.Bundle{DisableTask: true}, true, []string{"Bash", "Glob", "Grep", "Read"}},
		{"remote-only agent", agent.Bundle{RemoteOnly: true, DisableTask: true}, false, []string{}},
		{"remote-only agent with skills reads SKILL.md", agent.Bundle{RemoteOnly: true, DisableTask: true, SkillEntries: skills}, false, []string{"Read"}},
		{"remote-only agent with Task", agent.Bundle{RemoteOnly: true}, false, []string{"Task"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := claudeTools(&tt.bundle, tt.readOnly)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("claudeTools = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaudeMCPConfig(t *testing.T) {
	t.Parallel()
	doc, names, err := claudeMCPConfig([]mcp.ServerConfig{
		{Name: "gdrive", Command: "/opt/google-mcp", Args: []string{"drive"}, Env: []string{"TOKEN=a=b"}},
		{Name: "burp", Transport: "sse", URL: "http://localhost:9876", Headers: []string{"X-Key=1"}},
		{Name: "hosted", Transport: "streamable_http", URL: "https://example.test/mcp"},
	})
	if err != nil {
		t.Fatalf("claudeMCPConfig: %v", err)
	}
	if want := []string{"gdrive", "burp", "hosted"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
	var got map[string]map[string]map[string]any
	if err := json.Unmarshal(doc, &got); err != nil {
		t.Fatalf("config is not JSON: %v", err)
	}
	servers := got["mcpServers"]
	if s := servers["gdrive"]; s["type"] != "stdio" || s["command"] != "/opt/google-mcp" ||
		!reflect.DeepEqual(s["args"], []any{"drive"}) || !reflect.DeepEqual(s["env"], map[string]any{"TOKEN": "a=b"}) {
		t.Errorf("gdrive = %v", s)
	}
	if s := servers["burp"]; s["type"] != "sse" || s["url"] != "http://localhost:9876" ||
		!reflect.DeepEqual(s["headers"], map[string]any{"X-Key": "1"}) {
		t.Errorf("burp = %v", s)
	}
	if s := servers["hosted"]; s["type"] != "http" || s["url"] != "https://example.test/mcp" {
		t.Errorf("hosted = %v", s)
	}
}

func TestClaudeMCPConfigRejectsUntranslatable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		servers []mcp.ServerConfig
		want    string
	}{
		{"stdio without command", []mcp.ServerConfig{{Name: "gdrive"}}, `MCP server "gdrive" cannot be passed to the claude CLI: stdio transport needs a command`},
		{"http without url", []mcp.ServerConfig{{Name: "h", Transport: "streamable_http"}}, "streamable_http transport needs a url"},
		{"unknown transport", []mcp.ServerConfig{{Name: "w", Transport: "websocket", URL: "ws://x"}}, `unsupported transport "websocket"`},
		{"bad env", []mcp.ServerConfig{{Name: "e", Command: "x", Env: []string{"NOEQUALS"}}}, `env entry "NOEQUALS" is not KEY=VALUE`},
		{"bad header", []mcp.ServerConfig{{Name: "h", Transport: "sse", URL: "http://x", Headers: []string{"=v"}}}, `header entry "=v" is not KEY=VALUE`},
		{"no name", []mcp.ServerConfig{{Command: "x"}}, "the server has no name"},
		{"duplicate", []mcp.ServerConfig{{Name: "d", Command: "x"}, {Name: "d", Command: "y"}}, `MCP server "d" is configured twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := claudeMCPConfig(tt.servers)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// installArgvRecordingCLI installs a fake claude that writes its argv (one
// per line) to args.txt and copies any --mcp-config file to mcp.json, then
// prints result.
func installArgvRecordingCLI(t *testing.T, result string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\n" +
		`printf '%s\n' "$@" > "` + filepath.Join(dir, "args.txt") + `"` + "\n" +
		`prev=""; for a in "$@"; do if [ "$prev" = "--mcp-config" ]; then cp "$a" "` + filepath.Join(dir, "mcp.json") + `"; fi; prev="$a"; done` + "\n" +
		`printf '%s' '` + result + `'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

const okResult = `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"done","usage":{"input_tokens":1,"output_tokens":1}}`

func readArgs(t *testing.T, dir string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// flagValue returns the argument after flag, and whether flag was present.
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
	}
	return "", false
}

func TestInvokeAgenticCLITransformRunsWithoutTools(t *testing.T) {
	dir := installArgvRecordingCLI(t, okResult)
	bundle := agent.BuildTransformBundle("diff --git a/x b/x", t.TempDir())
	// A transform never gets MCP servers, even ones passed on the command line.
	opts := &RunOptions{Mode: "readonly", MaxIterations: 100, MaxCost: 0.5,
		MCPServers: []mcp.ServerConfig{{Name: "srv", Command: "x"}}}
	if _, _, err := invokeAgenticCLI(tools.InitEdits(context.Background()), opts, "claude-code", "", "sys", bundle, metrics.New("claude-code", "")); err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	args := readArgs(t, dir)
	if v, ok := flagValue(args, "--tools"); !ok || v != "" {
		t.Errorf("--tools = %q (present %v), want empty value: no built-in tools", v, ok)
	}
	if v, _ := flagValue(args, "--max-turns"); v != "100" {
		t.Errorf("--max-turns = %q, want 100", v)
	}
	if v, _ := flagValue(args, "--max-budget-usd"); v != "0.5" {
		t.Errorf("--max-budget-usd = %q, want 0.5", v)
	}
	if _, ok := flagValue(args, "--strict-mcp-config"); !ok {
		t.Error("--strict-mcp-config missing")
	}
	if _, ok := flagValue(args, "--mcp-config"); ok {
		t.Error("transform passed --mcp-config, want no MCP servers")
	}
}

func TestInvokeAgenticCLIAgentRunGetsManifestLimits(t *testing.T) {
	dir := installArgvRecordingCLI(t, okResult)
	repo := initTestGitRepo(t)
	bundle := &agent.Bundle{
		WorkDir: repo, User: "task", MaxIterations: 60, DisableTask: true,
		MCPServers: []mcp.ServerConfig{{Name: "gdrive", Command: "/opt/google-mcp", Args: []string{"drive"}}},
	}
	opts := &RunOptions{MaxIterations: 100, MaxCost: 2}
	if _, _, err := invokeAgenticCLI(tools.InitEdits(context.Background()), opts, "claude-code", "", "sys", bundle, metrics.New("claude-code", "")); err != nil {
		t.Fatalf("invokeAgenticCLI: %v", err)
	}
	args := readArgs(t, dir)
	if v, _ := flagValue(args, "--tools"); v != "Bash,Edit,Glob,Grep,Read,Write" {
		t.Errorf("--tools = %q", v)
	}
	if v, _ := flagValue(args, "--max-turns"); v != "60" {
		t.Errorf("--max-turns = %q, want the manifest's 60", v)
	}
	if v, _ := flagValue(args, "--max-budget-usd"); v != "2" {
		t.Errorf("--max-budget-usd = %q, want 2", v)
	}
	path, ok := flagValue(args, "--mcp-config")
	if !ok {
		t.Fatal("--mcp-config missing")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("MCP config %s still exists after the run (stat err %v)", path, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if err != nil {
		t.Fatalf("CLI did not see the MCP config: %v", err)
	}
	if !strings.Contains(string(b), `"gdrive":{"type":"stdio","command":"/opt/google-mcp","args":["drive"]}`) {
		t.Errorf("MCP config = %s", b)
	}
}

func TestInvokeAgenticCLIFailsOnUntranslatableMCP(t *testing.T) {
	dir := installArgvRecordingCLI(t, okResult)
	bundle := &agent.Bundle{WorkDir: t.TempDir(), User: "task", MCPServers: []mcp.ServerConfig{{Name: "gdrive"}}}
	_, _, err := invokeAgenticCLI(tools.InitEdits(context.Background()), &RunOptions{}, "claude-code", "", "sys", bundle, metrics.New("claude-code", ""))
	if err == nil || !strings.Contains(err.Error(), `MCP server "gdrive" cannot be passed to the claude CLI`) {
		t.Fatalf("err = %v, want MCP translation failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "args.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("CLI ran despite the MCP failure")
	}
}

func TestInvokeAgenticCLISpendCapIsBudgetStop(t *testing.T) {
	installArgvRecordingCLI(t, `{"type":"result","subtype":"error_max_budget_usd","is_error":true,"num_turns":3,"errors":["Reached maximum budget ($2)"],"usage":{"input_tokens":1,"output_tokens":1}}`)
	bundle := &agent.Bundle{WorkDir: t.TempDir(), User: "task"}
	_, _, err := invokeAgenticCLI(tools.InitEdits(context.Background()), &RunOptions{MaxCost: 2}, "claude-code", "", "sys", bundle, metrics.New("claude-code", ""))
	if !errors.Is(err, metrics.ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	if !strings.Contains(err.Error(), "Reached maximum budget ($2)") {
		t.Errorf("err = %v, want the CLI's reason", err)
	}
}

func TestReportedStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, response, status, detail string
	}{
		{"plain ok", "Did the thing.\n\nSTATUS: ok", "ok", ""},
		{"blocked with detail", "Could not reach Drive.\nSTATUS: blocked: gdrive MCP tools are missing\n", "blocked", "gdrive MCP tools are missing"},
		{"markdown decoration", "report\n**Status:** BLOCKED - sign-in needed", "blocked", "sign-in needed"},
		{"code span", "report\n`STATUS: skipped: not Saturday`", "skipped", "not Saturday"},
		{"inside a fence", "```\nSTATUS: blocked: no input\n```", "blocked", "no input"},
		{"no status line", "All done, events created.", "", ""},
		{"status not last", "STATUS: blocked: earlier\nthen it recovered and finished", "", ""},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			status, detail := reportedStatus(tt.response)
			if status != tt.status || detail != tt.detail {
				t.Errorf("reportedStatus = (%q, %q), want (%q, %q)", status, detail, tt.status, tt.detail)
			}
		})
	}
}

func TestRoutineStatusError(t *testing.T) {
	t.Parallel()
	routine := &RunOptions{RoutineID: "global:planner"}
	if err := routineStatusError(routine, "blocked", "no tools"); err == nil || err.Error() != "routine fire failed: the agent reported blocked: no tools" {
		t.Errorf("blocked err = %v", err)
	}
	for _, status := range []string{"ok", "skipped", "failed", "error", ""} {
		if err := routineStatusError(routine, status, ""); err != nil {
			t.Errorf("status %q err = %v, want nil", status, err)
		}
	}
	if err := routineStatusError(&RunOptions{}, "blocked", "x"); err != nil {
		t.Errorf("non-routine run err = %v, want nil", err)
	}
}

// TestExecuteRunRoutineFireReportingBlockedFails drives a routine fire end to
// end: the agent is told to end with a status line, its blocked report fails
// the run, and the session keeps the final result and an error status.
func TestExecuteRunRoutineFireReportingBlockedFails(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	installArgvRecordingCLI(t, `{"type":"result","subtype":"success","is_error":false,"num_turns":2,"result":"The gdrive tools are not available.\nSTATUS: blocked: gdrive MCP tools missing","usage":{"input_tokens":1,"output_tokens":1}}`)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	opts := &RunOptions{
		Agent:           "smoke",
		AgentsDir:       writeTestAgent(t, "smoke"),
		Provider:        "claude-code",
		WorkingDir:      t.TempDir(),
		ConfigAvailable: true,
		RoutineID:       "global:planner",
	}
	err := ExecuteRun(cmd, []string{"hello"}, opts)
	if err == nil || err.Error() != "routine fire failed: the agent reported blocked: gdrive MCP tools missing" {
		t.Fatalf("ExecuteRun err = %v, want blocked failure", err)
	}
	dir := session.ResolveDir(opts.WorkingDir, opts.LastSessionID)
	events, readErr := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if readErr != nil {
		t.Fatalf("read events: %v", readErr)
	}
	var sawResult, sawRunEnd bool
	for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
		var ev struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad event %q: %v", line, err)
		}
		switch ev.Type {
		case session.EventResult:
			sawResult = true
			if ev.Payload["reported_status"] != "blocked" || !strings.Contains(ev.Payload["text"].(string), "gdrive tools are not available") {
				t.Errorf("result payload = %v", ev.Payload)
			}
		case session.EventRunEnd:
			sawRunEnd = true
			if ev.Payload["status"] != session.StatusError {
				t.Errorf("run_end status = %v, want error", ev.Payload["status"])
			}
		case session.EventRunStart:
		}
	}
	if !sawResult || !sawRunEnd {
		t.Errorf("events missing result (%v) or run_end (%v):\n%s", sawResult, sawRunEnd, events)
	}
}

// TestExecuteRunRoutineFirePromptsForStatus checks the status-line contract
// reaches a routine fire's system prompt and stays out of one-off runs.
func TestExecuteRunRoutineFirePromptsForStatus(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, routineID := range []string{"global:planner", ""} {
		dir := installArgvRecordingCLI(t, okResult)
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		opts := &RunOptions{
			Agent: "smoke", AgentsDir: writeTestAgent(t, "smoke"), Provider: "claude-code",
			WorkingDir: t.TempDir(), ConfigAvailable: true, RoutineID: routineID,
		}
		if err := ExecuteRun(cmd, []string{"hello"}, opts); err != nil {
			t.Fatalf("ExecuteRun(routine=%q) err = %v", routineID, err)
		}
		// The system prompt spans several argv lines in args.txt.
		argv := strings.Join(readArgs(t, dir), "\n")
		if got, want := strings.Contains(argv, "STATUS: blocked: <what stopped you>"), routineID != ""; got != want {
			t.Errorf("routine=%q: status contract in prompt = %v, want %v", routineID, got, want)
		}
	}
}
