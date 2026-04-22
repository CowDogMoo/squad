package tools

import "testing"

func TestIsSafeCommand(t *testing.T) {
	t.Parallel()
	safe := []string{
		"ls", "pwd", "git status", "git log --oneline", "go version",
		"cat foo.txt", "whoami", "echo hello", "git diff HEAD",
	}
	for _, cmd := range safe {
		if !IsSafeCommand(cmd) {
			t.Errorf("expected %q to be safe", cmd)
		}
	}
	notSafe := []string{
		"rm -rf /tmp/foo", "curl http://evil.com", "go run main.go",
		"make build", "docker run ubuntu",
	}
	for _, cmd := range notSafe {
		if IsSafeCommand(cmd) {
			t.Errorf("expected %q to NOT be safe", cmd)
		}
	}
}

func TestContainsReadCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd  string
		want bool
	}{
		// Simple read commands
		{"cat foo.rs", true},
		{"head -20 foo.rs", true},
		{"grep pattern file.rs", true},
		{"rg pattern file.rs", true},
		{"sed -n '1,10p' foo.rs", true},
		{"wc -l foo.rs", true},

		// cd && read — the main bypass pattern
		{"cd /path && cat file.rs", true},
		{"cd /path && grep foo bar.rs", true},
		{"cd /path && head -5 bar.rs", true},
		{"cd /path && wc -l *.rs", true},

		// Piped reads
		{"cat file.rs | head -5", true},
		{"grep foo bar.rs | wc -l", true},

		// Multi-segment with echo separators
		{"cat a.rs && echo '===' && cat b.rs", true},

		// Non-read commands should NOT match
		{"cargo test --lib", false},
		{"cd /path && cargo build", false},
		{"make lint", false},
		{"go test ./...", false},
		{"docker run ubuntu", false},
	}
	for _, tc := range cases {
		if got := ContainsReadCommand(tc.cmd); got != tc.want {
			t.Errorf("ContainsReadCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestIsBlockedCommand(t *testing.T) {
	t.Parallel()
	blocked := []string{
		"sudo rm -rf /", "rm -rf /", "nmap 192.168.1.0/24",
		"git push --force origin main", "pip install requests",
		"shutdown -h now", "dd if=/dev/zero of=/dev/sda",
	}
	for _, cmd := range blocked {
		if ok, _ := IsBlockedCommand(cmd); !ok {
			t.Errorf("expected %q to be blocked", cmd)
		}
	}
	allowed := []string{
		"ls -la", "git status", "go test ./...", "make lint",
		"echo hello", "git push origin feature-branch",
	}
	for _, cmd := range allowed {
		if ok, reason := IsBlockedCommand(cmd); ok {
			t.Errorf("expected %q to be allowed, but got: %s", cmd, reason)
		}
	}
}
