# Creating Agents

A step-by-step guide to creating new Squad agents.

## Quick Start (3 Commands)

```bash
# 1. Create agent from template
squad init agent my-review --lang go

# 2. Edit prompts (see Agent Structure below)
vim agents/my-review/system.md

# 3. Test
squad run --agent my-review --print
```

That's it. Your agent is ready to use.

## Agent Structure

Every agent needs these files:

```text
my-review/
├── agent.yaml      # Manifest (required)
├── system.md       # Main prompt - identity, rules, workflow (required)
├── agent.md        # Execution wrapper (required)
├── task.md         # Default task instructions (required)
└── references/     # Knowledge base docs (optional)
    └── criteria.md
```

### agent.yaml (Manifest)

```yaml
name: my-review
version: 0.1.0
description: Short description of what this agent does
entrypoint: system.md
wrapper: agent.md
references:
  - references/criteria.md
task: task.md
```

### system.md (Main Prompt)

The core prompt defines identity, rules, and workflow:

```markdown
# IDENTITY

{{if eq .Mode "edit"}}
You are an autonomous code review agent. Fix issues and verify compilation.
{{end}}
{{if eq .Mode "readonly"}}
You are an analysis agent. Report issues but do NOT modify files.
{{end}}

# HARD RULES

1. Read before writing - never edit a file you haven't read
2. Be proportional - only fix real bugs, not stylistic preferences
3. Follow conventions - match existing code patterns

# WORKFLOW

1. Glob for files, Read to understand
2. Analyze against criteria
3. {{if eq .Mode "edit"}}Fix issues, verify build/tests{{end}}
4. Emit report
```

### Mode Conditionals

Use `{{if eq .Mode "edit"}}...{{end}}` for edit-mode-only content:

```markdown
{{if eq .Mode "edit"}}
- **Edit**: Make targeted replacements in files
- **Bash**: Run commands (build, test, lint)
{{end}}
{{if eq .Mode "readonly"}}
Do NOT modify any files. Report only.
{{end}}
```

## Creating From Existing Agents

Fork an existing agent when your use case is similar:

```bash
# Copy go-review and customize
squad init agent my-review --from go-review
```

## Language Templates

Templates provide language-specific starting points:

```bash
squad init agent go-review --lang go
squad init agent py-review --lang python
squad init agent ansible-review --lang ansible
squad init agent shell-review --lang bash
squad init agent generic-review --lang generic
```

## Testing Your Agent

### Basic Test

```bash
# Run with output to terminal
squad run --agent my-review --print

# Test readonly mode
squad run --agent my-review --mode readonly --print

# Check the bundled prompt
squad run --agent my-review --print-bundle --dry-run
```

### Iteration Testing

Track iterations to ensure efficiency:

```bash
# Run and observe iteration count
squad run --agent my-review --verbose
```

**Targets:**

| Codebase | Target | Max |
|----------|--------|-----|
| Small (≤20 files) | ≤12 iterations | 18 |
| Medium (21-50 files) | ≤25 iterations | 35 |
| Large (50+ files) | ≤40 iterations | 60 |

### Grading

Grade agent output against the rubric:

```bash
# Run and capture output
squad run --agent my-review > output.md

# Grade the output
squad grade output.md --agent my-review --iterations 12 --files 8
```

See [agent-quality.md](./agent-quality.md) for the full rubric.

## Common Patterns

### Efficiency Rules

Add to every agent's system.md:

```markdown
# EFFICIENCY

1. Read each file ONCE - catalog issues in memory
2. Batch edits - multiple fixes per Edit call
3. After verification passes, emit report IMMEDIATELY
4. Do NOT re-read files after editing
```

### Proportionality

Prevent over-engineering:

```markdown
# PROPORTIONALITY

Before making a fix, ask: "Does this prevent a real bug?"

Skip:
- Micro-optimizations (strings.Builder for 3-element loops)
- Stylistic preferences without functional impact
- Changes that add complexity without clear benefit
```

### Output Format

Consistent reporting:

```markdown
# OUTPUT FORMAT

{{if eq .Mode "edit"}}
## Changes Made
| File | Change | Rationale |

## Verification
- [ ] Build passes
- [ ] Tests pass
{{end}}
{{if eq .Mode "readonly"}}
## Issues Found
| Severity | File | Line | Issue | Recommendation |
{{end}}
```

## Publishing Agents

### Local Development

During development, keep agents in `./agents/`:

```bash
# Squad looks here first
ls ./agents/my-review/
```

### Share via Git

Publish to a git repository:

```bash
# Push your agents repo
git init
git add .
git commit -m "Add my-review agent"
git remote add origin https://github.com/user/my-agents.git
git push -u origin main

# Others can use it
squad agents add https://github.com/user/my-agents.git
```

### Contribute to Official Agents

Submit a PR to [squad-agents](https://github.com/cowdogmoo/squad-agents):

1. Fork the repository
2. Add your agent
3. Test thoroughly
4. Submit PR with test results

See [CONTRIBUTING.md](https://github.com/cowdogmoo/squad-agents/blob/main/CONTRIBUTING.md)
for detailed guidelines.

## Troubleshooting

### Agent Not Found

```bash
# Check agent sources
squad agents sources

# Update git repos
squad agents update

# List available agents
squad agents list
```

### Bundle Issues

```bash
# View the assembled prompt
squad run --agent my-review --print-bundle --dry-run
```

### Mode Not Working

Ensure conditionals use exact syntax:

```markdown
{{if eq .Mode "edit"}}...{{end}}     # Correct
{{if .Mode == "edit"}}...{{end}}     # Wrong
```

## Reference

- [Agent Quality Rubric](./agent-quality.md) - Evaluation criteria
- [squad-agents](https://github.com/cowdogmoo/squad-agents) - Official agents
- [CONTRIBUTING.md](https://github.com/cowdogmoo/squad-agents/blob/main/CONTRIBUTING.md) - Detailed guidelines
