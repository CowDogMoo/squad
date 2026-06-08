package agent

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// RequiresConfig declares external dependencies an agent needs at runtime.
// The runner verifies these before invoking the model so a missing tool
// fails fast with an actionable install hint instead of mid-run.
type RequiresConfig struct {
	// Commands lists CLI binaries that must be on PATH for the agent
	// to function. The runner uses exec.LookPath to verify each.
	Commands []RequiredCommand `yaml:"commands,omitempty"`
}

// RequiredCommand names a single CLI binary and how to install it.
type RequiredCommand struct {
	// Name is the binary name as it would be invoked (e.g. "gosec").
	Name string `yaml:"name"`

	// Install maps a package manager (or "url") to the install identifier.
	// Common keys: brew, pipx, pip, npm, cargo, go, apt, dnf, pacman, url.
	// Values are passed verbatim to the user as install hints; the runner
	// does not execute them. An empty map is allowed but unhelpful.
	Install map[string]string `yaml:"install,omitempty"`
}

// Validate checks that the Requires block is structurally well-formed.
func (r *RequiresConfig) Validate(manifestName string) error {
	if r == nil {
		return nil
	}
	seen := make(map[string]bool, len(r.Commands))
	for i, c := range r.Commands {
		if c.Name == "" {
			return fmt.Errorf("agent %q: requires.commands[%d]: name is required", manifestName, i)
		}
		if strings.ContainsAny(c.Name, " \t/") {
			return fmt.Errorf("agent %q: requires.commands[%d]: name %q must be a bare binary name, not a path", manifestName, i, c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("agent %q: requires.commands: duplicate command %q", manifestName, c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// Preflight verifies every required command is on PATH. Returns nil when
// all are present (or when r is nil/empty). When commands are missing it
// returns a single error listing each missing tool with its install hints.
func (r *RequiresConfig) Preflight() error {
	if r == nil || len(r.Commands) == 0 {
		return nil
	}
	var missing []RequiredCommand
	for _, c := range r.Commands {
		if _, err := exec.LookPath(c.Name); err != nil {
			missing = append(missing, c)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%s", formatMissing(missing))
}

func formatMissing(missing []RequiredCommand) string {
	var b strings.Builder
	plural := "tool"
	if len(missing) > 1 {
		plural = "tools"
	}
	fmt.Fprintf(&b, "preflight: %d required %s missing:\n", len(missing), plural)
	for _, c := range missing {
		fmt.Fprintf(&b, "\n  %s\n", c.Name)
		for _, line := range formatInstall(c.Install) {
			fmt.Fprintf(&b, "    %s\n", line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatInstall renders an Install map as a stable, ordered list of hint
// lines. The first hint is prefixed "Install:"; subsequent hints align as
// "     or:" so the output reads like the example in docs/creating-agents.md.
func formatInstall(install map[string]string) []string {
	if len(install) == 0 {
		return []string{"Install: (no hint provided)"}
	}
	keys := make([]string, 0, len(install))
	for k := range install {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return installPriority(keys[i]) < installPriority(keys[j])
	})

	lines := make([]string, 0, len(keys))
	for i, k := range keys {
		hint := renderInstallHint(k, install[k])
		if i == 0 {
			lines = append(lines, "Install: "+hint)
		} else {
			lines = append(lines, "     or: "+hint)
		}
	}
	return lines
}

// installPriority orders well-known package managers ahead of less common
// ones so the most actionable hint appears first. Unknown keys sort
// alphabetically after the known set.
func installPriority(key string) int {
	switch key {
	case "brew":
		return 0
	case "pipx":
		return 1
	case "pip":
		return 2
	case "npm":
		return 3
	case "cargo":
		return 4
	case "go":
		return 5
	case "apt":
		return 6
	case "dnf":
		return 7
	case "pacman":
		return 8
	case "url":
		return 100
	default:
		return 50
	}
}

func renderInstallHint(manager, value string) string {
	if manager == "url" {
		return value
	}
	if value == "" {
		return manager + " (no package name provided)"
	}
	switch manager {
	case "brew":
		return "brew install " + value
	case "pipx":
		return "pipx install " + value
	case "pip":
		return "pip install " + value
	case "npm":
		return "npm install -g " + value
	case "cargo":
		return "cargo install " + value
	case "go":
		return "go install " + value
	case "apt":
		return "sudo apt install " + value
	case "dnf":
		return "sudo dnf install " + value
	case "pacman":
		return "sudo pacman -S " + value
	default:
		return manager + ": " + value
	}
}
