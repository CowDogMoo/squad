# Routines: Scheduled Unattended Agent Runs

## Goal

Add first-class scheduled execution to squad. A *routine* is a saved
agent invocation + cron schedule that fires automatically without the
user keeping a terminal open. Fits squad's fire-and-iterate model:
"review these N repos every night at 2 AM" is exactly the shape squad is
built for, the missing piece is the trigger.

```
squad routine create nightly-audit \
    --agent go-security-audit \
    --schedule "0 2 * * *" \
    --working-dir ~/code/api \
    --prompt "Audit pending changes since last run"

squad routine list
squad routine history nightly-audit
squad routine run-now nightly-audit
squad routine delete nightly-audit
```

## Design

### Execution model: single auto-installed user-level background agent

One long-lived daemon process (`squad routined`) holds a gocron
scheduler in memory and fires routines as their schedules come due.
On first `squad routine create`, squad installs an OS-native user-level
service whose only job is to run `squad routined`. The OS supervises
the daemon (restart on crash, start on login); gocron supervises the
individual jobs.

| OS | Service mechanism | Install location |
|---|---|---|
| macOS | launchd LaunchAgent | `~/Library/LaunchAgents/io.dreadnode.squad.routined.plist` |
| Linux | systemd `--user` unit | `~/.config/systemd/user/squad-routined.service` (+ `loginctl enable-linger`) |
| Windows | Task Scheduler per-user task | Registered via `Register-ScheduledTask`, `AtLogOn` trigger |

Rejected alternatives:

- **One OS scheduled entry per routine**: triples platform-specific code, every CRUD op mutates user's OS state.
- **Foreground daemon user-managed**: relies on user discipline, defeats the "squad handles it" goal.
- **Windows Service**: needs admin, runs in session 0, can't see user's git config / `%APPDATA%`. Wrong scope for a dev tool.

### Routine manifest

YAML, one file per routine. Two valid locations:

| Scope | Location | Use case |
|---|---|---|
| **Global (user-level)** | Linux/macOS: `$XDG_CONFIG_HOME/squad/routines/<id>.yaml` (default `~/.config/squad/routines/`)<br>Windows: `%APPDATA%\squad\routines\<id>.yaml` | Personal cross-repo automations — "audit all my Go repos at 2 AM" |
| **Per-repo (project-level)** | `<repo>/.squad/routines/<id>.yaml`, checked into git | Shareable team automations — "every PR-merged branch gets a security scan nightly" |

Both formats are identical. The only difference is location and how the
daemon discovers them (see Discovery below).

```yaml
id: nightly-audit                 # user-supplied slug, validated (see ID rules)
agent: go-security-audit
schedule: "0 2 * * *"             # standard 5-field cron, or "@every 30m"
prompt: "Audit pending changes since last run"
working_dir: /Users/l/code/api    # global routines: required absolute path
                                  # per-repo routines: optional, defaults to the repo root
provider: anthropic               # optional, falls back to default config
model: claude-sonnet-4-6          # optional
max_cost: 5.00                    # optional
max_iterations: 30                # optional
vars:                             # optional, passed as --var key=val
  threshold: high
enabled: true
wake_system: false                # opt-in: wake mac/win from sleep to fire
catchup: fire-once                # fire-once (default) | skip
```

**Status state (`last_run`, `last_status`, `last_session_id`)** is *not*
stored in the manifest. Manifests are user-authored / checked into git;
mixing daemon-written state into them creates merge conflicts. Status
lives in a sibling state file the daemon owns:

- Global routines: `$XDG_STATE_HOME/squad/routines/<id>.state.json`
- Per-repo routines: `<repo>/.squad/routines/.state/<id>.state.json` (the `.state/` dir is `.gitignore`d)

### ID validation rules

User-supplied slug. Validated on `routine create`:

- Pattern: `^[a-z][a-z0-9-]{0,62}[a-z0-9]$` (or single `[a-z]` for length-1)
- Lowercase alphanumeric + hyphens, must start with a letter, must not start or end with hyphen, max 64 chars
- Uniqueness: enforced *within a scope*. A per-repo `nightly` and a global `nightly` can coexist; the daemon namespaces internally as `<scope>:<id>`.

### Discovery

The daemon needs to know which directories to watch. One global config
file lists them:

`$XDG_CONFIG_HOME/squad/routine-roots.yaml` (Windows: `%APPDATA%\squad\routine-roots.yaml`)

```yaml
roots:
  - /Users/l/code/api            # per-repo routines under <root>/.squad/routines/
  - /Users/l/code/infra
```

Global routines are always watched (single fixed path, no config needed).

CLI to manage roots:

```
squad routine watch [<path>]      # add cwd or <path> to roots; auto-runs on `routine create`
                                  # inside a repo that isn't yet watched
squad routine unwatch <path>
squad routine roots               # list watched roots
```

When the user runs `squad routine create <id>` inside a directory
containing `.squad/`, default scope = per-repo and auto-add to roots if
not already watched. Outside such a directory, default scope = global.
Override via `--scope global|repo` and `--repo <path>`.

### Routine addressing on the CLI

For commands that take an `<id>` (`show`, `delete`, `enable`, `run-now`,
`history`), resolution order:

1. Exact match against `<scope>:<id>` (e.g. `repo:nightly`, `global:nightly`)
2. Bare `<id>` resolves uniquely if only one scope has it; otherwise error with the qualified options
3. Inside a watched repo, bare `<id>` prefers the per-repo match

`squad routine list` shows a `SCOPE` column (`global` / `repo:<basename>`).

### Catch-up policy for missed fires

Default: **fire once on next daemon start if any scheduled fire was
missed since `last_run`.** Matches systemd `Persistent=true` UX, doesn't
blow up costs if the laptop was off for a week. Never catches up more
than one instance per routine. Opt-out via `catchup: skip` per routine.

### Wake-from-sleep

Off by default on all platforms. Opt-in per routine via `wake_system:
true`. Implemented as `WakeSystem` in the macOS plist (per-task is
tricky — likely promote the daemon-level plist to `WakeSystem` if *any*
routine sets it) and `WakeToRun` on the Windows task. Not supported on
Linux (would need root + RTC wake); document as known limitation.

### Concurrency

- Per-routine: gocron singleton mode — if previous fire still running, skip the new one and write `status: skipped` to the routine's state file.
- Scheduler-wide: configurable `max_concurrent_routines` (default 2) to cap simultaneous agent runs and protect cost budgets.

### Session integration

Each routine fire creates a normal session under `<working_dir>/.squad/sessions/<id>/` and the daemon adds `routine_id` (qualified `<scope>:<id>`) to `meta.json`. `squad routine history <id>` is a filter over existing session storage; we don't invent a parallel store.

## Package layout

```
routine/
  manifest.go          # Routine type, YAML load/save, ID + schedule validation
  manifest_test.go
  state.go             # State file (last_run, last_status, last_session_id) read/write
  state_test.go
  scope.go             # Scope (global | repo) + qualified-id resolution, addressing logic
  scope_test.go
  roots.go             # routine-roots.yaml load/save, watch/unwatch
  roots_test.go
  storage.go           # Multi-root dir walk + fsnotify watcher (one watcher per root + global)
  storage_test.go
  scheduler.go         # gocron wiring, fire handler
  scheduler_test.go
  catchup.go           # Missed-fires reconciliation on daemon start
  catchup_test.go
  service/
    service.go         # Interface: Install, Uninstall, Status, Path
    service_darwin.go  # launchd plist template + launchctl bootstrap/bootout
    service_linux.go   # systemd --user unit + systemctl + linger
    service_windows.go # Register-ScheduledTask via powershell.exe -NoProfile
    service_test.go    # Cross-platform unit tests where possible

cmd/squad/
  routine.go           # routine subcommand tree
  routined.go          # hidden daemon entrypoint
```

Dependencies added:

- `github.com/go-co-op/gocron/v2` — scheduler
- `github.com/fsnotify/fsnotify` — manifest dir watcher (already commonly in Go deps; verify)

Rejected: `github.com/kardianos/service` — targets system services, we want user-level on all three platforms.

## CLI surface

```
squad routine create <id>      # flags: --agent, --schedule, --prompt, --working-dir,
                                #        --provider, --model, --max-cost, --var, --disabled,
                                #        --scope global|repo, --repo <path>
                                # Default scope: repo if cwd contains .squad/, else global
squad routine list             # scope, id, schedule, agent, next-fire, last-status
squad routine show <id>        # full manifest + state + computed next-fire times
squad routine delete <id>
squad routine enable <id>
squad routine disable <id>
squad routine run-now <id>     # manual trigger, bypasses schedule
squad routine history <id>     # list sessions for this routine
squad routine watch [<path>]   # add cwd or <path> to watched repo roots
squad routine unwatch <path>
squad routine roots            # list watched repo roots
squad routine logs [--follow]  # tail daemon log
squad routine doctor           # verify daemon installed + running per-platform
squad routine repair           # reinstall service if doctor reports problems
squad routined                 # hidden: daemon entrypoint
```

`<id>` accepts bare slug (resolved by addressing rules above) or
qualified `<scope>:<id>` (e.g. `repo:nightly`, `global:nightly`).

`squad routine create` auto-installs the OS service if missing, and
auto-runs `routine watch` for the current repo when a per-repo routine
is created in a not-yet-watched directory. First run prints what was
installed and where, so users know what's on their system.

## Windows-specific decisions

1. **Separate GUI-subsystem binary** `squad-routined.exe` built with `-ldflags "-H=windowsgui"` to suppress console flash on every fire. Add to `.goreleaser.yaml` matrix.
2. **PowerShell over `schtasks.exe`** for install/uninstall — `Register-ScheduledTask`, `Get-ScheduledTask`, `Unregister-ScheduledTask`. Shell out via `powershell.exe -NoProfile -Command`.
3. **Path quoting** on all exec paths in plists / unit files / task XML — homes-with-spaces is in scope from v1.
4. **Logs** at platform-conventional locations, size-rotated (10 MB × 5 files):
   - macOS: `~/Library/Logs/squad/routined.log`
   - Linux: `$XDG_STATE_HOME/squad/routined.log`
   - Windows: `%LOCALAPPDATA%\squad\Logs\routined.log`

## Implementation phases

### Phase 1: Manifest + storage + scheduler core (cross-platform)

- [ ] `routine/manifest.go` — Routine struct, YAML marshal, validation (slug regex, schedule parse, agent exists, working_dir resolution)
- [ ] `routine/state.go` — separate state file read/write (last_run/last_status/last_session_id), atomic writes
- [ ] `routine/scope.go` — Scope enum (global | repo), qualified-id parsing, addressing resolution (`<id>` → unique scope:id), `repo:<basename>` display
- [ ] `routine/roots.go` — `routine-roots.yaml` load/save, watch/unwatch ops, in-repo cwd detection
- [ ] `routine/storage.go` — multi-root list/load/save/delete; one fsnotify watcher for the global dir plus one per registered repo root's `.squad/routines/`
- [ ] `routine/scheduler.go` — gocron setup, fire handler that calls into existing `runner.ExecuteRun()` / composed path; per-routine singleton mode; tag session `routine_id` with qualified scope:id
- [ ] `routine/catchup.go` — on daemon start, for each routine compute most recent missed fire from state file `last_run` and current time; queue one immediate fire if missed (unless `catchup: skip`)
- [ ] `cmd/squad/routine.go` — CLI subtree (create/list/show/delete/enable/disable/run-now/history/watch/unwatch/roots)
- [ ] `cmd/squad/routined.go` — daemon entrypoint (foreground; no service install yet)
- [ ] Tests with `clockwork.FakeClock` for time-sensitive logic; per-repo + global mixed-scope addressing tests

Validation gate: `squad routined` in a terminal fires both global and per-repo routines on schedule, reloads on manifest add/edit/delete in any watched root, and resolves bare-id addressing correctly when both scopes have the same slug.

### Phase 2: macOS launchd service install

- [ ] `routine/service/service.go` — interface
- [ ] `routine/service/service_darwin.go` — plist template, `launchctl bootstrap gui/<uid>` install, `launchctl bootout` uninstall, `launchctl print` for status
- [ ] `routine create` auto-installs if not installed
- [ ] `routine doctor` reports plist path + daemon PID + last fire
- [ ] `routine repair` reinstalls

### Phase 3: Linux systemd --user service install

- [ ] `routine/service/service_linux.go` — unit template, `systemctl --user enable --now`, `loginctl enable-linger`, error surface if linger fails
- [ ] Same doctor/repair UX
- [ ] Document linger requirement and what to do if user is on a system without it

### Phase 4: Windows Task Scheduler install + GUI-subsystem binary

- [ ] `.goreleaser.yaml` — add `squad-routined` Windows GUI-subsystem build
- [ ] `routine/service/service_windows.go` — PowerShell `Register-ScheduledTask` install with `AtLogOn` trigger, restart-on-failure, `RunLevel Limited`; `Unregister-ScheduledTask` uninstall; `Get-ScheduledTask` for status
- [ ] Path-with-spaces test on Windows runner in CI

### Phase 5: Polish

- [ ] `routine logs --follow` over rotated log files
- [ ] `wake_system` plumbed through to platform-specific install code (macOS + Windows only)
- [ ] `max_concurrent_routines` scheduler-wide cap
- [ ] OpenTelemetry spans on routine fires (reuse existing run instrumentation, add `routine.id` attribute)
- [ ] Docs: `docs/routines.md` with platform-specific gotchas (linger, wake limitations, log paths)

## Out of scope (v1)

- Distributed / multi-machine routines (gocron supports it, we don't need it yet)
- Webhook triggers (only cron + duration for now)
- Notification on failure (email/Slack) — defer until users ask
- Pause-all / resume-all (just toggle individual routines)
- Routine import/export between machines (manifests are already YAML, users can copy them)
