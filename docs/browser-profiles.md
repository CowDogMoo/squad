# Browser Profiles

Agents that drive a browser (via [chrome-devtools-mcp][cdt]) need an
authenticated browser session — most useful sites won't let the agent do
anything without a logged-in user. Squad solves this with **named
browser profiles**: long-lived Chromium user-data directories that you
sign into once and that the agent reuses on every subsequent run.

## TL;DR

```bash
# one-time: sign into Amazon in a managed profile
squad browser open amazon https://www.amazon.com/

# in agent.yaml, reference the profile by name
# (the template helper resolves to the absolute path on this machine)
mcp_servers:
  - name: chrome
    command: npx
    args:
      - chrome-devtools-mcp@latest
      - --userDataDir={{.BrowserProfile "amazon"}}

# the agent now uses your authenticated Amazon session on every run
squad run --agent grocery-runner
```

## What's actually happening

- `squad browser open <name>` creates (lazily) a Chromium user-data
  directory under `$XDG_DATA_HOME/squad/browser-profiles/<name>`
  (default `~/.local/share/squad/browser-profiles/<name>`) and launches
  Chrome against it in a foreground window.
- Whatever you do in that window — sign in, accept cookies, dismiss
  banners — Chrome persists to disk in that profile dir.
- The next time anything launches Chrome with the same `--user-data-dir`
  pointed at that directory, your session is back: cookies, autofill,
  even local storage.

That "anything" is your agent's `chrome-devtools-mcp` invocation. Squad
exposes the path as a template helper so `agent.yaml` doesn't have to
hardcode anything machine-specific.

## CLI reference

```text
squad browser open NAME [URL]   # create / open a profile in Chrome
                                #   --wait  block until Chrome quits

squad browser list              # all profiles + last-modified time
squad browser path NAME         # absolute path (creates if missing)
squad browser delete NAME       # remove permanently; needs --force
```

Profile names follow the same rules as squad skill names: lowercase
alphanumerics with `-` and `_`, no leading/trailing punctuation, no
dots, no slashes.

## The `{{.BrowserProfile "name"}}` template helper

Available wherever squad runs `text/template` on agent prompts: inline
`prompt:` blocks, `system.md`, and (most importantly here) string fields
inside `mcp_servers` entries — `command`, `args`, `env`, `headers`,
`url`.

Calling it lazily creates the profile dir if missing. The returned value
is always an absolute path on the local machine, which means agent.yaml
can be checked into git and stay portable: each machine has its own
profile dir at the same logical name.

```yaml
mcp_servers:
  - name: chrome
    command: npx
    args:
      - chrome-devtools-mcp@latest
      - --userDataDir={{.BrowserProfile "github-prod"}}
```

## Preflight check

Squad's MCP preflight (`mcp.PreflightServer`) recognizes
chrome-devtools-mcp and inspects the `--userDataDir` arg. When the
referenced dir exists but doesn't yet look like a Chrome profile
(missing `Default/Cookies` and `Local State` markers), the run logs a
warning *before* the model is invoked:

```
chrome MCP pre-flight: user-data-dir /Users/you/.local/share/squad/browser-profiles/amazon
exists but has no saved Chrome session (no Default/Cookies, no Local State).
Any site that requires login will block the agent. Sign in once by running:
  squad browser open <profile-name> <site-url>
```

The warning is informational, not fatal — squad still lets the run
proceed in case the agent's first action is something that doesn't need
login (e.g. a public page). But you've been told.

## Choosing between profile, --autoConnect, and shared Chrome

| You want… | Use |
|---|---|
| The agent to run inside a *separate* browser from your daily browsing (no risk of it accidentally reading your gmail) | A named profile. **Recommended.** |
| The agent to use your already-logged-in everyday Chrome | `--autoConnect` (requires Chrome 144+ with remote-debugging-permission granted via `chrome://inspect/#remote-debugging`, OR Chrome launched with `--remote-debugging-port=9222`). Less isolation, no setup beyond logging in once. |
| The agent to launch a brand-new Chrome with no saved state, every run | No `--userDataDir` and no `--autoConnect` — chrome-devtools-mcp default. Useful for tests; useless for anything cookie-gated. |

Named profiles are the recommended default for any production agent
because:

- **Isolation.** The agent's Chrome can't accidentally access an unrelated
  signed-in account from your personal browser.
- **Reproducibility.** Each agent gets a known starting state; no "wait,
  did I clear cookies in my main browser yesterday?" surprises.
- **Per-machine portability.** Same `agent.yaml` runs on every machine
  with no path edits; first run per machine prompts a one-time sign-in.

## Multiple agents sharing a profile

Two agents can point at the same profile — they'll share cookies and
storage. Useful when several agents drive the same site on behalf of
the same identity. Be aware that chrome-devtools-mcp launches one
browser per MCP process, and Chromium can lock a user-data-dir to a
single process. Don't run two `squad run` invocations against the same
profile simultaneously.

## Where the files live

```text
$XDG_DATA_HOME/squad/browser-profiles/    # default: ~/.local/share/squad/browser-profiles/
├── amazon/        ← one Chromium user-data-dir per name
│   ├── Default/
│   │   ├── Cookies
│   │   ├── History
│   │   └── ...
│   └── Local State
├── github-prod/
└── google/
```

These dirs are **not** managed by git or rsync'd between machines by
squad. Treat them like ~/Library/Application Support — a per-machine,
per-user data dir. If you back them up yourself, expect Chromium to
re-encrypt entries on first load on a different host.

[cdt]: https://github.com/ChromeDevTools/chrome-devtools-mcp
