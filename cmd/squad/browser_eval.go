package main

import (
	"fmt"

	"github.com/chromedp/chromedp"
	"github.com/cowdogmoo/squad/browser"
	"github.com/spf13/cobra"
)

func newBrowserEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval NAME SCRIPT",
		Short: "Evaluate JavaScript in an active browser session",
		Long: `Connects to the active Chrome session for the given profile and
evaluates the provided JavaScript code. The result is printed to stdout.

Example:
  squad browser eval myprofile "document.body.innerText"
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			script := args[1]

			if err := browser.ValidateName(name); err != nil {
				return err
			}

			wsURL, err := browser.ActivePort(name)
			if err != nil {
				return err
			}

			allocCtx, cancel := chromedp.NewRemoteAllocator(cmd.Context(), wsURL)
			defer cancel()

			ctx, cancelCtx := chromedp.NewContext(allocCtx)
			defer cancelCtx()

			var res any
			if err := chromedp.Run(ctx, chromedp.Evaluate(script, &res)); err != nil {
				return fmt.Errorf("evaluate failed: %w", err)
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), res)
			return err
		},
	}
	return cmd
}
