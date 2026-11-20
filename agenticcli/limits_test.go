package agenticcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClaudeCommonArgsLimits(t *testing.T) {
	t.Setenv(SettingSourcesEnv, "")
	tests := []struct {
		name string
		req  Request
		want []string
	}{
		{
			name: "transform: no tools, caps, no MCP",
			req:  Request{Provider: "claude-code", LimitTools: true, Tools: []string{}, MaxTurns: 100, MaxBudgetUSD: 5, ReadOnly: true},
			want: []string{
				"--setting-sources", "", "--settings", claudeAttributionOff,
				"--tools", "", "--max-turns", "100", "--max-budget-usd", "5",
				"--strict-mcp-config", "--disallowed-tools", readOnlyDisallowedTools,
			},
		},
		{
			name: "agent: manifest tools, caps, MCP config",
			req: Request{
				Provider: "claude-code", LimitTools: true, Tools: []string{"Bash", "Read"},
				MaxTurns: 60, MaxBudgetUSD: 2.5, MCPConfigFile: "/tmp/squad-claude-mcp-1.json",
			},
			want: []string{
				"--setting-sources", "", "--settings", claudeAttributionOff,
				"--tools", "Bash,Read", "--max-turns", "60", "--max-budget-usd", "2.5",
				"--strict-mcp-config", "--mcp-config", "/tmp/squad-claude-mcp-1.json",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := claudeCommonArgs(tt.req); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("claudeCommonArgs = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckMCPServers(t *testing.T) {
	t.Parallel()
	reported := []mcpServerStatus{
		{Name: "ok", Status: "connected"},
		{Name: "slow", Status: "pending"},
		{Name: "broken", Status: "failed"},
		{Name: "auth", Status: "needs-auth"},
	}
	if err := checkMCPServers(nil, reported); err != nil {
		t.Errorf("no required servers: %v", err)
	}
	if err := checkMCPServers([]string{"ok", "slow"}, reported); err != nil {
		t.Errorf("connected and pending: %v", err)
	}
	err := checkMCPServers([]string{"ok", "broken", "auth", "absent"}, reported)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"broken (failed)", "auth (needs-auth)", "absent (not started)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "ok (") {
		t.Errorf("err = %v names a connected server", err)
	}
}

// TestRunFailedMCPServerKillsCLI checks the single-shot path aborts a run at
// its init event when a required MCP server failed, rather than letting the
// CLI carry on to a paid result.
func TestRunFailedMCPServerKillsCLI(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\ncat > /dev/null\n" +
		`printf '%s\n' '{"type":"system","subtype":"init","session_id":"s","mcp_servers":[{"name":"gdrive","status":"failed"}]}'` + "\n" +
		"exec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	_, err := Run(context.Background(), Request{
		Provider: "claude-code", UserPrompt: "task", WorkDir: t.TempDir(),
		RequiredMCPServers: []string{"gdrive"},
	})
	if err == nil || !strings.Contains(err.Error(), "gdrive (failed)") {
		t.Fatalf("err = %v, want the failed MCP server named", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("run took %s; the CLI was not killed at init", elapsed)
	}
}

func TestClaudeRunError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		out     claudeOutput
		want    string
		wantErr error
	}{
		{"turn cap", claudeOutput{IsError: true, Subtype: "error_max_turns", Errors: []string{"Reached maximum number of turns (60)"}},
			"claude run failed: claude turn cap reached: Reached maximum number of turns (60)", ErrMaxTurns},
		{"spend cap", claudeOutput{IsError: true, Subtype: "error_max_budget_usd", Errors: []string{"Reached maximum budget ($2)"}},
			"claude run failed: claude spend cap reached: Reached maximum budget ($2)", ErrMaxBudget},
		{"other subtype", claudeOutput{IsError: true, Subtype: "error_during_execution", Errors: []string{"boom"}},
			"claude run failed (error_during_execution): boom", nil},
		{"result text", claudeOutput{IsError: true, Subtype: "success", Result: "API Error: overloaded"},
			"claude run failed: API Error: overloaded", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := claudeResult(tt.out)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want errors.Is %v", err, tt.wantErr)
			}
		})
	}
}
