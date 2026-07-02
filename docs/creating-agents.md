# Creating Agents

A step-by-step guide to creating new Squad agents.

> New to prompt engineering? Read [Prompt Engineering Basics](./prompt-engineering-basics.md) before writing your first `system.md`.

## Quick Start (3 Commands)

```bash
# 1. Create agent from template
squad init agent my-review --lang go

# 2. Edit prompts (see Agent Structure below)
vim agents/my-review/system.md

# 3. Test
squad run --agent my-review --print
```

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

# Optional: ranked model preferences. First entry is primary; later entries
# are fallbacks when their provider credentials are detected.
models:
  - model: claude-sonnet-4-6
    provider: anthropic
  - model: gpt-4.1-mini
    provider: openai

# Optional: per-agent run policy
max_iterations: 100        # iteration cap (0 = use CLI default)
edit_deadline: 25          # stop after N iterations with no Edit calls
disable_task: false        # when true, the Task tool is not registered
comments_only: false       # when true, Edit/MultiEdit reject non-comment changes
isolation: ""              # "worktree" | "branch" | "commit" | "staged" | ""
working_dir: ""             # set "none" for remote-only agents (no local FS tools)

# Optional: execution environment (local, docker, ssm, kubectl)
environment:
  type: docker
  options:
    image: golang:1.26
    volumes: ".:/workspace"
    working_dir: /workspace

# Optional: cost estimation hints for --dry-run
budget:
  max_tokens: 4000
  estimated_iterations: 12
  scale_factor: files          # cost scales with source file count
  files_per_iteration: 4
  children:                    # child agents dispatched via Task tool
    - go-review
    - go-security-audit

# Optional: structured output contract
output:
  format: json                 # json | markdown (default: markdown)
  schema:
    type: object
    properties:
      findings:
        type: array

# Optional: MCP server dependencies
mcp_servers:
  - name: grafana
    transport: sse
    url: https://grafana.example.com/mcp
  - name: mytools
    command: npx
    args: ["@myorg/mcp-server"]

# Optional: Agent Skills catalog control. See docs/skills.md.
# Omit the whole block to auto-enable whenever any skill is discovered.
skills:
  enabled: true                # set false to disable skills for this agent
  scopes: [repo, catalog]      # which scopes to surface
  allow: []                    # exclusive allowlist of skill names
  deny: []                     # remove skills by name (only when allow is empty)

# Optional: external CLI dependencies the agent invokes via Bash.
# The runner verifies each on PATH before invoking the model and aborts
# with install hints on missing tools. See "Declaring Tool Dependencies".
requires:
  commands:
    - name: gosec
      install:
        brew: gosec
        go: github.com/securego/gosec/v2/cmd/gosec@latest
    - name: govulncheck
      install:
        go: golang.org/x/vuln/cmd/govulncheck@latest
```

Only `name`, `entrypoint`, `wrapper`, and `task` are required. Everything else is optional.

**Notable manifest options:**

- `comments_only` is for cleanup agents (e.g. `go-scrub-comments`). When set, the Edit and MultiEdit tools reject any change that touches non-comment lines, so an LLM hallucinating new code can't silently mutate the codebase.
- `disable_task` removes the `Task` tool from the agent, useful for leaf agents that shouldn't dispatch child runs.
- `edit_deadline` enters a grace period after N iterations without an Edit; reads are blocked so the model is forced to commit pending fixes.
- `isolation` defers to the CLI `--isolate` flag and, ultimately, the config default. `worktree` is the safest for agents that apply diffs.

### system.md (Main Prompt)

The core prompt defines identity, rules, and workflow. Start it with a YAML
frontmatter block — the **Claude-native format**. This is the format every
official [squad-agents](https://github.com/cowdogmoo/squad-agents) agent
uses, and it makes the same file loadable by Claude Code with no conversion:

```markdown
---
name: my-review
description: "Reviews Go code for correctness issues. Use proactively when asked to review Go code. By default it fixes issues in place; say \"readonly\" or \"report only\" for a findings report with no edits."
tools: "Bash, Glob, Grep, Read, Edit, MultiEdit"
model: opus
---
# IDENTITY

You are an autonomous code review agent. By default you run in **edit
mode**: fix issues and verify compilation. If the caller's prompt asks for
"readonly" or "report only", run in **readonly mode**: report issues and
change nothing.

# HARD RULES

1. Read before writing - never edit a file you haven't read
2. Be proportional - only fix real bugs, not stylistic preferences
3. Follow conventions - match existing code patterns

# WORKFLOW

1. Glob for files, Read to understand
2. Analyze against criteria
3. Edit mode only: fix issues, verify build/tests
4. Emit report
```

When the entrypoint starts with a frontmatter fence, squad:

- strips the frontmatter — squad reads its own metadata from `agent.yaml`;
  the frontmatter fields are for Claude Code
- skips Go-template rendering for **all** of that agent's prompt files
  (`system.md`, `agent.md`, `task.md`). The body is delivered verbatim, so
  literal `{{.VAR}}` text (Taskfile or Helm examples, say) survives
  untouched.

Write the body plain — no template syntax:

- **Modes**: no `{{if .Mode}}` blocks. Describe both modes in prose: edit
  as the default, readonly opt-in on caller phrases ("readonly", "report
  only", "do not modify"). Squad injects a literal `Mode: readonly` line
  into the assembled prompt under `--mode readonly`; Claude Code callers
  put the keyword in the task prompt. Readonly runs also hard-reject
  Write/Edit/MultiEdit at the tool layer, so the prose is belt and the
  runner is suspenders.
- **Variables**: no `{{.Var}}` / `{{.Default}}`. Bake the default into
  prose: "target 75% unless the caller specifies otherwise."
- **Includes**: no `{{include}}`. Inline shared content, or move it into a
  [skill](./skills.md) and reference it as `Skill("name")` — skill
  references resolve in both hosts.
- **References**: squad injects `agent.yaml` `references:` into the
  prompt; Claude Code does not. Phrase the knowledge-base section for both
  hosts: "If the host has not already injected `criteria.md` into your
  prompt, Read `<absolute path>` on your FIRST iteration."

### Running the Same Agent in Claude Code

A Claude-native `system.md` is a valid Claude Code agent definition as-is.
Register it with a symlink:

```bash
ln -s /path/to/agents/my-review/system.md ~/.claude/agents/my-review.md
```

Claude Code reads `name`, `description`, `tools`, and `model` from the
frontmatter and ignores `agent.yaml` entirely (unknown frontmatter keys are
also ignored, so squad-only metadata can live there if you prefer one
file). The `description` doubles as Claude's auto-delegation router —
include when-to-use trigger text and the readonly opt-in phrases. Edits to
the file reach both hosts immediately; there is no copy to drift.

### Legacy Templated Format

Prompt files without frontmatter are rendered as Go text/templates. New
agents should use the Claude-native format; the template path remains for
existing agents.

#### Template Variables

- `{{.Mode}}` - Current mode (`edit` or `readonly`)
- `{{.Var "KEY"}}` - Custom variable passed via `--var KEY=VALUE`
- `{{.Vars.KEY}}` - Alternate syntax for custom variables

#### Mode Conditionals

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

#### Escaping Literal Braces

In templated files, a literal `{{` in prose or examples must be escaped as
`{{"{{"}}` or the renderer will evaluate it (an unknown variable becomes
`<no value>`). Claude-native files need no escaping — one more reason to
prefer them for agents whose subject matter is itself templated (Taskfile,
Helm, GitHub Actions).

### Execution Backends

Agents run commands locally by default. To run in a different environment,
set the `environment` field in `agent.yaml`. See
[Execution Backends](./execution-backends.md) for all options and examples.

### Declaring Tool Dependencies

Agents that shell out to external CLIs (linters, scanners, formatters)
declare them in `requires.commands`. The runner verifies each is on PATH
before invoking the model — so a missing tool fails in milliseconds with
an install hint instead of mid-run after burning tokens.

```yaml
requires:
  commands:
    - name: bandit
      install:
        brew: bandit
        pipx: bandit
        url: https://bandit.readthedocs.io
    - name: pip-audit
      install:
        pipx: pip-audit
```

**Fields:**

- `name` (required): the bare binary name as it would be invoked. Must not
  contain slashes or whitespace.
- `install` (optional): a map from package-manager key to install
  identifier. The runner does not execute these — they're rendered into
  the failure message as human-readable hints.

**Recognized install keys** (rendered with the right command prefix and
ordered by likely availability): `brew`, `pipx`, `pip`, `npm`, `cargo`,
`go`, `apt`, `dnf`, `pacman`. The special key `url` renders verbatim.
Unknown keys (e.g. `snap`, `nix`) are passed through as `key: value`.

**Composed agents** should declare the union of all stages' tool
dependencies at the top level. Sub-agent manifests may also declare
their own `requires:` block, which is checked when that sub-agent runs
directly. Dry-runs (`--dry-run`) skip the preflight.

### MCP Server Integration

Agents can declare MCP server dependencies in `agent.yaml`. These are
automatically connected when the agent starts, providing additional tools
like database queries, monitoring APIs, or custom services.

Two transport types are supported:

- **stdio**: Launches a local process (`command` + `args`)
- **sse**: Connects to a remote HTTP endpoint (`url`)

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

See [Agent Quality Rubric](./agent-quality.md#3-iteration-efficiency-15-of-grade)
for iteration targets by codebase size.

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

Consistent reporting, with one labeled section per mode:

```markdown
# OUTPUT FORMAT

## Edit-mode report

### Changes Made
| File | Change | Rationale |

### Verification
- [ ] Build passes
- [ ] Tests pass

## Readonly-mode report

### Issues Found
| Severity | File | Line | Issue | Recommendation |
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

Claude-native agents (frontmatter): mode is prose-dispatched. Make sure the
body names the trigger phrases ("readonly", "report only") and that squad
runs with `--mode readonly` — check the assembled bundle for the
`Mode: readonly` line.

Legacy templated agents: ensure conditionals use exact syntax:

```markdown
{{if eq .Mode "edit"}}...{{end}}     # Correct
{{if .Mode == "edit"}}...{{end}}     # Wrong
```

## Reference

- [Pipelines](./pipelines.md) - Multi-agent orchestration
- [Agent Quality Rubric](./agent-quality.md) - Evaluation criteria
- [squad-agents](https://github.com/cowdogmoo/squad-agents) - Official agents
- [CONTRIBUTING.md](https://github.com/cowdogmoo/squad-agents/blob/main/CONTRIBUTING.md) - Contribution guidelines
