package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/cowdogmoo/squad/browser"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// browserToolTimeout bounds one tool call against the shared browser: a page
// that never finishes loading must not wedge the server forever.
const browserToolTimeout = 2 * time.Minute

func newMCPServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run built-in MCP servers",
	}
	cmd.AddCommand(newMCPServerBrowserCmd())
	return cmd
}

// browserServerOptions selects how the browser MCP server gets its browser:
// attach to a running session (Profile) or launch its own (everything else).
type browserServerOptions struct {
	// Profile names a squad browser profile whose already-running Chrome
	// session (started with `squad browser open NAME --remote-debug`) the
	// server attaches to. This is the mode that shares the user's login
	// state and launches nothing. Mutually exclusive with UserDataDir.
	Profile string
	// UserDataDir is the profile directory for a launched browser.
	UserDataDir string
	// Headless controls the launched browser's mode.
	Headless bool
	// NoSandbox disables the launched Chrome's sandbox. Needed in some
	// containers; weakens isolation from visited pages.
	NoSandbox bool
}

func newMCPServerBrowserCmd() *cobra.Command {
	var opts browserServerOptions

	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Run the built-in browser MCP server",
		Long: `Run an MCP server exposing browser automation tools (navigate,
read_page, evaluate_js, click). It speaks standard MCP over stdio, so
any squad agent can use it regardless of model provider.

By default a headless browser is launched for the server's own use. Pass
--profile to instead attach to the already-running Chrome session of a
squad browser profile, so the tools drive a real, logged-in browser and
nothing new is launched. While attached, the controlled page is framed
in orange with a "squad" badge so it's always visible which page the
agent is driving:

  squad browser open myprofile --remote-debug
  squad mcp server browser --profile myprofile

Wire it into any agent via agent.yaml:

  mcp_servers:
    - name: browser
      command: squad
      args: [mcp, server, browser, --profile, myprofile]
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserServerIO(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&opts.Profile, "profile", "",
		"Attach to the active Chrome session of this squad browser profile instead of launching a browser")
	cmd.Flags().StringVar(&opts.UserDataDir, "user-data-dir", "", "Path to browser profile directory")
	cmd.Flags().BoolVar(&opts.Headless, "headless", true, "Run in headless mode")
	cmd.Flags().BoolVar(&opts.NoSandbox, "no-sandbox", false,
		"Disable Chrome's sandbox (needed in some containers; weakens isolation from visited pages)")

	return cmd
}

func runBrowserServerIO(ctx context.Context, opts browserServerOptions, in io.Reader, out io.Writer) error {
	s := server.NewMCPServer("squad-browser", "1.0.0")

	browserCtx, cleanup, err := connectBrowserServer(ctx, opts)
	if err != nil {
		return err
	}
	defer cleanup()

	registerBrowserTools(s, browserCtx)

	stdioServer := server.NewStdioServer(s)
	return stdioServer.Listen(ctx, in, out)
}

// connectBrowserServer resolves the browser context the tools run against:
// an attachment to the profile's running session, or a freshly launched
// browser. The launched path pins the binary to the same discovery Launch
// uses (SQUAD_BROWSER_BIN, then Google Chrome before Chromium) instead of
// chromedp's own preference order.
func connectBrowserServer(ctx context.Context, opts browserServerOptions) (context.Context, func(), error) {
	if opts.Profile != "" {
		if opts.UserDataDir != "" {
			return nil, nil, errors.New("--profile attaches to a running session and cannot be combined with --user-data-dir")
		}
		tabCtx, cleanup, err := attachToActivePage(ctx, opts.Profile)
		if err != nil {
			return nil, nil, err
		}
		removeIndicator, err := installAttachIndicator(tabCtx)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("install attach indicator: %w", err)
		}
		return tabCtx, func() {
			removeIndicator()
			cleanup()
		}, nil
	}

	execOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", opts.Headless),
	)
	if bin, err := browser.FindChrome(); err == nil {
		execOpts = append(execOpts, chromedp.ExecPath(bin))
	}
	if opts.NoSandbox {
		execOpts = append(execOpts, chromedp.NoSandbox)
	}
	if opts.UserDataDir != "" {
		execOpts = append(execOpts, chromedp.UserDataDir(opts.UserDataDir))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, execOpts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	cleanup := func() {
		cancelBrowser()
		cancelAlloc()
	}

	if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to initialize browser: %w", err)
	}
	return browserCtx, cleanup, nil
}

// runBrowserAction runs actions against the shared browser context, bounded
// by browserToolTimeout and aborted early when the MCP request's own context
// is cancelled. chromedp actions must run on the browser context (not the
// request context), which would otherwise let a hung page load outlive both
// the request and the timeout.
func runBrowserAction(reqCtx, browserCtx context.Context, actions ...chromedp.Action) error {
	runCtx, cancel := context.WithTimeout(browserCtx, browserToolTimeout)
	defer cancel()
	stop := context.AfterFunc(reqCtx, cancel)
	defer stop()
	return chromedp.Run(runCtx, actions...)
}

func registerBrowserTools(s *server.MCPServer, browserCtx context.Context) {
	s.AddTool(mcp.NewTool("navigate",
		mcp.WithDescription("Navigate the browser to a given URL"),
		mcp.WithString("url", mcp.Required(), mcp.Description("The URL to navigate to")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments"), nil
		}
		url, ok := args["url"].(string)
		if !ok {
			return mcp.NewToolResultError("url must be a string"), nil
		}

		err := runBrowserAction(ctx, browserCtx, chromedp.Navigate(url), chromedp.WaitReady("body", chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to navigate: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Navigated to %s", url)), nil
	})

	s.AddTool(mcp.NewTool("read_page",
		mcp.WithDescription("Extract all text from the current page body"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var text string
		err := runBrowserAction(ctx, browserCtx, chromedp.Text("body", &text, chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read page text: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	s.AddTool(mcp.NewTool("evaluate_js",
		mcp.WithDescription("Evaluate JavaScript on the current page; the result is returned as JSON"),
		mcp.WithString("script", mcp.Required(), mcp.Description("The JavaScript code to evaluate")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments"), nil
		}
		script, ok := args["script"].(string)
		if !ok {
			return mcp.NewToolResultError("script must be a string"), nil
		}

		var res json.RawMessage
		err := runBrowserAction(ctx, browserCtx, chromedp.Evaluate(script, &res))
		switch {
		case errors.Is(err, chromedp.ErrJSUndefined):
			res = json.RawMessage("undefined")
		case errors.Is(err, chromedp.ErrJSNull):
			res = json.RawMessage("null")
		case err != nil:
			return mcp.NewToolResultError(fmt.Sprintf("Failed to evaluate: %v", err)), nil
		}

		return mcp.NewToolResultText(string(res)), nil
	})

	s.AddTool(mcp.NewTool("click",
		mcp.WithDescription("Click an element on the current page matching a CSS selector"),
		mcp.WithString("selector", mcp.Required(), mcp.Description("CSS selector to click")),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, ok := req.Params.Arguments.(map[string]any)
		if !ok {
			return mcp.NewToolResultError("invalid arguments"), nil
		}
		sel, ok := args["selector"].(string)
		if !ok {
			return mcp.NewToolResultError("selector must be a string"), nil
		}

		err := runBrowserAction(ctx, browserCtx, chromedp.Click(sel, chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to click %s: %v", sel, err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Clicked element matching %s", sel)), nil
	})
}
