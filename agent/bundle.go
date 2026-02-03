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
}

// Manifest represents the structure of an agent's manifest file.
type Manifest struct {
	Name       string                  `yaml:"name"`
	Version    string                  `yaml:"version"`
	EntryPoint string                  `yaml:"entrypoint"`
	Wrapper    string                  `yaml:"wrapper"`
	References []string                `yaml:"references"`
	Modes      map[string]ModeOverride `yaml:"modes,omitempty"`
}

// Bundle contains the assembled system, user, and combined prompt content for an agent run.
type Bundle struct {
	System   string // wrapper + system prompt + references
	User     string // user request only
	Combined []byte // concatenated for --print-bundle/--bundle-out
	WorkDir  string
}

// BuildBundle assembles the agent bundle from manifest, system prompt, wrapper, and references.
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

	if mode != "" {
		override, ok := manifest.Modes[mode]
		if !ok {
			return nil, fmt.Errorf("agent %q has no mode %q", agentName, mode)
		}
		if override.EntryPoint != "" {
			manifest.EntryPoint = override.EntryPoint
		}
		if override.Wrapper != "" {
			manifest.Wrapper = override.Wrapper
		}
		if len(override.References) > 0 {
			manifest.References = override.References
		}
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

	var refs []string
	for _, ref := range manifest.References {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		refPath := filepath.Join(agentPath, ref)
		refData, err := os.ReadFile(refPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read reference %s: %w", ref, err)
		}
		refs = append(refs, fmt.Sprintf("## Reference: %s\n\n%s\n", ref, strings.TrimSpace(string(refData))))
	}

	// Build the system message content (wrapper + system prompt + references).
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

	// Build the combined output for --print-bundle/--bundle-out.
	var combined bytes.Buffer
	combined.Write(sys.Bytes())
	combined.WriteString("\n\n## User Request\n\n")
	combined.WriteString(prompt)
	combined.WriteString("\n")

	return &Bundle{
		System:   sys.String(),
		User:     prompt,
		Combined: combined.Bytes(),
		WorkDir:  workingDir,
	}, nil
}
