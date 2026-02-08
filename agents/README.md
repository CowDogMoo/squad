# Squad Agents

This directory contains agent definitions for autonomous code review and testing.

## Agent Structure

Each agent includes:

- `agent.yaml` - manifest with metadata, references, and task
- `agent.md` - agent-mode wrapper instructions
- `system.md` - core system prompt (identity, rules, capabilities)
- `task.md` - task instructions (always included in system bundle)
- `references/` - knowledge base documents used by the agent

## Prompt Architecture

Following [Anthropic's context engineering best practices](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents),
prompts are modular:

```
System Bundle (always included):
├── agent.md     - wrapper with execution rules
├── system.md    - core identity and capabilities
├── references/  - knowledge base
└── task.md      - task instructions

User Message:
└── CLI prompt   - additional instructions (default: "Begin.")
```

The `task.md` contains the agent's standard instructions and is always
included in the system bundle. The CLI prompt provides additional context
from the user.

## Agent Manifest (agent.yaml)

```yaml
name: go-review
version: 0.2.0
description: Autonomous Go code review agent
entrypoint: system.md
wrapper: agent.md
references:
  - references/go-review-criteria.md
task: task.md
```

## Mode Support

Agents support multiple modes via conditional blocks in prompt files.
The `--mode` flag controls which content is included:

```bash
# Default edit mode - agent can make changes
squad run --agent go-review

# Readonly mode - agent only analyzes, no edits
squad run --agent go-review --mode readonly
```

### Conditional Block Syntax

Use Go `text/template` conditionals in any prompt file:

```markdown
# IDENTITY

{{if eq .Mode "edit"}}
You are an autonomous code review agent. You discover issues, fix them,
and verify the result compiles.
{{end}}
{{if eq .Mode "readonly"}}
You are a code analysis agent. You discover issues and report them.
You MUST NOT modify any files.
{{end}}

# COMMON CONTENT

This content appears in all modes.
```

Available template features:

- `{{if eq .Mode "value"}}...{{end}}` - include if mode matches
- `{{if ne .Mode "value"}}...{{end}}` - include if mode does NOT match
- `{{else}}` - alternative block
- Nesting is fully supported

- **Edit mode** (default): Agent can use Edit/Write tools to fix issues
- **Readonly mode**: Agent only uses Read/Grep/Glob to analyze and report
- **Custom modes**: Define any mode name and add corresponding blocks

Content outside conditional blocks is included in all modes.

## Usage

```bash
# Run with default task instructions (user message: "Begin.")
squad run --agent go-review

# Add custom instructions (task.md still included, your text becomes user message)
squad run --agent go-review "Focus only on error handling in cmd/"

# Run in readonly mode (uses readonly conditional blocks)
squad run --agent go-review --mode readonly
```

### How Prompts Work

The agent always receives its `task.md` instructions in the system bundle.
The CLI prompt (if any) becomes the user message:

| Command | System Bundle | User Message |
|---------|---------------|--------------|
| `squad run --agent go-review` | system.md + task.md + refs | "Begin." |
| `squad run --agent go-review "Focus on cmd/"` | system.md + task.md + refs | "Focus on cmd/" |

The CLI prompt **adds context**, it doesn't replace task.md. Use it to:

- Narrow scope: `"Only review files in pkg/auth/"`
- Add constraints: `"Skip any changes to generated code"`
- Provide context: `"This is a new feature for OAuth support"`

## Available Agents

- `ansible-molecule` - Molecule test quality
- `ansible-review` - Ansible code quality
- `go-cobra` - Cobra/Viper best practices
- `go-doc-comments` - Go documentation
- `go-review` - Go code quality
- `go-security-audit` - Go security vulnerabilities
- `go-taskfile` - Taskfile best practices
- `go-tests` - Go test coverage
- `python-doc-comments` - Python docstrings
- `python-review` - Python code quality
- `python-tests` - Python test coverage
