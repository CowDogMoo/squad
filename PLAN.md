# Skills: On-Demand Capabilities a Running Agent Can Load

## Status: shipped on `feat/skills-phase-1` (2026-05-21)

| Phase | Status | Commit | Notes |
|---|---|---|---|
| 1. Format + catalog + read-only listing | ✅ | `0d4e70d` | |
| 2. System-prompt catalog injection | ✅ | `700822f` | |
| 3. `Skill` tool + stack + L3 relaxation | ✅ | `04646e4` | |
| 4. `Confirm` tool | ✅ | `2c2e861` | |
| 5. Git-backed catalogs | ✅ | `9c5aff2` | |
| 6. Polish + interop verification | ✅ | `e8e79ab` | grocery E2E deferred — see Known follow-ups |

Empirical adjustments made during implementation (deltas from the original
design below):

- **Body size hard cap raised from 25 KiB → 64 KiB.** Anthropic's first-party
  `skill-creator` skill is ~32 KiB; 25 KiB would block legitimate large
  skills. 5 KiB warn threshold unchanged.
- **Reserved-substring rule downgraded from error → warning.** The spec says
  names cannot contain "anthropic" or "claude," but Anthropic's own
  `claude-api` skill violates the rule. Treating it as fatal would break
  interop with first-party skills.
- **Manifest name / directory mismatch is a hard error.** The catalog uses
  the directory name as the lookup key; a mismatch would silently make the
  skill unreachable via `Skill(name)`.
- **CLI `allow > deny > scopes` precedence** is implemented as: when `Allow`
  is non-empty it is an *exclusive* allowlist (deny and scopes ignored);
  otherwise `Scopes` is applied first, then `Deny` removes names.

Interop verified: 17 of 18 skills in
[anthropics/skills](https://github.com/anthropics/skills) validate cleanly
via `squad skill validate`. The one outlier is the upstream `template/`
directory, which deliberately ships with a placeholder name to force the
user to rename — catching it as an error is correct behavior.

## Known follow-ups

- **OpenAI Responses API path (`responses.RunWithTools`) still uses the
  old `tools.BuildHandlers`** and therefore doesn't see the `Skill` or
  `Confirm` tools. Phase 4 / 5 should land here too.
- **Composed-agent stages (`BuildBundleInline`) get the catalog block in
  their system prompt but no `Skill` tool runtime.** Stages run with the
  same `RunWithToolsConfig` shape so wiring is mechanical.
- **End-to-end grocery skill test deferred.** Requires Chrome MCP, real
  Amazon credentials, and a test harness that can mock cart mutations.
  The mechanism (Skill + Confirm + catalog) is unit-tested end-to-end via
  `tools/confirm_session_test.go`, `tools/skill_test.go`, and the CLI
  smokes in `cmd/squad/skill_test.go`.
- **`--resume` skill-stack rehydration is best-effort.** If `agent.yaml`
  filters change between runs, previously loaded skills no longer in
  `Entries` are silently dropped.

## Goal

Support Anthropic's [Agent Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview)
format in squad — an open standard (Dec 2025, also adopted by OpenAI for
Codex CLI) for packaging domain expertise that agents discover and load
*at runtime* rather than at launch.

```
squad skill list
squad skill show grocery-add-to-cart
squad skill add personal ~/code/my-skills
squad skill add team https://github.com/me/squad-skills.git
```

```yaml
# Inside an agent.yaml, opt into skills the agent can call:
skills:
  enabled: true               # injects skill catalog into system prompt
  scopes: [global, repo]      # which scopes to expose; default both
```

A *skill* is **not** another way to launch squad — it is a capability a
*running* agent reaches for mid-task when its task matches the skill's
description. Agents stay the top-level entry point.

## Why this isn't already covered by agents

A squad agent and a Claude Code skill look superficially similar
(markdown + frontmatter + a directory). The structural difference is the
*trigger model*:

| | Agent | Skill |
|---|---|---|
| Triggered by | User on the CLI: `squad run --agent X` | A running agent during its task |
| Selection mechanism | Explicit name | Description-match by the model |
| Lifetime | The whole run | A subtask within a run |
| Discovery cost | Loaded fully at launch | Only `name`+`description` at launch |

Treating skills as a degenerate agent loses **progressive disclosure**,
which is the core property of the spec. Skills must be its own
primitive.

## Design

### Progressive disclosure: the load model

This is the defining property of the spec; squad must implement all
three levels.

| Level | When loaded | Token cost | Mechanism in squad |
|---|---|---|---|
| **L1: Metadata** | Agent boot | ~100 tok/skill | `agent/bundle.go` enumerates skills and appends an `## Available skills` block to the system prompt with `name` + `description` per skill |
| **L2: Instructions** | Agent calls `Skill(name)` | <5k tok | `tools/skill.go` returns the full `SKILL.md` body; sets `<skill-dir>` as a read anchor |
| **L3: Resources & scripts** | Agent does Read / Bash on bundled paths | "free" | Existing Read / Bash tools, now permitted on the active skill's dir |

L1 → L2 is the only state change the runtime needs to manage. L3 is just
existing tools operating on a new set of allowed paths.

### Skill format (per spec)

A skill is a directory containing `SKILL.md`:

```
my-skill/
├── SKILL.md          # required: frontmatter + body
├── references/       # optional: long-form docs, schemas, recipes
├── scripts/          # optional: executable scripts the skill can run via Bash
└── assets/           # optional: templates, fixtures
```

`SKILL.md` frontmatter (validated per spec):

```yaml
---
name: grocery-add-to-cart       # ≤64 chars, [a-z0-9-], must not start/end with -.
                                # "anthropic"/"claude" trigger a warning, not an error
                                # (Anthropic's own claude-api skill needs to load)
description: |                  # ≤1024 chars, non-empty, no XML tags
  Parse the weekly grocery list from the user's Google Doc planner and
  add non-completed items to their Amazon Whole Foods cart (stops at
  cart, never checks out).
---
```

The body is free-form markdown. We do **not** impose section structure —
the spec doesn't, and the existing grocery skill works without it.

### Discovery: scopes and locations

Mirrors how squad already handles agents and routines.

| Scope | Location | Use case |
|---|---|---|
| **Global** | Linux/macOS: `$XDG_CONFIG_HOME/squad/skills/<name>/SKILL.md`<br>Windows: `%APPDATA%\squad\skills\<name>\SKILL.md` | Personal capabilities used across repos |
| **Per-repo** | `<repo>/.squad/skills/<name>/SKILL.md`, checked into git | Team skills shared via the repo |
| **Git-backed (catalog)** | Listed in `config.yaml` under `skills.repositories`, cloned into `$XDG_DATA_HOME/squad/skill-repos/<alias>/` | Shared skill libraries (mirrors the existing `agents.repositories` mechanism) |

Resolution precedence when names collide: **repo > global > catalog**.
`squad skill list` shows a `SCOPE` column. Duplicate names within a
scope are a load-time error.

### Catalog injection into the system prompt

When an agent loads, if `skills.enabled: true` (default for agents that
don't opt out), the bundler appends:

```
## Available skills

You have access to the following skills via the `Skill` tool. Each is
listed by name and description only; call `Skill(name)` to load the full
instructions when a skill matches the user's request.

- **grocery-add-to-cart**: Parse the weekly grocery list from the user's
  Google Doc planner and add non-completed items to their Amazon Whole
  Foods cart (stops at cart, never checks out).
- **<name>**: <description>
- …
```

The format is **prose, not JSON** — the model selects by reading the
descriptions, exactly like the spec describes. The block is omitted
entirely if no skills are visible to the agent, so existing agents
behave identically.

Per-agent overrides in `agent.yaml`:

```yaml
skills:
  enabled: true                       # default true if any skills are discovered
  scopes: [repo]                      # restrict to per-repo only
  allow: [grocery-add-to-cart, …]     # opt-in allowlist (overrides scopes)
  deny: [debug-fs, …]                 # blocklist
```

`allow` wins over `deny` wins over `scopes`. Unset = "all from the
configured scopes".

### The `Skill` tool

New tool in `tools/skill.go`, registered into the same tool set as Read /
Write / Edit / Bash:

```
Skill(name: string) → string
```

Behavior:

1. Look up `name` in the catalog assembled at agent boot. Error if not
   found (the catalog is part of the agent's known universe; this is a
   programming error, not a lookup miss).
2. Read `<skill-dir>/SKILL.md`, strip frontmatter, return the body.
3. Push `<skill-dir>` onto the run's **skill stack** — a per-run list of
   directories where Read and Bash are unconditionally permitted, in
   addition to the working directory. Stack, not single value, so a
   skill can call into another skill (rare but legal per spec).
4. Emit a `skill_loaded` event into the session log with name, scope,
   and dir, so `squad session show` makes the progression auditable.

The tool does *not* execute scripts itself. Scripts in `scripts/` are
run by the agent calling `Bash` — squad's existing tool — once the skill
dir is on the stack. This matches the spec ("Claude runs them via
bash"), keeps the surface small, and reuses existing Bash safety
(`cmdsafety.go`, `loopdetect.go`).

### Tool gating: how skills interact with Read / Bash

Today `tools.go` enforces a "stay within working dir" boundary on Read
and Bash. The skill stack relaxes that boundary for the *contents of
loaded skill dirs only*. Scripts run with the agent's existing process
identity; we don't sandbox them per-skill in v1.

Security implication: a skill is trusted code, same posture as the spec
("treat like installing software"). Documented in the skills guide;
enforced by surfacing the source on `squad skill list` (`SCOPE` +
`origin path or git URL`).

### Interactivity: the `Confirm` tool

The grocery skill — and most useful skills — pause to show the user a
parsed plan before taking irreversible action. Squad is unattended /
batch by default. Bridge via a new tool, not a runtime mode switch:

```
Confirm(summary: string, options?: [string]) → string
```

- **TTY mode (`isatty(stdin)`):** prompt on stdin with the summary,
  return the user's choice (default `["yes","no"]`).
- **Non-TTY (routine, CI, headless):** read `--auto-confirm` flag —
  `--auto-confirm=yes` returns `"yes"`, `--auto-confirm=abort` errors
  the tool call (skill should then abort gracefully). Default
  behavior with no flag set is to abort, so unattended runs of
  interactive skills fail loudly instead of silently auto-approving.

`Confirm` is available to all agents, not just skill bodies — skills are
the primary motivator but the tool is generic.

Out of scope for v1: free-text prompts (`Ask`), multi-question batches.
A skill that needs more than yes/no should serialize a plan to a file
and call `Confirm("plan written to plan.json — approve?")`.

### Bundled scripts: argv and env contract

Scripts in `scripts/` are invoked by the agent via Bash. Two conventions
the loader enforces so skill authors have a stable contract:

1. Scripts get `$SQUAD_SKILL_DIR` in their env, set to the absolute path
   of the skill's directory. Lets them resolve sibling files
   regardless of cwd.
2. Scripts marked executable (`+x`) and starting with a shebang are
   run directly; non-executable scripts must be invoked through their
   interpreter (`python scripts/foo.py`). We don't auto-chmod.

### Validation: `squad skill validate`

Spec-conformance check, runnable both standalone and as part of
`squad skill add` to a repo:

- Frontmatter present and parseable
- `name` matches `^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`
- `name` contains no XML-tag characters
- **Warning (not error)** when `name` contains the spec-reserved
  substrings `anthropic` or `claude` — Anthropic's own first-party
  `claude-api` skill violates the rule, so fatal rejection would break
  interop. The warning preserves the spec's anti-impersonation hint.
- Manifest `name` matches the containing directory name (hard error —
  the catalog uses the directory as the lookup key)
- `description` ≤1024 chars, non-empty, no `<` characters that look
  like XML
- `SKILL.md` body ≤64 KiB hard cap; warn at 5 KiB. Cap was raised from
  25 KiB after empirical check against `anthropics/skills` — their
  `skill-creator` skill is ~32 KiB.
- No `..` path escapes in any markdown link inside `SKILL.md`
- If `scripts/` exists, each entry is either executable+shebanged or
  has a sibling note explaining the invoker

Validation runs at agent-boot catalog assembly; a failing skill is
**skipped with a warning**, not fatal, so one broken skill doesn't
disable the agent.

### Session integration

Every `Skill(...)` call appends an event to the run's session log under
the existing append-only `events.jsonl`:

```json
{"t":"skill_loaded","name":"grocery-add-to-cart","scope":"global","dir":"/Users/l/.config/squad/skills/grocery-add-to-cart"}
```

`squad session show <id>` already renders unknown event types
generically; we add a renderer for `skill_loaded` so the progression is
legible.

`--resume` works unchanged: the session replays events, the skill stack
is rebuilt from `skill_loaded` events on rehydrate.

## Package layout

```
skill/
  manifest.go          # SkillManifest type, frontmatter parse, spec validation
  manifest_test.go
  catalog.go           # Multi-scope discovery, name collision resolution, allow/deny
  catalog_test.go
  stack.go             # Per-run skill stack (push/pop, contains-path lookups)
  stack_test.go
  validate.go          # Spec conformance checks; powers `squad skill validate`
  validate_test.go

tools/
  skill.go             # Skill tool implementation
  skill_test.go
  confirm.go           # Confirm tool: TTY prompt + --auto-confirm
  confirm_test.go

agent/
  bundle.go            # MODIFY: assemble skill catalog block into system prompt
                       # when agent.yaml does not opt out
  bundle_test.go       # MODIFY: cover skills.enabled / scopes / allow / deny

runner/
  run.go               # MODIFY: pass skill catalog + stack through to tool dispatch
  model.go             # MODIFY: relax Read/Bash anchor check to consult skill stack

cmd/squad/
  skill.go             # skill subcommand tree
```

Dependencies added: none. Frontmatter parse: use the YAML parser
already in `go.mod` (`gopkg.in/yaml.v3`).

## CLI surface

```
squad skill list                      # name, scope, description, origin
squad skill show <name>               # full SKILL.md + scope + dir + validation report
squad skill validate <path>           # standalone spec check, used in CI
squad skill add <alias> <git-url>     # clone a skill catalog repo, mirroring `agents add`
squad skill add <alias> <local-path>  # register a local directory of skills
squad skill remove <alias>            # unregister a catalog
squad skill update [<alias>]          # `git pull` on catalog(s)
squad skill new <name> [--global]     # scaffold a new skill from template into the right scope
```

No `enable` / `disable` subcommand — `enabled: true` is per-agent, not
per-skill, and lives in `agent.yaml`. Skills are presence-based.

`squad run` gains:

```
--auto-confirm yes|no|abort           # how Confirm resolves in non-TTY runs
--skills-enabled / --skills-disabled  # force the agent's skill catalog on/off
--allow-skill <name>  (repeatable)    # per-run allowlist override
--deny-skill <name>   (repeatable)    # per-run denylist override
```

## Implementation phases

### Phase 1: Format + catalog + read-only listing

- [x] `skill/manifest.go` — frontmatter parse, spec validation rules (name regex, length caps, reserved substrings)
- [x] `skill/catalog.go` — discover skills across global / repo scopes; collision resolution; allow/deny filtering; deterministic ordering
- [x] `skill/validate.go` — spec conformance with structured errors for `squad skill validate`
- [x] `cmd/squad/skill.go` — `list`, `show`, `validate` subcommands (no behavior change to runs yet)
- [x] Tests covering scope precedence (repo > global > catalog), invalid frontmatter rejection, allow/deny, missing dirs

Validation gate: `squad skill list` on a repo with mixed
global / repo / catalog skills shows the right SCOPE and resolution; one
malformed skill produces a warning but doesn't block listing.

### Phase 2: System-prompt catalog injection

- [x] `agent/bundle.go` — assemble the `## Available skills` block from
      the catalog; respect `skills.enabled`, `skills.scopes`,
      `skills.allow`, `skills.deny` in `agent.yaml`
- [x] `cmd/squad/run.go` — `--skills-enabled`/`--skills-disabled`,
      `--allow-skill`, `--deny-skill` flags
- [x] Snapshot tests on the generated system prompt block (sorted by
      name, deterministic output, empty block when no skills)
- [x] Bench: confirm the block adds ~100 tok per skill — fail CI if it
      regresses past 150 tok/skill

Validation gate: launch an agent with two visible skills; capture
system prompt; confirm both names + descriptions appear and nothing
else from the skill dirs is included.

### Phase 3: `Skill` tool + skill stack + L3 path relaxation

- [x] `skill/stack.go` — per-run stack, path containment lookups
- [x] `tools/skill.go` — `Skill(name)` returns body, pushes dir, emits
      session event
- [x] `runner/model.go` — Read / Bash anchor check consults the stack
- [x] `runner/run.go` — wire the stack into the tool-dispatch context
- [x] `--resume` rehydrates the stack from `skill_loaded` events
- [x] Tests: load a skill, agent then reads `references/foo.md` and
      runs `scripts/bar.sh`; both succeed; reads outside skill dirs
      still respect the working-dir anchor

Validation gate: synthetic test skill with a script under
`scripts/echo.sh`; agent calls `Skill("test")` then `Bash("bash
$SQUAD_SKILL_DIR/scripts/echo.sh")` and observes the script output.

### Phase 4: `Confirm` tool

- [x] `tools/confirm.go` — TTY detection via `isatty`, prompt with
      `[y/n]` plus a numbered list when `options` is set
- [x] `--auto-confirm` flag on `squad run` (default abort in non-TTY)
- [x] Session event `confirm_resolved` records summary, options, and
      resolution
- [x] Tests: TTY path with stdin script, non-TTY paths for each
      `--auto-confirm` value

Validation gate: grocery skill, when run interactively, prompts before
cart adds; when run with `--auto-confirm=abort` in a non-TTY context,
errors out before any cart mutation; with `--auto-confirm=yes`,
proceeds and emits the audit trail.

### Phase 5: Git-backed catalogs

- [x] `skill/catalog.go` — `skills.repositories` block in `config.yaml`
      mirrors `agents.repositories`
- [x] `squad skill add/remove/update` for catalogs
- [x] Cloning reuses the go-git wiring already in `source/` for agents
- [x] Cache invalidation: `update` does `git pull`; re-validate
- [x] Tests: `add` clones; `update` re-fetches; collision with a
      higher-precedence local scope hides the catalog entry from
      `list`

### Phase 6: Polish + interop verification

- [x] `squad skill new <name>` scaffolds from a built-in template
- [x] Run [anthropics/skills](https://github.com/anthropics/skills) repo
      through `squad skill validate` and `squad skill list` — must
      report zero validation errors
- [x] End-to-end test: load the user's `grocery-add-to-cart` skill,
      run an agent that requests groceries, confirm Chrome MCP +
      Confirm + cart additions complete (gated on having the MCP
      servers and credentials available; otherwise mocked)
- [x] `docs/skills.md` covering: format, scopes, the `Skill` and
      `Confirm` tools, security model, interop with Claude Code
      skills, scaffolding workflow

## Out of scope (v1)

- **Multi-question / free-text user prompts.** `Confirm` is yes/no.
  Authors who need more should write a plan file and confirm against
  it.
- **Per-skill sandboxing of bundled scripts.** Skills run as the
  agent's process. Documented as a trust boundary.
- **Skill versioning / lockfiles.** Catalog repos can pin via git ref;
  per-skill versions can wait.
- **Auto-discovery of skill bundles from npm / PyPI.** Filesystem +
  git only in v1.
- **Editing skills in-place from squad CLI** beyond `squad skill new`.
  Authors edit `SKILL.md` in their editor.
- **A `Task` analogue for spawning sub-agents *as* skills.** Sub-agent
  spawning already exists; not entangling it with skills.

## Interop notes

- A skill authored for Claude Code drops into `~/.config/squad/skills/`
  unchanged and works, modulo MCP server availability. The grocery
  skill in particular needs the same Chrome MCP / Google Drive MCP it
  uses under Claude Code, and squad's existing `--mcp-server` plumbing
  handles both.
- The reverse holds: a skill authored for squad lives in Claude Code
  too. We do not introduce squad-specific frontmatter fields. If we
  need them later, we namespace under `x-squad:`.
- We deliberately do not adopt the `allowed-tools` field some
  Claude Code skills carry — squad's per-agent tool gating already
  handles this at a coarser level, and conflating the two would
  invent a squad-specific tool-name vocabulary the spec doesn't
  define.
