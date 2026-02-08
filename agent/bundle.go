// Package agent assembles agent prompt bundles and configuration metadata.
package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Manifest represents the structure of an agent's manifest file.
type Manifest struct {
	Name       string   `yaml:"name"`
	Version    string   `yaml:"version"`
	EntryPoint string   `yaml:"entrypoint"`
	Wrapper    string   `yaml:"wrapper"`
	References []string `yaml:"references"`
	Task       string   `yaml:"task,omitempty"`
}

// Bundle contains the assembled system, user, and combined prompt content for an agent run.
type Bundle struct {
	System   string // wrapper + system prompt + references + task
	User     string // user request (CLI prompt or default)
	Combined []byte // concatenated for --print-bundle/--bundle-out
	WorkDir  string
}

// TemplateData holds the data passed to prompt templates.
type TemplateData struct {
	Mode string
}

// makeIncludeFunc creates an include function that reads from the _templates directory.
// Templates can use {{include "hard-rules/universal.md"}} to include shared content.
func makeIncludeFunc(agentsDir string) func(string) (string, error) {
	return func(path string) (string, error) {
		// Validate path doesn't escape _templates directory
		if strings.Contains(path, "..") {
			return "", fmt.Errorf("include path cannot contain '..': %s", path)
		}

		templatePath := filepath.Join(agentsDir, "_templates", path)
		content, err := os.ReadFile(templatePath)
		if err != nil {
			return "", fmt.Errorf("failed to include template %s: %w", path, err)
		}
		return strings.TrimSpace(string(content)), nil
	}
}

// processTemplate executes a Go text/template with the given mode.
// Templates can use {{if eq .Mode "edit"}}...{{end}} conditionals.
// Templates can use {{include "hard-rules/universal.md"}} to include shared content.
func processTemplate(name, content, mode, agentsDir string) (string, error) {
	if mode == "" {
		mode = "edit"
	}

	funcMap := template.FuncMap{
		"include": makeIncludeFunc(agentsDir),
	}

	tmpl, err := template.New(name).Funcs(funcMap).Parse(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, TemplateData{Mode: mode}); err != nil {
		return "", fmt.Errorf("failed to execute template %s: %w", name, err)
	}

	return buf.String(), nil
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

// loadManifest reads and parses the agent manifest.
func loadManifest(agentPath string) (*Manifest, error) {
	manifestPath := filepath.Join(agentPath, "agent.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent manifest: %w", err)
	}
	return &manifest, nil
}

// loadAndProcessPrompts loads system, wrapper, and task files, then processes them as templates.
func loadAndProcessPrompts(agentPath, agentsDir string, manifest *Manifest, mode string) (system, wrapper, task string, err error) {
	systemData, err := os.ReadFile(filepath.Join(agentPath, manifest.EntryPoint))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read system prompt: %w", err)
	}
	wrapperData, err := os.ReadFile(filepath.Join(agentPath, manifest.Wrapper))
	if err != nil {
		return "", "", "", fmt.Errorf("failed to read agent wrapper: %w", err)
	}
	taskContent, err := loadTask(agentPath, manifest.Task)
	if err != nil {
		return "", "", "", err
	}

	system, err = processTemplate("system", string(systemData), mode, agentsDir)
	if err != nil {
		return "", "", "", err
	}
	wrapper, err = processTemplate("wrapper", string(wrapperData), mode, agentsDir)
	if err != nil {
		return "", "", "", err
	}
	if taskContent != "" {
		task, err = processTemplate("task", taskContent, mode, agentsDir)
		if err != nil {
			return "", "", "", err
		}
	}
	return system, wrapper, task, nil
}

// BuildBundle assembles the agent bundle from manifest, system prompt, wrapper, references, and task.
// The task instructions are included in the system bundle. The CLI prompt becomes the user message.
// If no CLI prompt is provided, a default user message is used.
func BuildBundle(agentsDir, agentName, prompt, workingDir, mode string) (*Bundle, error) {
	agentPath := filepath.Join(agentsDir, agentName)

	manifest, err := loadManifest(agentPath)
	if err != nil {
		return nil, err
	}

	displayMode := mode
	if displayMode == "" {
		displayMode = "edit"
	}

	systemContent, wrapperContent, taskContent, err := loadAndProcessPrompts(agentPath, agentsDir, manifest, mode)
	if err != nil {
		return nil, err
	}

	refs, err := loadReferences(agentPath, manifest.References)
	if err != nil {
		return nil, err
	}

	// Build the system message content (wrapper + system prompt + references + task).
	var sys bytes.Buffer
	sys.WriteString("# Squad Agent Bundle\n\n")
	sys.WriteString(fmt.Sprintf("Agent: %s (%s)\n", manifest.Name, manifest.Version))
	sys.WriteString(fmt.Sprintf("Mode: %s\n", displayMode))
	sys.WriteString(fmt.Sprintf("Working Directory: %s\n\n", workingDir))
	sys.WriteString("## Agent Wrapper\n\n")
	sys.WriteString(wrapperContent)
	sys.WriteString("\n\n## System Prompt\n\n")
	sys.WriteString(systemContent)

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

	userMessage := prompt
	if userMessage == "" {
		userMessage = "Begin."
	}

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
