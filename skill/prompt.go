package skill

import (
	"fmt"
	"sort"
	"strings"
)

// PromptBlockHeader is the markdown heading the system-prompt section opens
// with. Exported so tests and callers that want to detect the block can grep
// for it without hardcoding the exact wording.
const PromptBlockHeader = "## Available skills"

// RenderPromptBlock builds the Level-1 system-prompt section listing every
// visible skill the agent should know about. The block is intentionally
// prose — the model selects skills by reading the descriptions, not by
// pattern-matching structured fields.
//
// Returns the empty string when entries is empty so callers can append
// unconditionally:
//
//	if block := RenderPromptBlock(catalog); block != "" {
//	    sys.WriteString(block)
//	}
//
// The output is sorted by name so the block is stable across runs.
func RenderPromptBlock(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}

	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Name() < sorted[j].Name()
	})

	var sb strings.Builder
	sb.WriteString(PromptBlockHeader)
	sb.WriteString("\n\n")
	sb.WriteString("You have access to the following skills via the `Skill` tool. Each is listed by name and description only; call `Skill(name)` to load the full instructions when a skill matches the user's request.\n\n")
	for _, e := range sorted {
		fmt.Fprintf(&sb, "- **%s**: %s\n", e.Name(), oneLineDescription(e.Manifest.Description))
	}
	return sb.String()
}

// oneLineDescription collapses internal whitespace so multi-line YAML
// descriptions render cleanly as a single bullet.
func oneLineDescription(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
