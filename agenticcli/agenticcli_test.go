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
		{provider: "agy", wantBinary: "agy", wantOK: true},
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
				"--append-system-prompt", "be brief",
				"--model", "sonnet",
				"--disallowed-tools", readOnlyDisallowedTools,
			},
			wantStdin: "task",
		},
		{
			name: "agy minimal",
			req:  Request{Provider: "agy", UserPrompt: "task"},
			wantArgs: []string{
				"--print", "task", "--output-format", "json",
				"--print-timeout", agyPrintTimeout, "--dangerously-skip-permissions",
			},
		},
		{
			name: "agy system prompt prepended and readonly",
			req: Request{
				Provider: "agy", Model: "gpt5",
				SystemPrompt: "be brief", UserPrompt: "task", ReadOnly: true,
			},
			wantArgs: []string{
				"--print", "be brief\n\ntask", "--output-format", "json",
				"--print-timeout", agyPrintTimeout, "--dangerously-skip-permissions",
				"--model", "gpt5",
				"--mode", "plan",
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
	agySuccess := `{"conversation_id":"abc","status":"SUCCESS","response":"done\n","num_turns":2,"usage":{"input_tokens":500,"output_tokens":40,"total_tokens":540}}`

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
			name: "agy success", provider: "agy", stdout: agySuccess,
			wantResp: "done\n", wantInput: 500, wantOutput: 40, wantTurns: 2,
		},
		{
			name: "agy failure status", provider: "agy",
			stdout:  `{"status":"ERROR","response":"boom","num_turns":1,"usage":{}}`,
			wantErr: "status=ERROR",
		},
		{
			name: "agy non-JSON falls back to raw", provider: "agy",
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
	_, err := Run(context.Background(), Request{Provider: "agy", UserPrompt: "x", WorkDir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "requires the \"agy\" CLI on PATH") {
		t.Fatalf("err = %v, want missing-binary error", err)
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
