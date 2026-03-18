// Package templates provides embedded templates for scaffolding new agents.
package templates

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cowdogmoo/squad/logging"
)

// LangVerifyCommands maps language to verification commands for scaffolded agents.
var LangVerifyCommands = map[string]string{
	"go":      "go build ./... && go test ./...",
	"python":  "ruff check . && python -m py_compile *.py",
	"bash":    "shellcheck *.sh",
	"ansible": "ansible-lint",
	"generic": "",
}

// LangFilePatterns maps language to glob patterns for scaffolded agents.
var LangFilePatterns = map[string]string{
	"go":      "**/*.go",
	"python":  "**/*.py",
	"bash":    "**/*.sh",
	"ansible": "**/*.yml",
	"generic": "**/*",
}

// CreateOptions configures agent scaffolding.

type CreateOptions struct {
	Name        string
	Lang        string
	Description string
	AgentsDir   string
	Force       bool
}

// CreateAgent scaffolds a new agent from templates.
func CreateAgent(ctx context.Context, opts CreateOptions) error {
	if !IsValidAgentName(opts.Name) {
		return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens", opts.Name)
	}

	if _, ok := LangVerifyCommands[opts.Lang]; !ok {
		return fmt.Errorf("unknown language %q: must be one of go, python, bash, ansible, generic", opts.Lang)
	}

	agentPath := filepath.Join(opts.AgentsDir, opts.Name)

	if _, err := os.Stat(agentPath); err == nil {
		if !opts.Force {
			return fmt.Errorf("agent %q already exists at %s (use --force to overwrite)", opts.Name, agentPath)
		}
		logging.WarnContext(ctx, "Overwriting existing agent at %s", agentPath)
	}

	description := opts.Description
	if description == "" {
		description = generateDescription(opts.Name, opts.Lang)
	}

	if err := os.MkdirAll(filepath.Join(agentPath, "references"), 0o755); err != nil {
		return fmt.Errorf("failed to create agent directory: %w", err)
	}

	data := AgentData{
		Name:        opts.Name,
		NameTitle:   ToTitleCase(opts.Name),
		Description: description,
		Lang:        opts.Lang,
		Version:     "0.1.0",
	}

	files := map[string]string{
		"agent.yaml":                            "agent.yaml.tmpl",
		"system.md":                             "system.md.tmpl",
		"agent.md":                              "agent.md.tmpl",
		"task.md":                               "task.md.tmpl",
		"README.md":                             "README.md.tmpl",
		"references/" + opts.Name + "-guide.md": "reference.md.tmpl",
	}

	for outFile, tmplFile := range files {
		content, err := Render(tmplFile, data)
		if err != nil {
			return fmt.Errorf("failed to render %s: %w", tmplFile, err)
		}

		outPath := filepath.Join(agentPath, outFile)
		if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", outFile, err)
		}
		logging.InfoContext(ctx, "Created %s", outPath)
	}

	logging.InfoContext(ctx, "Agent %q created successfully at %s", opts.Name, agentPath)
	logging.InfoContext(ctx, "Next steps:")
	logging.InfoContext(ctx, "  1. Edit references/%s-guide.md with domain knowledge", opts.Name)
	logging.InfoContext(ctx, "  2. Customize system.md with agent-specific rules")
	logging.InfoContext(ctx, "  3. Test with: squad run --agent %s", opts.Name)

	return nil
}

// CopyAgent copies an existing agent to a new name.
func CopyAgent(ctx context.Context, agentsDir, from, to string, force bool) error {
	if !IsValidAgentName(to) {
		return fmt.Errorf("invalid agent name %q: must be lowercase alphanumeric with hyphens", to)
	}

	srcPath := filepath.Join(agentsDir, from)
	dstPath := filepath.Join(agentsDir, to)

	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return fmt.Errorf("source agent %q not found at %s", from, srcPath)
	}

	if _, err := os.Stat(dstPath); err == nil {
		if !force {
			return fmt.Errorf("agent %q already exists at %s (use --force to overwrite)", to, dstPath)
		}
		if err := os.RemoveAll(dstPath); err != nil {
			return fmt.Errorf("failed to remove existing agent: %w", err)
		}
	}

	if err := copyDir(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to copy agent: %w", err)
	}

	manifestPath := filepath.Join(dstPath, "agent.yaml")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest: %w", err)
	}

	updated := strings.Replace(string(content), "name: "+from, "name: "+to, 1)
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	logging.InfoContext(ctx, "Agent %q copied from %q to %s", to, from, dstPath)
	logging.InfoContext(ctx, "Don't forget to update the references and customize the prompts!")

	return nil
}

// IsValidAgentName checks if the agent name is valid.
func IsValidAgentName(name string) bool {
	matched, _ := regexp.MatchString(`^[a-z][a-z0-9-]*[a-z0-9]$`, name)
	return matched || (len(name) >= 2 && regexp.MustCompile(`^[a-z][a-z0-9]*$`).MatchString(name))
}

// ToTitleCase converts hyphenated-name to Title Case.
func ToTitleCase(name string) string {
	words := strings.Split(name, "-")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// generateDescription creates a default description based on name and language.
func generateDescription(name, lang string) string {
	titleName := ToTitleCase(name)

	switch lang {
	case "go":
		return fmt.Sprintf("Autonomous %s agent for Go codebases", titleName)
	case "python":
		return fmt.Sprintf("Autonomous %s agent for Python codebases", titleName)
	case "bash":
		return fmt.Sprintf("Autonomous %s agent for Bash scripts", titleName)
	case "ansible":
		return fmt.Sprintf("Autonomous %s agent for Ansible playbooks and roles", titleName)
	default:
		return fmt.Sprintf("Autonomous %s agent", titleName)
	}
}

// copyDir recursively copies a directory.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, content, info.Mode())
	})
}
