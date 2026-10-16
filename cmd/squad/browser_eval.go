package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/cowdogmoo/squad/browser"
	"github.com/spf13/cobra"
)

func newBrowserEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval NAME SCRIPT",
		Short: "Evaluate JavaScript in an active browser session",
		Long: `Connects to the active Chrome session for the given profile and
evaluates the provided JavaScript in its first open page. The result is
printed to stdout as JSON.

The session must have been started with remote debugging enabled:

  squad browser open myprofile --remote-debug

Example:
  squad browser eval myprofile "document.body.innerText"
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			script := args[1]

			tabCtx, cleanup, err := attachToActivePage(cmd.Context(), name)
			if err != nil {
				return err
			}
			defer cleanup()

			res, err := chromedp.Run(tabCtx, evaluateJSON(script))
			if err != nil {
				return fmt.Errorf("evaluate failed: %w", err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(res))
			return err
		},
	}
	return cmd
}

// evaluateJSON evaluates script and returns its result as JSON, or the
// literal undefined when the script produces no value, so callers print
// exactly what the page produced.
func evaluateJSON(script string) chromedp.Action[json.RawMessage] {
	eval := chromedp.Evaluate[*runtime.RemoteObject](script, func(p *runtime.EvaluateParams) {
		p.ReturnByValue = new(true)
	})
	return func(ctx context.Context, t *chromedp.Target) (json.RawMessage, error) {
		obj, err := eval(ctx, t)
		switch {
		case err != nil:
			return nil, err
		case obj.Type == runtime.RemoteObjectTypeUndefined:
			return json.RawMessage("undefined"), nil
		case len(obj.Value) == 0:
			return json.RawMessage("null"), nil
		}
		return json.RawMessage(obj.Value), nil
	}
}

// attachToActivePage connects to the active browser session for the named
// profile (via its DevToolsActivePort endpoint) and returns a context
// attached to the session's first real page, plus a cleanup func. The
// cleanup detaches WITHOUT closing the page: by default chromedp's context
// cancel sends Target.closeTarget to an attached target, which would close
// the user's tab out from under them.
func attachToActivePage(ctx context.Context, profile string) (context.Context, func(), error) {
	wsURL, err := browser.ActivePort(profile)
	if err != nil {
		return nil, nil, err
	}

	allocCtx, cancelAlloc := remote.NewAllocator(ctx, wsURL)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	teardown := func() {
		cancelBrowser()
		cancelAlloc()
	}

	page, err := firstPageTarget(browserCtx)
	if err != nil {
		teardown()
		return nil, nil, err
	}

	tabCtx, cancelTab := chromedp.NewContext(browserCtx,
		chromedp.WithTargetID(page.TargetID),
		chromedp.WithDetachOnCancel(),
	)
	cleanup := func() {
		cancelTab()
		teardown()
	}

	// Attach now, on the long-lived context: chromedp binds the session's
	// event handling to the context of the first Run, and callers may only
	// ever Run with short-lived contexts derived from tabCtx.
	if err := chromedp.Do(tabCtx); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("attach to page: %w", err)
	}
	return tabCtx, cleanup, nil
}

// firstPageTarget returns the session's first real page, skipping Chrome's
// internal targets — evaluating in those (or in a fresh tab, which is what
// a target-less context would silently create) is never what the caller
// asked for.
func firstPageTarget(browserCtx context.Context) (*target.Info, error) {
	targets, err := chromedp.Targets(browserCtx)
	if err != nil {
		return nil, fmt.Errorf("list browser targets: %w", err)
	}
	for _, t := range targets {
		if t.Type == "page" &&
			!strings.HasPrefix(t.URL, "chrome://") &&
			!strings.HasPrefix(t.URL, "devtools://") {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no open page in the browser session (%d targets)", len(targets))
}
