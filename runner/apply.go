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
		logging.InfoContext(ctx, "edit deadline reached with no edits — treating as valid no-changes outcome")
		return nil
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
