package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/chromedp/chromedp"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

func newMCPServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run built-in MCP servers",
	}
	cmd.AddCommand(newMCPServerBrowserCmd())
	return cmd
}

func newMCPServerBrowserCmd() *cobra.Command {
	var userDataDir string
	var headless bool

	cmd := &cobra.Command{
		Use:   "browser",
		Short: "Run the built-in browser MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowserServerIO(cmd.Context(), userDataDir, headless, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&userDataDir, "user-data-dir", "", "Path to browser profile directory")
	cmd.Flags().BoolVar(&headless, "headless", true, "Run in headless mode")

	return cmd
}

func runBrowserServer(ctx context.Context, userDataDir string, headless bool) error {
	return runBrowserServerIO(ctx, userDataDir, headless, os.Stdin, os.Stdout)
}

func runBrowserServerIO(ctx context.Context, userDataDir string, headless bool, in io.Reader, out io.Writer) error {
	s := server.NewMCPServer("squad-browser", "1.0.0")

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", headless),
		chromedp.Flag("no-sandbox", true),
	)
	if userDataDir != "" {
		opts = append(opts, chromedp.UserDataDir(userDataDir))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	if err := chromedp.Run(browserCtx, chromedp.Navigate("about:blank")); err != nil {
		return fmt.Errorf("failed to initialize browser: %w", err)
	}

	registerBrowserTools(s, browserCtx)

	stdioServer := server.NewStdioServer(s)
	return stdioServer.Listen(ctx, in, out)
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

		err := chromedp.Run(browserCtx, chromedp.Navigate(url), chromedp.WaitReady("body", chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to navigate: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Navigated to %s", url)), nil
	})

	s.AddTool(mcp.NewTool("read_page",
		mcp.WithDescription("Extract all text from the current page body"),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var text string
		err := chromedp.Run(browserCtx, chromedp.Text("body", &text, chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to read page text: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	s.AddTool(mcp.NewTool("evaluate_js",
		mcp.WithDescription("Evaluate JavaScript on the current page"),
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

		var res any
		err := chromedp.Run(browserCtx, chromedp.Evaluate(script, &res))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to evaluate: %v", err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("%v", res)), nil
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

		err := chromedp.Run(browserCtx, chromedp.Click(sel, chromedp.ByQuery))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to click %s: %v", sel, err)), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Clicked element matching %s", sel)), nil
	})
}
