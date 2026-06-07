// Confirm tool — interactive yes/no checkpoint for an agent mid-run.
//
// Anthropic Agent Skills (and most useful agent workflows) need to pause
// before taking an irreversible action — adding items to a cart, sending
// an email, deleting files. The Confirm tool lets the model ask a human
// "is this okay?" without baking interactivity into every tool.
//
// Behavior depends on whether the run is attached to a terminal:
//
//   - TTY: print the summary (and numbered options) to Out, read a line
//     from In, match it to an option (prefix / number / exact match),
//     return the chosen option.
//
//   - Non-TTY (routines, CI, headless agents): consult AutoConfirm:
//       yes   → return the first option (typically "yes")
//       no    → return the second option (typically "no"); error if absent
//       abort → return an error so the skill aborts gracefully
//     The default when AutoConfirm is unset is to abort, matching PLAN.md:
//     "unattended runs of interactive skills fail loudly instead of
//     silently auto-approving."

package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/session"
	"github.com/tmc/langchaingo/llms"
)

// AutoConfirmMode controls how Confirm resolves when no TTY is attached.
type AutoConfirmMode string

// Recognised --auto-confirm values. AutoConfirmUnset is treated the same as
// AutoConfirmAbort but kept distinct so logs can show that no flag was set.
const (
	AutoConfirmUnset AutoConfirmMode = ""
	AutoConfirmYes   AutoConfirmMode = "yes"
	AutoConfirmNo    AutoConfirmMode = "no"
	AutoConfirmAbort AutoConfirmMode = "abort"
)

// IsValid reports whether m is one of the documented values. An empty mode
// (unset) is considered valid — the runner uses it as the "no flag given"
// signal.
func (m AutoConfirmMode) IsValid() bool {
	switch m {
	case AutoConfirmUnset, AutoConfirmYes, AutoConfirmNo, AutoConfirmAbort:
		return true
	}
	return false
}

// ConfirmRuntime is the per-run state the Confirm tool needs. A nil runtime
// is treated as if AutoConfirm=abort and no TTY — Confirm calls fail
// safely. This keeps tests, pipelines, and routines deterministic.
type ConfirmRuntime struct {
	// In is the source of user input. Typically os.Stdin.
	In io.Reader
	// Out is where the prompt is written. Typically os.Stderr because
	// stdout is often captured for the agent's final response.
	Out io.Writer
	// IsTTY, when non-nil, decides whether to take the interactive path.
	// A nil IsTTY is treated as "not a TTY" so non-TTY policy applies.
	IsTTY func() bool
	// AutoConfirm is the resolution policy for non-TTY runs.
	AutoConfirm AutoConfirmMode
}

// defaultOptions are the options used when the agent omits the field. The
// order matters: index 0 is the "yes" choice that AutoConfirmYes selects.
var defaultOptions = []string{"yes", "no"}

type confirmRuntimeKey struct{}

// WithConfirmRuntime attaches r to ctx so the Confirm handler can find it.
func WithConfirmRuntime(ctx context.Context, r *ConfirmRuntime) context.Context {
	if r == nil {
		return ctx
	}
	return context.WithValue(ctx, confirmRuntimeKey{}, r)
}

// GetConfirmRuntime returns the runtime stored on ctx, or nil if absent.
func GetConfirmRuntime(ctx context.Context) *ConfirmRuntime {
	r, _ := ctx.Value(confirmRuntimeKey{}).(*ConfirmRuntime)
	return r
}

func definitionConfirm() llms.Tool {
	return llms.Tool{
		Type: "function",
		Function: &llms.FunctionDefinition{
			Name: "Confirm",
			Description: "Pause for a yes/no checkpoint before taking an irreversible action. " +
				"The runtime resolves the prompt against an interactive user (TTY) or the run's " +
				"--auto-confirm policy. Returns the chosen option string or errors when the run " +
				"is non-interactive and not auto-confirmed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{
						"type":        "string",
						"description": "One-sentence description of the decision the user is being asked to make.",
					},
					"options": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional choices; defaults to [\"yes\",\"no\"]. Order matters — index 0 is the affirmative.",
					},
				},
				"required": []string{"summary"},
			},
		},
	}
}

type confirmArgs struct {
	Summary string   `json:"summary"`
	Options []string `json:"options,omitempty"`
}

// confirmTool returns the Handler.Call for the Confirm tool. The runtime is
// closed over so tests can inject their own In/Out/IsTTY without touching
// process-level globals.
func confirmTool(runtime *ConfirmRuntime) func(ctx context.Context, rawArgs []byte) (string, error) {
	return func(ctx context.Context, rawArgs []byte) (string, error) {
		var args confirmArgs
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", fmt.Errorf("parse Confirm args: %w", err)
		}
		args.Summary = strings.TrimSpace(args.Summary)
		if args.Summary == "" {
			return "", errors.New("confirm: summary is required")
		}
		options := args.Options
		if len(options) == 0 {
			options = defaultOptions
		}
		// Reject empty option strings — they'd never be selectable.
		for i, o := range options {
			if strings.TrimSpace(o) == "" {
				return "", fmt.Errorf("confirm: option %d is empty", i)
			}
		}

		resolution, via, err := resolveConfirm(ctx, runtime, args.Summary, options)
		// Emit the session event regardless of outcome so the audit trail
		// records both approvals and aborts.
		logConfirmResolution(ctx, args.Summary, options, resolution, via, err)
		if err != nil {
			return "", err
		}
		return resolution, nil
	}
}

// resolveConfirm picks the resolution path based on the runtime. The bool
// "via" string is used for the audit event.
func resolveConfirm(ctx context.Context, runtime *ConfirmRuntime, summary string, options []string) (resolution, via string, err error) {
	if runtime != nil && runtime.IsTTY != nil && runtime.IsTTY() {
		choice, terr := promptTTY(runtime, summary, options)
		if terr != nil {
			return "", "tty", terr
		}
		return choice, "tty", nil
	}
	return resolveAutoConfirm(runtime, options)
}

// resolveAutoConfirm handles the non-TTY path. Each branch returns the
// option string that should be sent back to the model.
func resolveAutoConfirm(runtime *ConfirmRuntime, options []string) (resolution, via string, err error) {
	mode := AutoConfirmUnset
	if runtime != nil {
		mode = runtime.AutoConfirm
	}
	switch mode {
	case AutoConfirmYes:
		return options[0], "auto-confirm=yes", nil
	case AutoConfirmNo:
		if len(options) < 2 {
			return "", "auto-confirm=no", errors.New("confirm: --auto-confirm=no requires at least two options")
		}
		return options[1], "auto-confirm=no", nil
	case AutoConfirmAbort, AutoConfirmUnset:
		return "", string("auto-confirm=" + nonEmptyOrDefault(string(mode), "abort")),
			errors.New("confirm: non-interactive run without --auto-confirm; aborting")
	default:
		return "", "auto-confirm=" + string(mode),
			fmt.Errorf("confirm: invalid --auto-confirm mode %q", mode)
	}
}

// nonEmptyOrDefault is a tiny helper that keeps the "via" label informative
// for the unset case (records "auto-confirm=abort" rather than "auto-confirm=").
func nonEmptyOrDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// promptTTY writes the summary and options to runtime.Out, reads a single
// response line from runtime.In, and matches it against the options.
// Matching is case-insensitive and accepts (in order): a 1-based index,
// an exact option string, or a non-empty prefix of exactly one option.
//
// The prompt is composed into a single buffer and written with one call
// so a partial write surfaces as one clear error instead of silently
// leaving the user looking at a half-rendered prompt.
func promptTTY(runtime *ConfirmRuntime, summary string, options []string) (string, error) {
	if runtime.In == nil {
		return "", errors.New("confirm: no stdin available for interactive prompt")
	}
	if runtime.Out != nil {
		var buf strings.Builder
		buf.WriteString(summary)
		buf.WriteByte('\n')
		for i, opt := range options {
			fmt.Fprintf(&buf, "  [%d] %s\n", i+1, opt)
		}
		buf.WriteString("> ")
		if _, err := io.WriteString(runtime.Out, buf.String()); err != nil {
			return "", fmt.Errorf("confirm: write prompt: %w", err)
		}
	}
	reader := bufio.NewReader(runtime.In)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("confirm: read user response: %w", err)
	}
	answer := strings.TrimSpace(line)
	if answer == "" {
		return "", errors.New("confirm: empty response")
	}
	return matchOption(answer, options)
}

// matchOption applies the documented ordering: numeric index, exact match,
// then unique prefix. Ambiguous prefixes return an error so the model knows
// to ask again with a clearer answer.
func matchOption(answer string, options []string) (string, error) {
	lower := strings.ToLower(answer)
	if n, ok := parsePositiveInt(answer); ok && n >= 1 && n <= len(options) {
		return options[n-1], nil
	}
	for _, opt := range options {
		if strings.EqualFold(opt, answer) {
			return opt, nil
		}
	}
	matches := make([]string, 0, 2)
	for _, opt := range options {
		if strings.HasPrefix(strings.ToLower(opt), lower) {
			matches = append(matches, opt)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("confirm: %q is ambiguous among %v", answer, matches)
	}
	return "", fmt.Errorf("confirm: %q does not match any option in %v", answer, options)
}

func parsePositiveInt(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 && s != "0" {
		return 0, false
	}
	return n, true
}

// logConfirmResolution writes a session event capturing the user-facing
// summary, the option set, the resolution string (empty when aborted), and
// the path that produced the answer ("tty", "auto-confirm=yes", etc.).
// Safe on a nil session logger.
func logConfirmResolution(ctx context.Context, summary string, options []string, resolution, via string, resolveErr error) {
	logger := session.FromContext(ctx)
	payload := map[string]any{
		"summary":    summary,
		"options":    options,
		"resolution": resolution,
		"via":        via,
	}
	if resolveErr != nil {
		payload["error"] = resolveErr.Error()
	}
	if err := logger.Append(session.EventConfirmResolved, payload); err != nil {
		logging.WarnContext(ctx, "failed to log confirm_resolved event: %v", err)
	}
	logging.DebugContext(ctx, "Confirm resolved: via=%s resolution=%s", via, resolution)
}
