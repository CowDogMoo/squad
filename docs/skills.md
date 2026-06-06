# Skills

Skills are single-directory capabilities a running agent loads on demand. They follow [Anthropic's open Agent Skills standard](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview) — the same format Claude Code, Codex CLI, and ChatGPT consume — so a skill checked into your repo runs everywhere without conversion.

A skill is one folder with a `SKILL.md` inside it. The file's YAML frontmatter advertises the skill to the agent; the markdown body is the playbook the agent reads when it triggers the skill.

```
my-skill/
├── SKILL.md        # required: name + description + body
├── references/     # optional: long-form docs, schemas
├── scripts/        # optional: executable helpers the skill invokes
└── assets/         # optional: templates, fixtures
```

```yaml
---
name: grocery-add-to-cart
description: |
  Add weekly groceries from the planner doc to the Amazon Whole Foods
  cart. Stops at the cart — never checks out. Use this when the user
  asks for "groceries this week" or references the planner doc.
---

# Grocery Add To Cart
...
```

## Quick start

```bash
# Scaffold a skill in the current repo.
squad skill new grocery-add-to-cart

# Edit the body to describe when and how the agent should use it.
$EDITOR .squad/skills/grocery-add-to-cart/SKILL.md

# Confirm it passes the spec checks.
squad skill validate .squad/skills/grocery-add-to-cart

# See what your next agent run will be told about.
squad skill list
```

Run an agent and the catalog block lands in its system prompt automatically:

```
## Available skills

You have access to the following skills via the `Skill` tool. ...

- **grocery-add-to-cart**: Add weekly groceries from the planner doc...
```

The agent calls `Skill("grocery-add-to-cart")` when the task matches; squad delivers the full SKILL.md body and unlocks Read / Bash access inside the skill's directory for the rest of the run.

## Scopes

Three places skills can live, in precedence order. When the same `name` appears at multiple scopes, the higher-precedence one wins and the rest are *shadowed* — still on disk, hidden from the agent.

| Scope | Location | Use case |
|---|---|---|
| **repo** | `<cwd>/.squad/skills/<name>/SKILL.md` (checked into git) | Project-specific skills shared with the team |
| **global** | `$XDG_CONFIG_HOME/squad/skills/<name>/SKILL.md` | Personal cross-project skills |
| **catalog** | cloned git repos under the skills cache + paths in `cfg.Skills.LocalPaths` | Shared libraries: a team's `squad-skills` repo, a vendor's published catalog |

`squad skill list --all` shows shadowed entries with the `(shadowed)` marker so collisions are auditable.

## Catalog sources

Skill catalogs are git-backed. Register one and squad clones it into the local cache; `squad skill update` pulls the latest.

```bash
# Add the official squad-skills catalog.
squad skill add official https://github.com/cowdogmoo/squad-skills.git

# Add a team-shared skills repo.
squad skill add myteam https://github.com/example/squad-skills.git

# Register a local directory of skills (no clone, just a path).
squad skill add local /opt/shared/skills

# List configured catalog sources.
squad skill sources

# Pull latest from every registered repo.
squad skill update

# Unregister.
squad skill remove myteam
```

`file://` URLs are accepted for local-repo workflows and CI tests. The catalog is sourced from `cfg.Skills.Repositories` and `cfg.Skills.LocalPaths`; see [configuration.md](configuration.md) for the schema.

## The `Skill` tool

While an agent is running, the `Skill(name)` tool delivers a skill's full body and pushes its directory onto the run's *skill stack*. While on the stack:

- **Read** can open files anywhere under the skill directory (in addition to the working dir).
- **Grep** can search inside the skill directory.
- **Bash** commands get an extra environment variable, `SQUAD_SKILL_DIR`, set to the absolute path of the top-of-stack skill. Bundled scripts use it to resolve sibling files regardless of cwd:

  ```bash
  bash "$SQUAD_SKILL_DIR/scripts/scrape-recipe.sh"
  ```

- **Write / Edit / MultiEdit** stay strictly anchored to the working directory. Skills are reference material, not write targets.

Each `Skill(...)` call emits a `skill_loaded` event into `events.jsonl` for the run's session, so the trail of which skill the agent reached for is auditable. On `squad run --resume <id>`, the stack is rehydrated from those events — a resumed run picks up the skills the prior run loaded.

## The `Confirm` tool

Useful skills pause for a human before taking irreversible actions (adding to a cart, sending an email, dropping a table). The `Confirm(summary, options?)` tool wraps that pause:

```python
# from inside the agent's reasoning, conceptually
result = Confirm({
    "summary": "Add 17 items to the Whole Foods cart?",
    "options": ["yes", "no"]  # optional, defaults to ["yes","no"]
})
```

Resolution depends on whether the run is attached to a terminal:

- **TTY**: squad prints the summary and numbered options to stderr, reads one line from stdin, returns the chosen option. Accepts the index (`2`), the exact label (`yes`), or a unique prefix (`y`).
- **Non-TTY** (routines, CI, headless): consult the `--auto-confirm` flag:
  - `--auto-confirm=yes` → returns the first option (typically `yes`).
  - `--auto-confirm=no` → returns the second option.
  - `--auto-confirm=abort` → errors so the skill aborts gracefully.
  - **Unset (the default)** → same as `abort`. Unattended runs fail loudly instead of silently auto-approving destructive actions.

Every Confirm call writes a `confirm_resolved` session event with the summary, options, resolution, and the path that produced the answer.

## CLI reference

```
squad skill list                 # discovered skills, by scope
squad skill list --all           # include shadowed entries
squad skill show <name>          # full SKILL.md with metadata
squad skill validate <path>      # spec-conformance check
squad skill new <name>           # scaffold a starter SKILL.md
                                 #   flags: --global, --repo <path>, --description "..."
squad skill add <alias> <url>    # register a catalog source (git URL or local path)
squad skill remove <alias>       # unregister
squad skill update               # git pull every registered catalog repo
squad skill sources              # list configured catalog sources
```

`squad run` gains:

```
--skills-enabled            # force the catalog on for this run (overrides agent.yaml)
--skills-disabled           # force off
--allow-skill <name>        # exclusive allowlist (repeatable)
--deny-skill <name>         # blocklist (repeatable)
--auto-confirm yes|no|abort # how Confirm resolves in non-TTY
```

## `agent.yaml` skills block

Per-agent control lives alongside the agent manifest:

```yaml
skills:
  enabled: true                  # default true when any skill is discoverable
  scopes: [repo, global]         # which scopes to surface (default: all)
  allow: [grocery-add-to-cart]   # exclusive allowlist; wins over deny + scopes
  deny: [debug-fs]               # blocklist applied after scopes
```

CLI flags override the manifest. `allow` wins over `deny` wins over `scopes`. An empty / unset block falls through to "enabled, all scopes, no filters."

## Spec validation

`squad skill validate` mirrors the open Agent Skills spec:

- `name`: ≤64 chars, `[a-z0-9-]`, no XML, no leading/trailing hyphen.
- `description`: non-empty, ≤1024 chars, no XML.
- Body: ≤64 KiB (hard), >5 KiB triggers a "consider splitting" warning.
- Manifest `name` must match the containing directory (otherwise catalog discovery can't find it).
- `references` / `scripts` paths in the body must not escape the skill directory (`..` traversal rejected).
- Scripts under `scripts/` should be either executable or shebanged so the agent knows how to invoke them.
- Names containing `anthropic` or `claude` get a warning (the spec calls them out as reserved, but Anthropic's own first-party `claude-api` skill ships with the substring, so we don't fatally reject).

The skill ecosystem moves fast. If a skill written for Claude Code validates here, it runs here. Tested against the [anthropics/skills](https://github.com/anthropics/skills) repository: 17 of 18 first-party skills validate cleanly; the one outlier is the starter template, which deliberately ships with a placeholder name to force the user to rename.

## Security model

Skills are trusted code — they can direct an agent to run scripts in the skill directory, hit external APIs via MCP, and write files in the working directory.

- **Only install skills from trusted sources.** Audit unfamiliar SKILL.md bodies and scripts the same way you would a build script.
- **Catalog sources are remote.** A compromised `squad skill update` fetches whatever the upstream now publishes. Pin to specific revisions in `cfg.Skills.Repositories` if that matters for your team.
- **Skill-dir Read/Bash relaxation is one-way.** Even on the stack, Read / Grep cannot escape the skill directory itself — `../etc/passwd` from inside a skill still errors.
- **Write / Edit are not relaxed.** A skill cannot mutate itself mid-run.

## Interop with Claude Code skills

The open standard is the open standard. A skill authored for Claude Code drops into `$XDG_CONFIG_HOME/squad/skills/` unchanged. The reverse holds: a skill authored for squad lives in Claude Code too.

Squad does not introduce squad-specific frontmatter fields. If you see `x-squad:` namespaced keys in a future SKILL.md, that's where private extensions would live; the standard fields stay untouched.

We do not adopt the `allowed-tools` field some Claude Code skills carry. Squad's per-agent tool gating already handles that at a coarser level, and conflating the two would invent a squad-specific tool-name vocabulary the spec doesn't define.

## When to use a skill vs. an agent

| | Skill | Agent |
|---|---|---|
| Triggered by | A running agent during its task | The user on the CLI |
| Selection mechanism | The agent reads descriptions and decides | Explicit `--agent` flag |
| Lifetime | A subtask within a run | The whole run |
| Discovery cost | Only `name` + `description` at boot | Loaded fully at launch |

Use an agent when the work is the whole job. Use a skill when the work is a *piece* of a job that other agents should reach for when the task description matches.
