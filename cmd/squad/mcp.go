/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

MIT License — see LICENSE for full text.
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cowdogmoo/squad/agent"
	"github.com/cowdogmoo/squad/mcp"
	"github.com/cowdogmoo/squad/runner"
	"github.com/spf13/cobra"
)

// newMCPCmd builds the `squad mcp` parent command and its subcommands.
// These tools let users inspect an MCP server without doing a full agent run.
func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Inspect and debug MCP servers",
		Long: `Commands for working with Model Context Protocol servers.

Server specs follow the same format as --mcp-server:
  Stdio:           NAME:COMMAND[:ARG1,ARG2,...]
  SSE:             NAME:sse:URL
  Streamable HTTP: NAME:http:URL`,
	}
	cmd.AddCommand(newMCPListCmd())
	cmd.AddCommand(newMCPProbeCmd())
	cmd.AddCommand(newMCPToolsCmd())
	return cmd
}

func newMCPListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List MCP servers declared by an agent",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			agentName, _ := cmd.Flags().GetString("agent")
			if agentName == "" {
				return fmt.Errorf("--agent is required")
			}
			agentsDir, _ := cmd.Flags().GetString("agents-dir")
			cfg := configFromContext(cmd)
			agentDir, err := runner.FindAgentDir(agentName, agentsDir, cfg)
			if err != nil {
				return err
			}
			manifest, err := agent.LoadManifest(agentDir)
			if err != nil {
				return err
			}
			if len(manifest.MCPServers) == 0 {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "agent %q declares no MCP servers\n", agentName)
				return err
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "Agent %q declares %d MCP server(s):\n\n", agentName, len(manifest.MCPServers)); err != nil {
				return err
			}
			for _, s := range manifest.MCPServers {
				if _, err := fmt.Fprintf(out, "  %s  (%s)\n", s.Name, s.ConnectString()); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().String("agent", "", "Agent name (e.g. weekly-planner)")
	cmd.Flags().String("agents-dir", "", "Agents directory (default: search configured sources)")
	return cmd
}

func newMCPProbeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "probe SPEC",
		Short: "Connect to an MCP server and dump server info",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cfg, err := parseSingleMCPSpec(args[0])
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			client, err := mcp.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer closeMCPClient(client)
			out := cmd.OutOrStdout()
			_, err = fmt.Fprintf(out, "Connected to %q (%s): %d tools\n", client.Name(), cfg.ConnectString(), len(client.Tools()))
			return err
		},
	}
	return cmd
}

func newMCPToolsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tools SPEC",
		Short: "Connect to an MCP server and list its tools",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cfg, err := parseSingleMCPSpec(args[0])
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			client, err := mcp.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer closeMCPClient(client)

			tools := client.Tools()
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "%d tool(s) from %q:\n\n", len(tools), client.Name()); err != nil {
				return err
			}
			showJSON, _ := cmd.Flags().GetBool("json")
			for _, t := range tools {
				if showJSON {
					if err := emitToolJSON(out, t.Name, t.Description, t.InputSchema); err != nil {
						return err
					}
					continue
				}
				if _, err := fmt.Fprintf(out, "  %s — %s\n", t.Name, t.Description); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Emit each tool's full JSON schema")
	return cmd
}

// emitToolJSON serializes a single tool to the writer as pretty JSON.
func emitToolJSON(w io.Writer, name, description string, schema any) error {
	doc := map[string]any{"name": name, "description": description, "input_schema": schema}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n\n", b)
	return err
}

// parseSingleMCPSpec is a thin wrapper around parseMCPServers for the SPEC
// argument shared by `mcp probe` and `mcp tools`.
func parseSingleMCPSpec(spec string) (mcp.ServerConfig, error) {
	configs := parseMCPServers([]string{spec})
	if len(configs) == 0 {
		return mcp.ServerConfig{}, fmt.Errorf("invalid MCP server spec %q (want NAME:COMMAND[:ARGS], NAME:sse:URL, or NAME:http:URL)", spec)
	}
	cfg := configs[0]
	if cfg.Name == "" {
		return mcp.ServerConfig{}, fmt.Errorf("invalid MCP server spec %q: missing name", spec)
	}
	return cfg, nil
}

// closeMCPClient swallows the close error to keep CLI exit paths simple;
// any non-nil error is surfaced via stderr by the underlying logger.
func closeMCPClient(c *mcp.Client) {
	if c == nil {
		return
	}
	_ = c.Close()
}
