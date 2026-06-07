# Routines

Schedule agents to run unattended. A routine pairs an agent invocation (agent + prompt + flags + working directory) with a cron expression and lets the per-user `squad routined` daemon fire it on schedule, without a terminal open.

Routines fit squad's batch / fire-and-iterate model: "review these repos every night at 2 AM," not "pair-program with me." Cost budgets, missed-fire catch-up, OS-supervised supervision, and session logs are all there for the same reason — nobody is watching when the run happens.

## Quick start

```bash
# Create a global routine — fires every day at 2 AM.
squad routine create nightly \
    --agent go-security-audit \
    --schedule "0 2 * * *" \
    --prompt "Audit pending changes" \
    --working-dir ~/code/api

# Or a per-repo routine, checked into git with the project.
cd ~/code/api
squad routine create audit \
    --agent go-security-audit \
    --schedule "@daily" \
    --scope repo

# Verify the daemon is installed and supervising the schedule.
squad routine doctor

# Show full details for a routine, including next fire and last run status.
squad routine show nightly

# Fire one manually, bypassing the schedule.
squad routine run-now nightly

# Tail the daemon log to watch fires in real time.
squad routine logs --follow
```

On the first `routine create`, squad installs itself as a per-user OS service so the daemon survives reboot and login. No admin privileges are required.

## Scopes

Routines live in one of two scopes:

| Scope | Location | When to use |
|---|---|---|
| Global | `~/.config/squad/routines/<id>.yaml` (XDG config home) | Personal, cross-repo automations |
| Per-repo | `<repo>/.squad/routines/<id>.yaml`, checked into git | Shareable team automations that travel with the project |

The scope is chosen automatically: `routine create` inside a directory that already contains `.squad/` defaults to repo scope; otherwise it falls back to global. Override with `--scope global|repo` and `--repo <path>`.

Per-repo routines need their containing repo to be in the daemon's **watched-roots registry** so the daemon picks them up. `routine create` adds the current repo automatically; manage manually with:

```bash
squad routine watch [<path>]    # add cwd or <path> to watched roots
squad routine unwatch <path>
squad routine roots             # list watched roots
```

## Routine IDs

User-supplied slugs: lowercase letters, digits, hyphens, must start with a letter and not end with a hyphen, max 64 characters. Regex: `^[a-z]([a-z0-9-]*[a-z0-9])?$`.

The same id may exist in both scopes (and in multiple watched repos) at once. Internally each routine is namespaced as `<scope>:<id>` — e.g. `global:nightly`, `repo:audit`. Commands accept either the bare id or the qualified form:

```bash
squad routine show nightly       # bare; resolves to a unique scope automatically
squad routine show repo:audit    # qualified; explicit
```

If a bare id is ambiguous across scopes, squad errors with the qualified options. Inside a watched repo, the repo-scoped match wins.

## Schedule syntax

The `schedule` field accepts:

- Standard 5-field cron: `0 2 * * *`, `*/15 * * * *`, `0 9-17 * * 1-5`
- Predefined macros: `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`
- Duration intervals: `@every 30m`, `@every 2h`, `@every 1h30m`

All forms are validated at create time. The parser is robfig/cron via go-co-op/gocron.

## Schema

The on-disk manifest:

```yaml
id: nightly-audit             # required, user-supplied slug
agent: go-security-audit      # required
schedule: "0 2 * * *"         # required, see schedule syntax above
prompt: "Audit pending changes since last run"
working_dir: ~/code/my-project  # required for global; defaults to repo root for per-repo
provider: anthropic           # optional, falls back to squad config default
model: claude-sonnet-4-6      # optional
max_cost: 5.00                # optional per-fire USD cap
max_iterations: 30            # optional per-fire iteration cap
vars:
  threshold: high             # template variables, same as --var KEY=VALUE
enabled: true
wake_system: false            # macOS/Windows only — wake the machine from sleep to fire
catchup: fire-once            # fire-once (default) | skip
created_at: 2026-05-12T18:00:00Z
```

Daemon-written status (`last_run`, `last_status`, `last_session_id`, `last_error`, `last_duration_ms`) lives in a sibling JSON file so the manifest stays clean for git:

- Global routines: `$XDG_STATE_HOME/squad/routines/<id>.state.json` (default `~/.local/state/...`)
- Per-repo routines: `<repo>/.squad/routines/.state/<id>.state.json` (gitignore the `.state/` directory)

## Catch-up policy

If the daemon is not running when a scheduled fire arrives — the laptop was off, asleep, or the daemon crashed — that fire is missed. On the next daemon start, every routine's catch-up policy decides what to do:

| Policy | Behavior |
|---|---|
| `fire-once` (default) | Queue exactly one immediate fire if at least one was missed since the last recorded run |
| `skip` | Do nothing; wait for the next regularly scheduled time |

Matches systemd `Persistent=true` semantics. Even after a week of downtime, you get at most one catch-up fire per routine — schedules don't pile up.

## OS service supervision

The daemon is installed per-user, with no admin rights, in the platform-native scheduler:

| OS | Mechanism | Path |
|---|---|---|
| macOS | launchd LaunchAgent | `~/Library/LaunchAgents/dev.cowdogmoo.squad.routined.plist` |
| Linux | systemd `--user` unit | `~/.config/systemd/user/squad-routined.service` |
| Windows | Task Scheduler per-user task | `\Squad\routined` |

`squad routine create` runs the install automatically the first time. `squad routine repair` re-runs it (useful after `go install` updates the squad binary). `squad routine doctor` reports the install state and paths.

### Linux — linger

For the daemon to keep running when no shell session is open, the user account needs to have linger enabled (`loginctl enable-linger $USER`). Squad attempts this automatically on install. If it fails — some hosting environments forbid it — squad warns but still installs; the daemon will then stop on logout until you fix linger.

### Windows — log path

On macOS and Linux, the OS service runtime handles daemon stdout/stderr (launchd's `StandardOutPath`, systemd's journald). Windows Task Scheduler has no equivalent, so the squad routined process redirects its own output to a log file passed via `--log-file` at install time. Default: `%LOCALAPPDATA%\squad\Logs\routined.log`.

### Wake from sleep

Off by default on all platforms. Setting `wake_system: true` on a routine is a hint for future support; full plumbing through launchd's `WakeSystem` and Windows Task Scheduler's `WakeToRun` is on the roadmap. Linux user services cannot wake the system. As of today, treat schedules as "fires when the machine is awake."

## Sessions and history

Each fire creates a normal squad session under `<working_dir>/.squad/sessions/<session-id>/` with `events.jsonl`, `meta.json`, and result spill files — exactly the same shape as an interactive `squad run`. The session's `meta.json` carries a `routine_id` field with the qualified form (`global:nightly`, `repo:audit`) so history queries can filter by exact provenance.

```bash
squad routine history nightly
# SESSION                    CREATED               STATUS     COST     ITER  NOTE
# 20260512T020000Z-abc12345  2026-05-12T02:00:00Z  completed  $0.0123  4     (last recorded fire)
```

The last-recorded fire row matches `state.last_session_id` from the state file. Open any session directly with `cat <working_dir>/.squad/sessions/<id>/meta.json` to see the full run.

## Observability

Every fire is wrapped in a `routine.fire` OpenTelemetry span carrying:

- `squad.routine.id`, `squad.routine.scope`, `squad.routine.qualified`
- `squad.routine.agent`, `squad.routine.schedule`
- `squad.routine.status` (`ok` / `failed` / `skipped` / `running`)
- `squad.routine.duration_ms`
- `squad.session.id` (when a session was created)

Export to your tracing backend with `--otel-endpoint`. The span is the parent of the agent's `agent.invoke` and `llm.call` spans, so a single trace shows the full fire → agent → model call hierarchy.

## CLI reference

```text
squad routine create <id>       Create a routine (auto-installs OS service)
squad routine list              List all routines across scopes
squad routine show <id>         Full details + state + next fire
squad routine delete <id>       Remove routine and its state file
squad routine enable <id>       Mark enabled
squad routine disable <id>      Mark disabled (daemon stops scheduling)
squad routine run-now <id>      Fire immediately, bypassing schedule
squad routine history <id>      List sessions for this routine
squad routine watch [<path>]    Add a repo root to the watched-roots registry
squad routine unwatch <path>    Remove from the registry
squad routine roots             List watched repo roots
squad routine logs [--follow]   Tail the daemon log
squad routine doctor            Report daemon health and paths
squad routine repair            Reinstall the OS service
```

## Known limitations

- **Wake-from-sleep is not implemented.** Routines fire only when the machine is awake. (Tracking item.)
- **One service binary per machine.** The OS service points at whichever `squad` binary called `routine create` (or `routine repair`) most recently; mixed installs of different squad versions are not supported.
- **Routines from different repos with the same slug are ambiguous on the CLI.** Use the qualified `repo:<id>` form, or move to unique slugs. Internal disambiguation is correct; this only matters at the typing layer.
- **No distributed coordination.** gocron supports it; squad does not wire it up yet. Two daemons running against the same routines on different machines would both fire.

## See also

- [Pipelines](pipelines.md) — multi-agent orchestration with dependency ordering
- [Observability](observability.md) — session logs, OTel tracing, cost budgeting
- [Configuration](configuration.md) — providers, environment variables, XDG paths
