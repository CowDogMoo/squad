package runner

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/tools"
)

// applyResponseDiff extracts and applies a unified diff from the model response.

func applyResponseDiff(ctx context.Context, response, workingDir string, fallback bool) error {
	diff, err := extractUnifiedDiff(response)
	if err != nil {
		if responseIndicatesNoChanges(response) {
			logging.InfoContext(ctx, "no changes reported; skipping apply")
			return nil
		}
		if tools.EditsApplied(ctx) {
			logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
			return nil
		}
		return err
	}
	if tools.EditsApplied(ctx) {
		logging.InfoContext(ctx, "edits already applied via tools; skipping diff apply")
		return nil
	}
	logging.InfoContext(ctx, "applying diff (%d bytes)", len(diff))
	if err := applyUnifiedDiff(ctx, workingDir, diff, fallback); err != nil {
		return err
	}
	logging.InfoContext(ctx, "diff applied to %s", workingDir)
	return nil
}

func validateActionableResponse(ctx context.Context, response string) error {
	if tools.EditsApplied(ctx) {
		return nil
	}
	if tools.EditDeadlineReached(ctx) {
		return fmt.Errorf("edit deadline reached: agent exhausted its read budget without producing any edits")
	}
	if _, err := extractUnifiedDiff(response); err == nil {
		return nil
	}
	lower := strings.ToLower(response)
	if strings.Contains(lower, "files touched") || strings.Contains(lower, "no changes") {
		return nil
	}
	return fmt.Errorf("response is not actionable: missing diff, files touched, or no changes section (disable with --require-actionable=false)")
}

func responseIndicatesNoChanges(response string) bool {
	return strings.Contains(strings.ToLower(response), "no changes")
}

// claimsSpecificFilesTouched reports whether the response contains a
// "files touched:" line that names at least one real file — i.e. anything
// other than "none". A genuine no-changes report says "files touched: none";
// a fabricated edit report names files it never wrote.
//
// This is deliberately independent of any "no changes" marker. A report that
// both names a specific file AND says "no changes" is self-contradictory, and
// that contradiction is itself a fabrication signature (observed from local
// models that describe a rewrite in prose without ever calling an edit tool);
// it must be cross-checked against the working tree rather than waved through.
func claimsSpecificFilesTouched(response string) bool {
	lines := strings.Split(response, "\n")
	for i, line := range lines {
		idx := strings.Index(strings.ToLower(line), "files touched")
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len("files touched"):])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
		// The value may sit on the next line (e.g. a bullet list under the label).
		if rest == "" && i+1 < len(lines) {
			rest = strings.TrimSpace(lines[i+1])
		}
		// Strip surrounding markdown emphasis / list punctuation.
		rest = strings.Trim(rest, " *_`-.|")
		if rest == "" || strings.EqualFold(rest, "none") {
			continue
		}
		return true
	}
	return false
}

// validateActionableChanges guards against fabricated "changes made" reports.
// validateActionableResponse accepts a report that merely contains the text
// "files touched", which a local model can emit without ever calling an edit
// tool. When such a report claims files were touched but nothing actually
// changed — no tool edit, no unified diff, and a clean git working tree — the
// run is treated as a fabrication and fails. It is only meaningful for
// edit-mode runs inside a git repo; every other case passes through.
func validateActionableChanges(ctx context.Context, response, workingDir string) error {
	// A real tool edit or a unified diff is authoritative actionable output.
	if tools.EditsApplied(ctx) {
		return nil
	}
	if _, err := extractUnifiedDiff(response); err == nil {
		return nil
	}
	// Only a claim that specific files were touched needs cross-checking. A
	// genuine "files touched: none" / no-changes report passes. A stray "no
	// changes" marker does NOT exempt a report that also names specific files —
	// that self-contradiction is exactly how some local models fabricate edits.
	if !claimsSpecificFilesTouched(response) {
		return nil
	}
	// Changes can only be verified inside a git repo; don't penalize others.
	if !isGitRepo(ctx, workingDir) {
		return nil
	}
	// If git can't report (error) or the tree actually changed, the report is
	// trustworthy; only a confirmed-clean tree is a fabrication.
	if dirty, err := workingTreeDirty(ctx, workingDir); err != nil || dirty {
		return nil
	}
	return fmt.Errorf("report claims \"files touched\" but the working tree is unchanged: the agent reported edits it never applied (disable with --require-actionable=false)")
}

func extractUnifiedDiff(response string) (string, error) {
	const fence = "```diff"
	const altFence = "```patch"
	const endFence = "```"
	var blocks []string
	for {
		start := strings.Index(response, fence)
		useFence := fence
		if start == -1 {
			start = strings.Index(response, altFence)
			useFence = altFence
		}
		if start == -1 {
			break
		}
		response = response[start+len(useFence):]
		end := strings.Index(response, endFence)
		if end == -1 {
			block := strings.TrimSpace(response)
			if block != "" {
				blocks = append(blocks, block)
			}
			break
		}
		block := strings.TrimSpace(response[:end])
		if block != "" {
			blocks = append(blocks, block)
		}
		response = response[end+len(endFence):]
	}
	if len(blocks) == 0 {
		return "", fmt.Errorf("apply requires a unified diff block (```diff ... ```)")
	}
	diff := strings.Join(blocks, "\n")
	if !looksLikeDiff(diff) {
		return "", fmt.Errorf("diff block does not look like a unified diff (missing diff headers)")
	}
	return diff, nil
}

func applyUnifiedDiff(ctx context.Context, workingDir, diff string, applyFallback bool) error {
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("diff content is empty")
	}

	gitErr := applyWithGit(ctx, workingDir, diff)
	if gitErr == nil {
		return nil
	}

	if !applyFallback {
		return fmt.Errorf("failed to apply diff with git: %v (hint: retry with --apply-fallback)", gitErr)
	}

	patchErr := applyWithPatch(ctx, workingDir, diff)
	if patchErr == nil {
		return nil
	}

	return fmt.Errorf("failed to apply diff with git or patch: %v; %v", gitErr, patchErr)
}

func looksLikeDiff(diff string) bool {
	if strings.Contains(diff, "diff --git ") {
		return true
	}
	if strings.Contains(diff, "--- a/") && strings.Contains(diff, "+++ b/") {
		return true
	}
	return false
}

func applyWithGit(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "--recount", "-")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git apply failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func applyWithPatch(ctx context.Context, workingDir, diff string) error {
	cmd := exec.CommandContext(ctx, "patch", "-p1")
	cmd.Dir = workingDir
	cmd.Stdin = strings.NewReader(diff)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("patch failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
