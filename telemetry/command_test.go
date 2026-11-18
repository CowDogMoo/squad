package telemetry

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestCommandName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single command", "ls -la /tmp", "ls"},
		{"path is reduced to basename", "/opt/hydra/hydra -l admin -p " + fakeSecret + " ssh://10.0.0.5", "hydra"},
		{"relative path", "./run.sh --token " + fakeSecret, "run.sh"},
		{"only the first word is used", "curl -s http://lab.test | jq .data && echo " + fakeSecret, "curl"},
		{"leading whitespace", "\n\t  go test ./...", "go"},
		{"redirect after the name", "curl -s lab.test &>/dev/null", "curl"},
		{"env assignment", "DB_PASSWORD=" + fakeSecret + " ./migrate up", ComplexCommand},
		{"assignment value with slashes", "AWS_SECRET_ACCESS_KEY=wJal/K7MDENG/" + fakeSecret + " aws s3 ls", ComplexCommand},
		{"variable expansion in path", "${HOME}/tools/nxc smb h -p " + fakeSecret, ComplexCommand},
		{"command substitution", "$(which nxc) smb h -p " + fakeSecret, ComplexCommand},
		{"quoted name", "'curl' -H 'Authorization: Bearer " + fakeSecret + "'", ComplexCommand},
		{"subshell", "(cd /tmp && echo " + fakeSecret + ")", ComplexCommand},
		{"trailing slash", "tools/ " + fakeSecret, ComplexCommand},
		{"empty", "", ComplexCommand},
		{"whitespace only", " \n\t", ComplexCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CommandName(tt.in)
			if got != tt.want {
				t.Errorf("CommandName() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, fakeSecret) {
				t.Error("CommandName() returned argument text containing a credential")
			}
		})
	}
}

func TestCommandAttributesDefaultExportsNameOnly(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "")
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{"bearer header", "curl -H 'Authorization: Bearer " + fakeSecret + "' https://api.lab.test/v1", "curl"},
		{"json body", "curl -d '{\"password\":\"" + fakeSecret + "\"}' https://lab.test/login", "curl"},
		{"url userinfo", "git clone https://bob:" + fakeSecret + "@git.lab.test/repo.git", "git"},
		{"password flag", "nxc smb 10.0.0.5 -u admin -p '" + fakeSecret + "'", "nxc"},
		{"jwt argument", "python3 decode.py " + fakeJWT, "python3"},
		{"bash array", "CREDS=(admin:Winter2024! root:" + fakeSecret + "); for c in \"${CREDS[@]}\"; do sshpass" + " -p \"${c#*:}\" ssh root@10.0.0.5 id; done", ComplexCommand},
		{"quoted bash array", `passwords=("Winter2024!" "` + fakeSecret + `"); for p in "${passwords[@]}"; do echo "$p"; done`, ComplexCommand},
		{"apostrophe in comment", "# Let's try the creds we found\nsshpass" + " -p 'ab;" + fakeSecret + "' ssh admin@10.0.0.5 id", ComplexCommand},
		{"nested quotes in substitution", `OUT="$(curl -H "Authorization:Bearer ` + fakeSecret + `" lab.test)"; echo "$OUT"`, ComplexCommand},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := attrMap(CommandAttributes("squad.tool", tt.cmd))
			got := attrs["squad.tool.command"].AsString()
			if strings.Contains(got, fakeSecret) || strings.Contains(got, fakeJWT) {
				t.Errorf("default squad.tool.command carries a credential for a %d-byte command", len(tt.cmd))
			}
			if got != tt.want {
				t.Errorf("squad.tool.command = %q, want %q", got, tt.want)
			}
			if n := attrs["squad.tool.command_length"].AsInt64(); n != int64(len(tt.cmd)) {
				t.Errorf("squad.tool.command_length = %d, want %d", n, len(tt.cmd))
			}
		})
	}
}

func TestCommandAttributesOptIn(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "1")
	cmd := "curl -H 'Authorization: Bearer " + fakeSecret + "' 'https://lab.test/?page=2&token=" + fakeSecret + "'"
	attrs := attrMap(CommandAttributes("squad.executor", cmd))
	want := "curl -H 'Authorization: Bearer [REDACTED]' 'https://lab.test/?page=2&token=[REDACTED]'"
	if got := attrs["squad.executor.command"].AsString(); got != want {
		t.Errorf("squad.executor.command = %q, want %q", got, want)
	}
	if n := attrs["squad.executor.command_length"].AsInt64(); n != int64(len(cmd)) {
		t.Errorf("squad.executor.command_length = %d, want %d", n, len(cmd))
	}
}

func TestCommandAttributesOtherValuesStayDefault(t *testing.T) {
	for _, v := range []string{"0", "true", "yes", "on", " 1", "maybe"} {
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

func TestCommandAttributesOptInTruncatesOnRuneBoundary(t *testing.T) {
	t.Setenv(ExportCommandsEnv, "1")
	head := strings.Repeat("x", maxExportedCommandBytes-1)
	cmd := head + "— ✓ done"
	got := attrMap(CommandAttributes("squad.tool", cmd))["squad.tool.command"].AsString()
	if !utf8.ValidString(got) {
		t.Fatalf("opt-in squad.tool.command is not valid UTF-8: ends %q", got[len(got)-8:])
	}
	if got != head+"..." {
		t.Errorf("opt-in squad.tool.command = %d bytes ending %q, want %d bytes ending %q", len(got), got[len(got)-8:], len(head)+3, "xxxxx...")
	}
}

func TestAgentCommand(t *testing.T) {
	wrapped := "export SQUAD_SKILL_DIR='/skills/alpha'; curl -s lab.test"
	if got := AgentCommand(context.Background(), wrapped); got != wrapped {
		t.Errorf("AgentCommand() without a recorded command = %q, want %q", got, wrapped)
	}
	ctx := WithAgentCommand(context.Background(), "curl -s lab.test")
	if got := AgentCommand(ctx, wrapped); got != "curl -s lab.test" {
		t.Errorf("AgentCommand() = %q, want %q", got, "curl -s lab.test")
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
