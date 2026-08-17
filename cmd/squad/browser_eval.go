package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
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

			var res json.RawMessage
			err = chromedp.Run(tabCtx, chromedp.Evaluate(script, &res))
			switch {
			case errors.Is(err, chromedp.ErrJSUndefined):
				res = json.RawMessage("undefined")
			case errors.Is(err, chromedp.ErrJSNull):
				res = json.RawMessage("null")
			case err != nil:
				return fmt.Errorf("evaluate failed: %w", err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(res))
			return err
		},
	}
	return cmd
}

// attachToActivePage connects to the active browser session for the named
// profile (via its DevToolsActivePort endpoint) and returns a context
// attached to the session's first real page, plus a cleanup func. The
// cleanup detaches WITHOUT closing the page: chromedp's own context cancel
// sends Target.closeTarget to any attached target, which would close the
// user's tab out from under them.
func attachToActivePage(ctx context.Context, profile string) (context.Context, func(), error) {
	wsURL, err := browser.ActivePort(profile)
	if err != nil {
		return nil, nil, err
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL)
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

	tabCtx, cancelTab := chromedp.NewContext(browserCtx, chromedp.WithTargetID(page.TargetID))
	cleanup := func() {
		// Drop the attachment before cancelling so chromedp's cleanup
		// goroutine (which fires Target.closeTarget on a non-nil Target)
		// leaves the page open. The CDP session itself dies with the
		// websocket connection.
		if c := chromedp.FromContext(tabCtx); c != nil {
			c.Target = nil
		}
		cancelTab()
		teardown()
	}

	// Attach now, on the long-lived context: chromedp binds the session's
	// event handling to the context of the first Run, and callers may only
	// ever Run with short-lived contexts derived from tabCtx.
	if err := chromedp.Run(tabCtx); err != nil {
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
