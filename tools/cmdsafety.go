package tools

import (
	"fmt"
	"strings"
)

// safeCommands are read-only commands that do not require extra scrutiny.
// They are allowed to run without warnings in any context.
var safeCommands = []string{
	"cat ", "cat\t",
	"echo ",
	"env",
	"head ",
	"id",
	"ls ",
	"ls\n", "ls\t",
	"pwd",
	"tail ",
	"wc ",
	"whoami",
	"which ",
	"file ",
	"stat ",
	"df ",
	"du ",
	"date",
	"uname",
	"hostname",
	"printenv",
	// git read-only
	"git status",
	"git log",
	"git diff",
	"git show",
	"git blame",
	"git branch",
	"git tag",
	"git remote",
	"git rev-parse",
	"git ls-files",
	"git ls-tree",
	"git describe",
	"git stash list",
	"git config --get",
	"git config --list",
	// go read-only
	"go version",
	"go env",
	"go list",
	"go doc",
	"go vet",
	// python read-only
	"python --version",
	"python3 --version",
	"pip list",
	"pip3 list",
	"pip show",
	"pip3 show",
	// node read-only
	"node --version",
	"npm list",
	"npm ls",
	"npm --version",
}

// blockedCommands are dangerous commands that should never be executed by an
// agent. The bash tool will refuse to run any command matching these prefixes.
var blockedCommands = []string{
	// destructive system commands
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	":(){",
	// privilege escalation
	"sudo ",
	"su ",
	"doas ",
	// network reconnaissance / exfiltration
	"nmap ",
	"masscan ",
	"zmap ",
	"curl -X POST",
	"wget --post",
	// system modification
	"shutdown",
	"reboot",
	"halt",
	"init 0",
	"init 6",
	"systemctl stop",
	"systemctl disable",
	"launchctl unload",
	// credential access
	"passwd",
	"chpasswd",
	// dangerous git operations
	"git push --force",
	"git push -f ",
	"git reset --hard",
	"git clean -fd",
	"git clean -fx",
	// package manager installs (prevent supply chain)
	"pip install ",
	"pip3 install ",
	"npm install -g",
	"npm i -g",
	"gem install ",
	"cargo install ",
	"go install ",
	"apt install",
	"apt-get install",
	"yum install",
	"dnf install",
	"brew install",
	"pacman -S",
}

// readLikeBinaries are command names that read file content. Used by
// ContainsReadCommand to detect read-like operations inside compound
// shell expressions (pipes, &&-chains, cd && cat, etc.).
var readLikeBinaries = []string{
	"cat", "head", "tail", "less", "more", "bat",
	"grep", "rg", "ag", "ack",
	"sed", "awk",
	"find", "fd",
	"strings", "xxd", "hexdump", "od",
	"wc",
}

// IsSafeCommand reports whether the command is known to be read-only.
func IsSafeCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range safeCommands {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	// Bare "ls" with no arguments.
	if trimmed == "ls" || trimmed == "env" || trimmed == "pwd" ||
		trimmed == "whoami" || trimmed == "date" || trimmed == "id" ||
		trimmed == "uname" || trimmed == "hostname" || trimmed == "printenv" {
		return true
	}
	return false
}

// ContainsReadCommand reports whether a compound shell command contains any
// read-like operations. It splits on shell operators (&&, ||, ;, |) and
// checks each segment for known read binaries. This catches bypass patterns
// like "cd /path && cat file" that IsSafeCommand misses because the overall
// command doesn't start with "cat ".
func ContainsReadCommand(cmd string) bool {
	// Normalise separators so we can split once.
	norm := strings.NewReplacer("&&", ";", "||", ";", "|", ";").Replace(cmd)
	for _, segment := range strings.Split(norm, ";") {
		seg := strings.TrimSpace(segment)
		// Strip a leading cd … that changes directory before the real command.
		if strings.HasPrefix(seg, "cd ") {
			continue // cd itself isn't a read — the next segment is
		}
		// Check if the segment starts with a known read binary.
		for _, bin := range readLikeBinaries {
			if seg == bin || strings.HasPrefix(seg, bin+" ") || strings.HasPrefix(seg, bin+"\t") {
				return true
			}
		}
		// Also check the existing safeCommands list (covers "stat ", "file ", etc.).
		for _, prefix := range safeCommands {
			if strings.HasPrefix(seg, prefix) {
				return true
			}
		}
	}
	return false
}

// IsBlockedCommand returns true and a reason if the command is dangerous
// and should not be executed by an agent.
func IsBlockedCommand(cmd string) (bool, string) {
	trimmed := strings.TrimSpace(cmd)
	for _, prefix := range blockedCommands {
		if strings.HasPrefix(trimmed, prefix) {
			return true, fmt.Sprintf("%v: %s commands are not allowed", ErrBlockedCommand, prefix)
		}
	}
	return false, ""
}
