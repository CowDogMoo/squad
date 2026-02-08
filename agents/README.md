# Squad Agents

This directory contains agent definitions for autonomous code review and testing.

## Agent Structure

Each agent includes:

- `agent.yaml` - manifest with metadata, references, and task
- `agent.md` - agent-mode wrapper instructions
- `system.md` - core system prompt (identity, rules, capabilities)
- `task.md` - task instructions (always included in system bundle)
- `task_readonly.md` - task instructions for readonly mode
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
modes:
  readonly:
    entrypoint: system-readonly.md
    wrapper: agent-readonly.md
    task: task_readonly.md
```

## Usage

```bash
# Run with default task instructions
squad run --agent go-review

# Add custom instructions (task.md still included)
squad run --agent go-review "Focus only on error handling in cmd/"

# Run in readonly mode (uses task_readonly.md)
squad run --agent go-review --mode readonly
```

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
