package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cowdogmoo/squad/executor"
	"github.com/cowdogmoo/squad/logging"
	"github.com/tmc/langchaingo/llms"
)

// SystemInfo holds structured information about the execution environment.
type SystemInfo struct {
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	Kernel       string   `json:"kernel"`
	Architecture string   `json:"architecture"`
	CPUs         string   `json:"cpus"`
	Memory       string   `json:"memory"`
	Disk         string   `json:"disk"`
	Interfaces   []string `json:"interfaces,omitempty"`
	Containers   []string `json:"containers,omitempty"`
	Users        string   `json:"current_user"`
}

func definitionSystemInfo() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name:        "SystemInfo",
			Description: "Collect structured information about the execution environment (hostname, OS, architecture, memory, disk, network interfaces, running containers). Use this to understand the system you are operating on.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func systemInfoTool(ex executor.Executor) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, _ []byte) (string, error) {
		logging.InfoContext(ctx, "collecting system information via %s executor", ex.Type())

		info := &SystemInfo{}

		info.Hostname = execTrim(ctx, ex, "hostname")
		info.OS = execTrim(ctx, ex, "cat /etc/os-release 2>/dev/null | head -5 || sw_vers 2>/dev/null || echo unknown")
		info.Kernel = execTrim(ctx, ex, "uname -sr 2>/dev/null || echo unknown")
		info.Architecture = execTrim(ctx, ex, "uname -m 2>/dev/null || echo unknown")
		info.CPUs = execTrim(ctx, ex, "nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo unknown")
		info.Memory = execTrim(ctx, ex, "free -h 2>/dev/null | head -2 || vm_stat 2>/dev/null | head -5 || echo unknown")
		info.Disk = execTrim(ctx, ex, "df -h / 2>/dev/null || echo unknown")
		info.Users = execTrim(ctx, ex, "whoami 2>/dev/null || echo unknown")

		ifOutput := execTrim(ctx, ex, "ip -br addr 2>/dev/null || ifconfig 2>/dev/null | grep -E '^[a-z]|inet ' || echo unknown")
		if ifOutput != "" && ifOutput != "unknown" {
			for line := range strings.SplitSeq(ifOutput, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					info.Interfaces = append(info.Interfaces, line)
				}
			}
		}

		containerOutput := execTrim(ctx, ex, "docker ps --format '{{.Names}} ({{.Image}}) {{.Status}}' 2>/dev/null || echo none")
		if containerOutput != "" && containerOutput != "none" {
			for line := range strings.SplitSeq(containerOutput, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					info.Containers = append(info.Containers, line)
				}
			}
		}

		result, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal system info: %w", err)
		}
		return string(result), nil
	}
}

// execTrim runs a command via the executor and returns trimmed output.
func execTrim(ctx context.Context, ex executor.Executor, command string) string {
	out, err := ex.Execute(ctx, command)
	if err != nil {
		logging.DebugContext(ctx, "sysinfo command failed (%s): %v", command[:min(len(command), 30)], err)
		return ""
	}
	return strings.TrimSpace(string(out))
}
