package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cowdogmoo/squad/browser"
	"github.com/spf13/cobra"
)

// newBrowserCmd is the top-level `squad browser` command. Subcommands
// manage named Chromium user-data directories that browser-driving MCPs
// (chrome-devtools-mcp) reference by name via the {{.BrowserProfile}}
// template helper in agent.yaml.
func newBrowserCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Manage named browser profiles for agents that drive Chrome",
		Long: `Manage named Chromium user-data directories used by browser-driving MCPs.

Profiles live under $XDG_DATA_HOME/squad/browser-profiles/<name>. Each
profile is a long-lived Chrome data dir the user signs into once. Agents
reference a profile by name from agent.yaml:

  mcp_servers:
    - name: chrome
      command: npx
      args:
        - chrome-devtools-mcp@latest
        - --userDataDir={{.BrowserProfile "amazon"}}

Typical workflow:

  squad browser open amazon https://www.amazon.com/
  # ...sign into Amazon in the Chrome window that opens, then quit Chrome
  squad run --agent grocery-runner    # agent uses the now-authenticated profile
`,
	}
	cmd.AddCommand(newBrowserOpenCmd())
	cmd.AddCommand(newBrowserListCmd())
	cmd.AddCommand(newBrowserDeleteCmd())
	cmd.AddCommand(newBrowserPathCmd())
	return cmd
}

func newBrowserOpenCmd() *cobra.Command {
	var wait bool
	cmd := &cobra.Command{
		Use:   "open NAME [URL]",
		Short: "Open a profile in Chrome for interactive setup",
		Long: `Launch Chrome against the named profile. The profile is created if it
doesn't exist yet. Use this to sign into sites whose cookies the agent
needs later.

By default this command starts Chrome and returns immediately, leaving
the browser window open for you to interact with. Use --wait to block
until you quit Chrome.
`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := browser.ValidateName(name); err != nil {
				return err
			}
			url := ""
			if len(args) == 2 {
				url = args[1]
			}
			dir, err := browser.ProfileDir(name)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
				"Opening Chrome with profile %q at %s\n"+
					"Sign in / set things up, then quit Chrome to save the session.\n",
				name, dir)
			return browser.Launch(name, browser.LaunchOptions{
				URL:    url,
				Wait:   wait,
				Stderr: os.Stderr,
			})
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "Block until Chrome quits (default: start and return)")
	return cmd
}

func newBrowserListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List squad-managed browser profiles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			profiles, err := browser.List()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"No browser profiles. Create one with: squad browser open <name>\n"+
						"Root: %s\n", browser.Root())
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-24s  %-25s  %s\n", "NAME", "LAST-MODIFIED", "PATH")
			for _, p := range profiles {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-24s  %-25s  %s\n",
					p.Name, p.ModTime.Format(time.RFC3339), p.Dir)
			}
			return nil
		},
	}
}

func newBrowserDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a browser profile (irreversible)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := browser.ValidateName(name); err != nil {
				return err
			}
			if !browser.Exists(name) {
				return fmt.Errorf("profile %q does not exist", name)
			}
			if !force {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"This will permanently delete profile %q (including any saved logins).\n"+
						"Re-run with --force to confirm.\n", name)
				return errors.New("delete not confirmed")
			}
			if err := browser.Delete(name); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "deleted profile %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Required to actually delete")
	return cmd
}

func newBrowserPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path NAME",
		Short: "Print the absolute filesystem path for a profile (creates it if missing)",
		Long: `Useful when constructing shell commands or wiring an agent.yaml
manually. Mirrors what {{.BrowserProfile "name"}} resolves to at
template-evaluation time.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := browser.ProfileDir(args[0])
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		},
	}
}
