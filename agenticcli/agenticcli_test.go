package agenticcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLookup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider   string
		wantBinary string
		wantOK     bool
	}{
		{provider: "claude-code", wantBinary: "claude", wantOK: true},
		{provider: "antigravity", wantBinary: "agy", wantOK: true},
		{provider: "agy", wantOK: false}, // legacy alias resolves in runner.normalizeProvider
		{provider: "anthropic", wantOK: false},
		{provider: "", wantOK: false},
	}
	for _, tt := range tests {
		spec, ok := Lookup(tt.provider)
		if ok != tt.wantOK {
			t.Errorf("Lookup(%q) ok = %v, want %v", tt.provider, ok, tt.wantOK)
			continue
		}
		if ok && spec.Binary != tt.wantBinary {
			t.Errorf("Lookup(%q).Binary = %q, want %q", tt.provider, spec.Binary, tt.wantBinary)
		}
	}
}

func TestBuildArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		req       Request
		wantArgs  []string
		wantStdin string
	}{
		{
			name: "claude minimal",
			req:  Request{Provider: "claude-code", UserPrompt: "do the thing"},
			wantArgs: []string{
				"--print", "--output-format", "json", "--dangerously-skip-permissions",
				"--setting-sources", "",
			},
			wantStdin: "do the thing",
		},
		{
			name: "claude full",
			req: Request{
				Provider: "claude-code", Model: "sonnet",
				SystemPrompt: "be brief", UserPrompt: "task", ReadOnly: true,
			},
			wantArgs: []string{
				"--print", "--output-format", "json", "--dangerously-skip-permissions",
				"--setting-sources", "",
				"--append-system-prompt", "be brief",
				"--model", "sonnet",
				"--disallowed-tools", readOnlyDisallowedTools,
			},
			wantStdin: "task",
		},
		{
			name: "antigravity minimal",
			req:  Request{Provider: "antigravity", UserPrompt: "task"},
			wantArgs: []string{
				"--print", "task", "--output-format", "json",
				"--print-timeout", antigravityPrintTimeout, "--dangerously-skip-permissions",
			},
		},
		{
			name: "antigravity system prompt and workdir rooting",
			req: Request{
				Provider: "antigravity", Model: "gemini-3.7-flash-low",
				SystemPrompt: "be brief", UserPrompt: "task", WorkDir: "/tmp/repo",
			},
			wantArgs: []string{
				"--print", "be brief\n\n" + workDirNote("/tmp/repo") + "\n\ntask",
				"--output-format", "json",
				"--print-timeout", antigravityPrintTimeout, "--dangerously-skip-permissions",
				"--add-dir", "/tmp/repo",
				"--model", "gemini-3.7-flash-low",
			},
		},
		{
			name: "unknown provider",
			req:  Request{Provider: "nope"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args, stdin := buildArgs(tt.req)
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %q, want %q", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Fatalf("args[%d] = %q, want %q (full: %q)", i, args[i], tt.wantArgs[i], args)
				}
			}
			if stdin != tt.wantStdin {
				t.Errorf("stdin = %q, want %q", stdin, tt.wantStdin)
			}
		})
	}
}

func TestParseOutput(t *testing.T) {
	t.Parallel()
	claudeSuccess := `{"is_error":false,"num_turns":3,"result":"done","usage":{"input_tokens":6,"cache_creation_input_tokens":100,"cache_read_input_tokens":200,"output_tokens":9},"type":"result"}`
	antigravitySuccess := `{"conversation_id":"abc","status":"SUCCESS","response":"done\n","num_turns":2,"usage":{"input_tokens":500,"output_tokens":40,"total_tokens":540}}`

	tests := []struct {
		name       string
		provider   string
		stdout     string
		wantErr    string
		wantResp   string
		wantInput  int64
		wantOutput int64
		wantTurns  int
	}{
		{
			name: "claude success", provider: "claude-code", stdout: claudeSuccess,
			wantResp: "done", wantInput: 306, wantOutput: 9, wantTurns: 3,
		},
		{
			name: "claude is_error", provider: "claude-code",
			stdout:  `{"is_error":true,"result":"credit exhausted","num_turns":1,"usage":{}}`,
			wantErr: "credit exhausted",
		},
		{
			name: "claude non-JSON falls back to raw", provider: "claude-code",
			stdout: "plain text answer", wantResp: "plain text answer",
		},
		{
			name: "antigravity success", provider: "antigravity", stdout: antigravitySuccess,
			wantResp: "done\n", wantInput: 500, wantOutput: 40, wantTurns: 2,
		},
		{
			name: "antigravity failure status", provider: "antigravity",
			stdout:  `{"status":"ERROR","response":"boom","num_turns":1,"usage":{}}`,
			wantErr: "status=ERROR",
		},
		{
			name: "antigravity failure detail in error field", provider: "antigravity",
			stdout:  `{"status":"ERROR","response":"","error":"invalid model selection (--model \"gemini-3.1-pro\")","num_turns":0,"usage":{}}`,
			wantErr: "invalid model selection",
		},
		{
			name: "antigravity non-JSON falls back to raw", provider: "antigravity",
			stdout: "plain", wantResp: "plain",
		},
		{
			name: "unknown provider", provider: "nope", stdout: "{}",
			wantErr: "unknown agentic CLI provider",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res, err := parseOutput(tt.provider, []byte(tt.stdout))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Response != tt.wantResp {
				t.Errorf("Response = %q, want %q", res.Response, tt.wantResp)
			}
			if res.InputTokens != tt.wantInput || res.OutputTokens != tt.wantOutput {
				t.Errorf("tokens = %d/%d, want %d/%d", res.InputTokens, res.OutputTokens, tt.wantInput, tt.wantOutput)
			}
			if res.Turns != tt.wantTurns {
				t.Errorf("Turns = %d, want %d", res.Turns, tt.wantTurns)
			}
		})
	}
}

// writeFakeCLI installs an executable shell script named binary into dir that
// records its argv and stdin next to itself and prints canned stdout.
func writeFakeCLI(t *testing.T, dir, binary, stdout string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		`printf '%s\n' "$@" > "` + filepath.Join(dir, "args.txt") + `"` + "\n" +
		`cat > "` + filepath.Join(dir, "stdin.txt") + `"` + "\n" +
		`printf '%s' '` + stdout + `'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, binary), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRunSpawnsBinary(t *testing.T) {
	dir := t.TempDir()
	writeFakeCLI(t, dir, "claude",
		`{"is_error":false,"num_turns":2,"result":"ran","usage":{"input_tokens":10,"output_tokens":5}}`)
	// Prepend so the fake wins over any real install while /bin/cat stays
	// reachable for the script's stdin capture.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	res, err := Run(context.Background(), Request{
		Provider:     "claude-code",
		SystemPrompt: "sys",
		UserPrompt:   "user task",
		WorkDir:      dir,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Response != "ran" || res.Turns != 2 || res.InputTokens != 10 || res.OutputTokens != 5 {
		t.Errorf("unexpected result: %+v", res)
	}

	args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--print", "--append-system-prompt", "sys", "--dangerously-skip-permissions"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "user task" {
		t.Errorf("stdin = %q, want %q", stdin, "user task")
	}
}

func TestRunMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Run(context.Background(), Request{Provider: "antigravity", UserPrompt: "x", WorkDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "requires the \"agy\" CLI on PATH") {
		t.Fatalf("err = %v, want missing-binary error", err)
	}
}

// TestRunReadOnlyRejectedForAntigravity pins the fail-loud behavior: the agy
// CLI has no flag that actually blocks edits in print mode, so a readonly
// request must error before the binary is even looked up.
func TestRunReadOnlyRejectedForAntigravity(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no agy on PATH: the rejection must come first
	_, err := Run(context.Background(), Request{Provider: "antigravity", UserPrompt: "x", ReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), "cannot enforce read-only mode") {
		t.Fatalf("err = %v, want read-only rejection", err)
	}
}

func TestNormalizeAntigravityModel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gemini-3.1-pro":           "gemini-3.1-pro-low",
		"gemini-3.1-pro-preview":   "gemini-3.1-pro-low",
		"gemini-3-flash-latest":    "gemini-3-flash-low",
		"gemini-3.7-flash-high":    "gemini-3.7-flash-high",
		"gemini-3.7-flash-medium":  "gemini-3.7-flash-medium",
		"claude-opus-4-6-thinking": "claude-opus-4-6-thinking",
		"gpt-oss-120b-medium":      "gpt-oss-120b-medium",
	}
	for in, want := range cases {
		if got := NormalizeAntigravityModel(in); got != want {
			t.Errorf("NormalizeAntigravityModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunUnknownProvider(t *testing.T) {
	t.Parallel()
	_, err := Run(context.Background(), Request{Provider: "nope", UserPrompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown agentic CLI provider") {
		t.Fatalf("err = %v, want unknown-provider error", err)
	}
}

func TestOutputTailTruncatesLongStreams(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("e", 3000)
	got := outputTail([]byte(long), []byte("out"))
	if !strings.Contains(got, "stderr: …") {
		t.Errorf("long stderr should be tail-truncated with ellipsis: %.80s", got)
	}
	if !strings.Contains(got, "stdout: out") {
		t.Errorf("stdout tail missing: %.80s", got)
	}
	if len(got) > 4200 {
		t.Errorf("tail output too long: %d bytes", len(got))
	}
	if outputTail(nil, nil) != "" {
		t.Error("empty streams should produce empty tail")
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("truncate long = %q, want abc…", got)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'auth expired' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	_, err := Run(context.Background(), Request{Provider: "claude-code", UserPrompt: "x", WorkDir: dir})
	if err == nil || !strings.Contains(err.Error(), "auth expired") {
		t.Fatalf("err = %v, want stderr tail included", err)
	}
}

func TestSettingSourcesArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{name: "unset is hermetic", env: "", want: []string{"--setting-sources", ""}},
		{name: "whitespace is hermetic", env: "  ", want: []string{"--setting-sources", ""}},
		{name: "inherit omits the flag", env: "inherit", want: nil},
		{name: "explicit list passes through", env: "user,project", want: []string{"--setting-sources", "user,project"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := settingSourcesArgs(tt.env)
			if len(got) != len(tt.want) {
				t.Fatalf("settingSourcesArgs(%q) = %q, want %q", tt.env, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("settingSourcesArgs(%q)[%d] = %q, want %q", tt.env, i, got[i], tt.want[i])
				}
			}
		})
	}
}
