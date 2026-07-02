package scaffold

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
		"agent.yaml": "agent.yaml.tmpl",
		"system.md":  "system.md.tmpl",
		"agent.md":   "agent.md.tmpl",
		"task.md":    "task.md.tmpl",
		"README.md":  "README.md.tmpl",
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

	if err := writeReferenceSkill(ctx, opts, data); err != nil {
		return err
	}

	logging.InfoContext(ctx, "Agent %q created successfully at %s", opts.Name, agentPath)
	logging.InfoContext(ctx, "Next steps:")
	logging.InfoContext(ctx, "  1. Edit skills/%s-guide/SKILL.md with domain knowledge", opts.Name)
	logging.InfoContext(ctx, "  2. Customize system.md with agent-specific rules")
	logging.InfoContext(ctx, "  3. Test with: squad run --agent %s", opts.Name)

	return nil
}

// writeReferenceSkill materializes the agent's knowledge base in the shared
// fleet layout: the document lives at <agentsDir>/skills/<name>-guide/SKILL.md
// (loadable by name via the Skill tool in any host) and
// <agent>/references/<name>-guide.md is a relative symlink to it, which keeps
// agent.yaml `references:` injection working for squad runs.
func writeReferenceSkill(ctx context.Context, opts CreateOptions, data AgentData) error {
	skillName := opts.Name + "-guide"
	skillDir := filepath.Join(opts.AgentsDir, "skills", skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	content, err := Render("reference.md.tmpl", data)
	if err != nil {
		return fmt.Errorf("failed to render reference.md.tmpl: %w", err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", skillPath, err)
	}
	logging.InfoContext(ctx, "Created %s", skillPath)

	linkPath := filepath.Join(opts.AgentsDir, opts.Name, "references", skillName+".md")
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to replace reference link %s: %w", linkPath, err)
	}
	target := filepath.Join("..", "..", "skills", skillName, "SKILL.md")
	if err := os.Symlink(target, linkPath); err != nil {
		return fmt.Errorf("failed to create reference symlink %s: %w", linkPath, err)
	}
	logging.InfoContext(ctx, "Created %s -> %s", linkPath, target)
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

// IsValidAgentName reports whether name is a valid agent name.
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

// PipelineData contains the data for rendering pipeline templates.
type PipelineData struct {
	Name        string // e.g. "security-pipeline"
	Description string // e.g. "Security analysis pipeline"
}

// CreatePipelineOptions configures pipeline scaffolding.
type CreatePipelineOptions struct {
	Name        string
	Description string
	OutputDir   string
	Force       bool
}

// CreatePipeline scaffolds a new pipeline config from templates.
func CreatePipeline(ctx context.Context, opts CreatePipelineOptions) error {
	if !IsValidAgentName(opts.Name) {
		return fmt.Errorf("invalid pipeline name %q: must be lowercase alphanumeric with hyphens", opts.Name)
	}

	outPath := filepath.Join(opts.OutputDir, opts.Name+".yaml")

	if _, err := os.Stat(outPath); err == nil {
		if !opts.Force {
			return fmt.Errorf("pipeline %q already exists at %s (use --force to overwrite)", opts.Name, outPath)
		}
		logging.WarnContext(ctx, "Overwriting existing pipeline at %s", outPath)
	}

	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	description := opts.Description
	if description == "" {
		description = fmt.Sprintf("Pipeline for %s", ToTitleCase(opts.Name))
	}

	data := AgentData{
		Name:        opts.Name,
		NameTitle:   ToTitleCase(opts.Name),
		Description: description,
		Version:     "0.1.0",
	}

	content, err := Render("pipeline.yaml.tmpl", data)
	if err != nil {
		return fmt.Errorf("failed to render pipeline template: %w", err)
	}

	if err := os.WriteFile(outPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write pipeline config: %w", err)
	}

	logging.InfoContext(ctx, "Pipeline %q created at %s", opts.Name, outPath)
	return nil
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
