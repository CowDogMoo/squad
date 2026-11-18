package telemetry

import (
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

// Fake credentials are assembled at runtime so no literal in this file looks
// like a live token to secret scanners.
var (
	fakeSecret = "hunter2-" + "not-real"
	fakeJWT    = "eyJ" + "hbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiIxMjM0NTY3ODkwIn0" + "." + "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
)

func attrMap(kvs []attribute.KeyValue) map[string]attribute.Value {
	m := make(map[string]attribute.Value, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value
	}
	return m
}

func TestCommandNames(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single command", "ls -la /tmp", "ls"},
		{"path is reduced to basename", "/opt/hydra/hydra -l admin -p " + fakeSecret + " ssh://10.0.0.5", "hydra"},
		{"pipeline and chains", "curl -s http://lab.test | jq .data && echo done; ./run.sh || true", "curl jq echo run.sh true"},
		{"background and newline", "sleep 5 &\nwc -l out.txt", "sleep wc"},
		{"quoted operators stay in the argument", `echo "a && b | c; d" && grep -c x file`, "echo grep"},
		{"single quotes", `bash -c 'mysql -p` + fakeSecret + ` -e "show databases"'`, "bash"},
		{"env assignments are skipped", "DB_PASSWORD=" + fakeSecret + ` API_KEY="` + fakeSecret + ` x" ./migrate up`, "migrate"},
		{"assignment only", "TOKEN=" + fakeSecret, ""},
		{"command substitution in assignment", "TOKEN=$(cat /run/secrets/" + fakeSecret + ") curl -H x http://lab.test", "curl"},
		{"command substitution as command", "$(which nxc) smb h -p " + fakeSecret, "$(...)"},
		{"backslash continuation", "curl -s \\\n  -H 'Authorization: Bearer " + fakeSecret + "' \\\n  http://lab.test", "curl"},
		{"redirections", "go test ./... 2>&1 > out.log && cat < out.log", "go cat"},
		{"subshell and group", "(cd /tmp && make) ; { echo hi; }", "cd make echo"},
		{"variable expansion in path", "${HOME}/tools/nxc smb h -p " + fakeSecret, "nxc"},
		{"heredoc body is skipped", "cat <<EOF > creds.txt\npassword " + fakeSecret + "\nuser admin\nEOF\nchmod 600 creds.txt", "cat chmod"},
		{"quoted heredoc delimiter with tabs", "cat <<-'END'\n\t" + fakeSecret + " is here\n\tEND\nls", "cat ls"},
		{"here-string", "base64 -d <<< " + fakeSecret, "base64"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommandNames(tt.in)
			if got != tt.want {
				t.Errorf("CommandNames() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, fakeSecret) {
				t.Error("CommandNames() returned argument text containing a credential")
			}
		})
	}
}

func TestCommandAttributesDefaultExportsNamesOnly(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "")
	commands := []string{
		"curl -H 'Authorization: Bearer " + fakeSecret + "' https://api.lab.test/v1",
		"curl -d '{\"password\":\"" + fakeSecret + "\"}' https://lab.test/login",
		"git clone https://bob:" + fakeSecret + "@git.lab.test/repo.git",
		"nxc smb 10.0.0.5 -u admin -p '" + fakeSecret + "'",
		"python3 decode.py " + fakeJWT,
	}
	for _, cmd := range commands {
		attrs := attrMap(CommandAttributes("squad.tool", cmd))
		got := attrs["squad.tool.command"].AsString()
		if strings.Contains(got, fakeSecret) || strings.Contains(got, fakeJWT) {
			t.Errorf("default squad.tool.command carries a credential for a %d-byte command", len(cmd))
		}
		if want := CommandNames(cmd); got != want {
			t.Errorf("squad.tool.command = %q, want %q", got, want)
		}
		if n := attrs["squad.tool.command_length"].AsInt64(); n != int64(len(cmd)) {
			t.Errorf("squad.tool.command_length = %d, want %d", n, len(cmd))
		}
	}
}

func TestCommandAttributesOptIn(t *testing.T) {
	for _, v := range []string{"1", "true", "YES", "on"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(ExportCommandsEnv, v)
			cmd := "curl -H 'Authorization: Bearer " + fakeSecret + "' 'https://lab.test/?page=2&token=" + fakeSecret + "'"
			attrs := attrMap(CommandAttributes("squad.executor", cmd))
			want := "curl -H 'Authorization: Bearer [REDACTED]' 'https://lab.test/?page=2&token=[REDACTED]'"
			if got := attrs["squad.executor.command"].AsString(); got != want {
				t.Errorf("squad.executor.command = %q, want %q", got, want)
			}
			if n := attrs["squad.executor.command_length"].AsInt64(); n != int64(len(cmd)) {
				t.Errorf("squad.executor.command_length = %d, want %d", n, len(cmd))
			}
		})
	}
}

func TestCommandAttributesOptInFalseValuesStayDefault(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "maybe"} {
		t.Setenv(ExportCommandsEnv, v)
		attrs := attrMap(CommandAttributes("squad.tool", "ls -la /secret-dir"))
		if got := attrs["squad.tool.command"].AsString(); got != "ls" {
			t.Errorf("%s=%q: squad.tool.command = %q, want %q", ExportCommandsEnv, v, got, "ls")
		}
	}
}

func TestCommandAttributesOptInTruncates(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "1")
	cmd := "echo password=" + fakeSecret + " " + strings.Repeat("x", 2000)
	got := attrMap(CommandAttributes("squad.tool", cmd))["squad.tool.command"].AsString()
	if strings.Contains(got, fakeSecret) {
		t.Fatal("opt-in squad.tool.command still carries the credential")
	}
	if len(got) != maxExportedCommandBytes+len("...") || !strings.HasPrefix(got, "echo password=[REDACTED] xxx") {
		t.Errorf("opt-in squad.tool.command = %d bytes starting %q", len(got), got[:30])
	}
}

func TestRedactCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bearer", "echo Bearer " + fakeSecret, "echo Bearer [REDACTED]"},
		{"jwt", "python3 decode.py " + fakeJWT, "python3 decode.py [REDACTED]"},
		{"url userinfo", "git clone https://bob:" + fakeSecret + "@git.lab.test/r.git", "git clone https://bob:[REDACTED]@git.lab.test/r.git"},
		{"key=value", "DB_PASSWORD=" + fakeSecret + " ./run --api-key=" + fakeSecret, "DB_PASSWORD=[REDACTED] ./run --api-key=[REDACTED]"},
		{"key: value", "printf 'secret: " + fakeSecret + "'", "printf 'secret: [REDACTED]'"},
		{"json member", `curl -d '{"user":"a","password":"` + fakeSecret + `"}'`, `curl -d '{"user":"a","password":"[REDACTED]"}'`},
		{"no secrets unchanged", `go test ./... -run 'TestFoo|TestBar' && grep -rn "func main" cmd/`, `go test ./... -run 'TestFoo|TestBar' && grep -rn "func main" cmd/`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RedactCommand(tt.in); got != tt.want {
				t.Errorf("RedactCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}
