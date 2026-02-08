// Package agent assembles agent prompt bundles and configuration metadata.
package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModeOverride represents an override for agent mode configuration in the manifest.
type ModeOverride struct {
	EntryPoint string   `yaml:"entrypoint,omitempty"`
	Wrapper    string   `yaml:"wrapper,omitempty"`
	References []string `yaml:"references,omitempty"`
	Task       string   `yaml:"task,omitempty"`
}

// Manifest represents the structure of an agent's manifest file.
type Manifest struct {
	Name       string                  `yaml:"name"`
	Version    string                  `yaml:"version"`
	EntryPoint string                  `yaml:"entrypoint"`
	Wrapper    string                  `yaml:"wrapper"`
	References []string                `yaml:"references"`
	Task       string                  `yaml:"task,omitempty"`
	Modes      map[string]ModeOverride `yaml:"modes,omitempty"`
}

// Bundle contains the assembled system, user, and combined prompt content for an agent run.
type Bundle struct {
	System   string // wrapper + system prompt + references + task
	User     string // user request (CLI prompt or default)
	Combined []byte // concatenated for --print-bundle/--bundle-out
	WorkDir  string
}

// applyModeOverride applies mode-specific overrides to the manifest.
func (m *Manifest) applyModeOverride(agentName, mode string) error {
	if mode == "" {
		return nil
	}
	override, ok := m.Modes[mode]
	if !ok {
		return fmt.Errorf("agent %q has no mode %q", agentName, mode)
	}
	if override.EntryPoint != "" {
		m.EntryPoint = override.EntryPoint
	}
	if override.Wrapper != "" {
		m.Wrapper = override.Wrapper
	}
	if len(override.References) > 0 {
		m.References = override.References
	}
	if override.Task != "" {
		m.Task = override.Task
	}
	return nil
}

// loadReferences reads all reference files and returns formatted content.
func loadReferences(agentPath string, refs []string) ([]string, error) {
	var result []string
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		refPath := filepath.Join(agentPath, ref)
		refData, err := os.ReadFile(refPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read reference %s: %w", ref, err)
		}
		result = append(result, fmt.Sprintf("## Reference: %s\n\n%s\n", ref, strings.TrimSpace(string(refData))))
	}
	return result, nil
}

// loadTask reads the task file if specified and returns its content.
func loadTask(agentPath, taskFile string) (string, error) {
	if taskFile == "" {
		return "", nil
	}
	taskPath := filepath.Join(agentPath, taskFile)
	taskData, err := os.ReadFile(taskPath)
	if err != nil {
		return "", fmt.Errorf("failed to read task %s: %w", taskFile, err)
	}
	return strings.TrimSpace(string(taskData)), nil
}

// BuildBundle assembles the agent bundle from manifest, system prompt, wrapper, references, and task.
// The task instructions are included in the system bundle. The CLI prompt becomes the user message.
// If no CLI prompt is provided, a default user message is used.
func BuildBundle(agentsDir, agentName, prompt, workingDir, mode string) (*Bundle, error) {
	agentPath := filepath.Join(agentsDir, agentName)
	manifestPath := filepath.Join(agentPath, "agent.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent manifest: %w", err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}

	if err := manifest.applyModeOverride(agentName, mode); err != nil {
		return nil, err
	}

	entryPath := filepath.Join(agentPath, manifest.EntryPoint)
	systemData, err := os.ReadFile(entryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read system prompt: %w", err)
	}

	wrapperPath := filepath.Join(agentPath, manifest.Wrapper)
	wrapperData, err := os.ReadFile(wrapperPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent wrapper: %w", err)
	}

	refs, err := loadReferences(agentPath, manifest.References)
	if err != nil {
		return nil, err
	}

	taskContent, err := loadTask(agentPath, manifest.Task)
	if err != nil {
		return nil, err
	}

	// Build the system message content (wrapper + system prompt + references + task).
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	sys.WriteString(fmt.Sprintf("Agent: %s (%s)\n", manifest.Name, manifest.Version))
	sys.WriteString(fmt.Sprintf("Working Directory: %s\n\n", workingDir))
	sys.WriteString("## Agent Wrapper\n\n")
	sys.Write(wrapperData)
	sys.WriteString("\n\n## System Prompt\n\n")
	sys.Write(systemData)

	if len(refs) > 0 {
		sys.WriteString("\n\n## References\n\n")
		for _, ref := range refs {
			sys.WriteString(ref)
			sys.WriteString("\n")
		}
	}

	if taskContent != "" {
		sys.WriteString("\n\n## Task\n\n")
		sys.WriteString(taskContent)
		sys.WriteString("\n")
	}

	// User message: CLI prompt if provided, otherwise a simple default.
	userMessage := prompt
	if userMessage == "" {
		userMessage = "Begin."
	}

	// Build the combined output for --print-bundle/--bundle-out.
	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Message\n\n")
	combined.WriteString(userMessage)
	combined.WriteString("\n")

	return &Bundle{
		System:   sys.String(),
		User:     userMessage,
		Combined: combined.Bytes(),
		WorkDir:  workingDir,
	}, nil
}
