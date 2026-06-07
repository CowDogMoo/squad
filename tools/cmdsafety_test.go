package tools

import (
	"testing"
)

func TestIsSafeCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"bare ls", "ls", true},
		{"ls with args", "ls -la", true},
		{"cat file", "cat file.go", true},
		{"echo", "echo hello", true},
		{"env bare", "env", true},
		{"pwd", "pwd", true},
		{"whoami", "whoami", true},
		{"date", "date", true},
		{"id", "id", true},
		{"uname", "uname", true},
		{"hostname", "hostname", true},
		{"printenv", "printenv", true},
		{"git status", "git status", true},
		{"git log", "git log --oneline", true},
		{"git diff", "git diff HEAD", true},
		{"go version", "go version", true},
		{"go env", "go env GOPATH", true},
		{"python version", "python --version", true},
		{"pip list", "pip list", true},
		{"node version", "node --version", true},
		{"npm list", "npm list", true},
		{"head file", "head -20 file.go", true},
		{"tail file", "tail -f log.txt", true},
		{"wc lines", "wc -l file.go", true},
		{"which cmd", "which go", true},
		{"stat file", "stat file.go", true},
		{"df disk", "df -h", true},
		{"du size", "du -sh .", true},
		{"rm command", "rm -rf /tmp/foo", false},
		{"sudo cmd", "sudo ls", false},
		{"curl post", "curl -X POST http://example.com", false},
		{"git push force", "git push --force", false},
		{"pip install", "pip install requests", false},
		{"empty string", "", false},
		{"random cmd", "myapp --run", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSafeCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("IsSafeCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsBlockedCommand(t *testing.T) {
	tests := []struct {
		name      string
		cmd       string
		wantBlock bool
	}{
		{"rm -rf /", "rm -rf /", true},
		{"rm -rf /*", "rm -rf /*", true},
		{"sudo ls", "sudo ls", true},
		{"su root", "su root", true},
		{"shutdown", "shutdown -h now", true},
		{"reboot", "reboot", true},
		{"git push force", "git push --force origin main", true},
		{"git reset hard", "git reset --hard HEAD", true},
		{"pip install", "pip install requests", true},
		{"npm install -g", "npm install -g typescript", true},
		{"go install", "go install github.com/foo/bar@latest", true},
		{"brew install", "brew install jq", true},
		{"apt install", "apt install curl", true},
		{"curl post", "curl -X POST http://example.com", true},
		{"nmap scan", "nmap -sV 192.168.1.0/24", true},
		{"safe ls", "ls -la", false},
		{"safe cat", "cat file.go", false},
		{"safe git log", "git log --oneline", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked, reason := IsBlockedCommand(tt.cmd)
			if blocked != tt.wantBlock {
				t.Errorf("IsBlockedCommand(%q) blocked=%v, want %v", tt.cmd, blocked, tt.wantBlock)
			}
			if tt.wantBlock && reason == "" {
				t.Errorf("IsBlockedCommand(%q) returned empty reason for blocked command", tt.cmd)
			}
		})
	}
}

func TestContainsReadCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"simple cat", "cat file.go", true},
		{"cat after cd", "cd /tmp && cat file.go", true},
		{"grep in pipe", "ls | grep foo", true},
		{"head in chain", "echo hi && head -5 file.go", true},
		{"tail pipe", "tail -f log.txt | grep error", true},
		{"sed command", "sed 's/foo/bar/' file.go", true},
		{"awk command", "awk '{print $1}' file.go", true},
		{"find command", "find . -name '*.go'", true},
		{"wc in chain", "cat file.go | wc -l", true},
		{"stat file", "stat file.go", true},
		{"mkdir only", "mkdir -p /tmp/foo", false},
		{"cd only", "cd /tmp", false},
		{"empty", "", false},
		{"git commit", "git commit -m 'fix'", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsReadCommand(tt.cmd)
			if got != tt.want {
				t.Errorf("ContainsReadCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
