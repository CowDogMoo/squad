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
