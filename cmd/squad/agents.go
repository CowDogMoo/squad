/*
Copyright © 2026 Jayson Grace <jayson.e.grace@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/source"
	"github.com/spf13/cobra"
)

var agentsCmd = &cobra.Command{
	Use:   "agents",
	Short: "Manage agent sources",
	Long: `Manage agent sources including git repositories and local directories.

Agent sources are searched in priority order:
1. ./agents (local development)
2. Configured local paths
3. Cached git repositories
4. ~/.config/squad/agents (user agents)`,
}

var agentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available agents",
	Long:  `List all available agents from all configured sources.`,
	RunE:  runAgentsList,
	Args:  cobra.NoArgs,
}

var (
	agentsAddRef       string
	agentsUpdateForce  bool
	agentsPinUnsetFlag bool
)

var agentsAddCmd = &cobra.Command{
	Use:   "add [name] <url-or-path>",
	Short: "Add an agent source",
	Long: `Add a git repository or local directory as an agent source.

For git repositories:
  squad agents add https://github.com/user/agents.git
  squad agents add myrepo https://github.com/user/agents.git

Pin a repository to a specific commit, tag, or branch with --ref so
unattended runs always resolve the same source content:

  squad agents add official https://github.com/cowdogmoo/squad-agents.git --ref v0.4.2
  squad agents add official https://github.com/cowdogmoo/squad-agents.git --ref 7a3fe6cf

For local directories:
  squad agents add /path/to/agents`,
	RunE: runAgentsAdd,
	Args: cobra.RangeArgs(1, 2),
}

var agentsPinCmd = &cobra.Command{
	Use:   "pin <name> <ref>",
	Short: "Pin an agent source to a specific ref",
	Long: `Pin an existing agent repository to a specific commit SHA, tag, or branch.

Subsequent 'squad agents update' calls skip pinned sources unless --force
is supplied. To unpin, use 'squad agents pin <name> --unset'.`,
	RunE: runAgentsPin,
	Args: cobra.RangeArgs(1, 2),
}

var agentsRemoveCmd = &cobra.Command{
	Use:     "remove <name-or-path>",
	Aliases: []string{"rm"},
	Short:   "Remove an agent source",
	Long:    `Remove a git repository by name or a local path from agent sources.`,
	RunE:    runAgentsRemove,
	Args:    cobra.ExactArgs(1),
}

var agentsUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update cached git repositories",
	Long:  `Pull the latest changes from all configured git repositories.`,
	RunE:  runAgentsUpdate,
	Args:  cobra.NoArgs,
}

var agentsSourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List configured agent sources",
	Long:  `List all configured agent sources (git repositories and local paths).`,
	RunE:  runAgentsSources,
	Args:  cobra.NoArgs,
}

func init() {
	agentsAddCmd.Flags().StringVar(&agentsAddRef, "ref", "", "Pin to commit SHA, tag, or branch (defaults to tracking the default branch)")
	agentsUpdateCmd.Flags().BoolVar(&agentsUpdateForce, "force", false, "Re-resolve and update pinned repositories too")
	agentsPinCmd.Flags().BoolVar(&agentsPinUnsetFlag, "unset", false, "Remove an existing pin (alias for empty ref)")

	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsAddCmd)
	agentsCmd.AddCommand(agentsRemoveCmd)
	agentsCmd.AddCommand(agentsUpdateCmd)
	agentsCmd.AddCommand(agentsSourcesCmd)
	agentsCmd.AddCommand(agentsPinCmd)
}

func runAgentsList(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return err
	}

	agents, err := manager.ListAgents()
	if err != nil {
		return err
	}

	if len(agents) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No agents found. Run 'squad agents update' to fetch from configured sources.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION\tSOURCE")
	for _, agent := range agents {
		source := shortenPath(agent.Source)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			agent.Name,
			agent.Version,
			truncate(agent.Description, 50),
			source,
		)
	}
	return w.Flush()
}

func runAgentsAdd(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	var name, urlOrPath string

	if len(args) == 2 {
		name = args[0]
		urlOrPath = args[1]
		if !source.IsGitURL(urlOrPath) {
			return fmt.Errorf("when providing a name, the second argument must be a git URL")
		}
	} else {
		urlOrPath = args[0]
	}

	if source.IsGitURL(urlOrPath) {
		if name == "" {
			name = guessRepoName(urlOrPath)
		}
		if err := manager.AddRepository(name, urlOrPath, agentsAddRef); err != nil {
			return err
		}
		if agentsAddRef != "" {
			logging.InfoContext(ctx, "Added repository %s: %s (pinned to %s)", name, urlOrPath, agentsAddRef)
		} else {
			logging.InfoContext(ctx, "Added repository %s: %s", name, urlOrPath)
		}
	} else {
		if agentsAddRef != "" {
			return fmt.Errorf("--ref only applies to git repositories, not local paths")
		}
		if err := manager.AddLocalPath(urlOrPath); err != nil {
			return err
		}
		logging.InfoContext(ctx, "Added local path: %s", urlOrPath)
	}

	return nil
}

func runAgentsPin(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return err
	}

	name := args[0]
	ref := ""
	switch {
	case agentsPinUnsetFlag:
		if len(args) > 1 {
			return fmt.Errorf("cannot combine --unset with a ref argument")
		}
	case len(args) == 2:
		ref = args[1]
	default:
		return fmt.Errorf("usage: squad agents pin <name> <ref> | squad agents pin <name> --unset")
	}

	if err := manager.PinRepository(name, ref); err != nil {
		return err
	}
	if ref == "" {
		logging.InfoContext(cmd.Context(), "Unpinned %s (now tracking the default branch)", name)
	} else {
		logging.InfoContext(cmd.Context(), "Pinned %s to %s", name, ref)
	}
	return nil
}

func runAgentsRemove(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return err
	}

	if err := manager.RemoveSource(args[0]); err != nil {
		return err
	}

	logging.InfoContext(cmd.Context(), "Removed source: %s", args[0])
	return nil
}

func runAgentsUpdate(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	manager, err := source.NewManager(cfg)
	if err != nil {
		return err
	}

	if err := manager.UpdateRepositories(agentsUpdateForce); err != nil {
		return err
	}

	logging.InfoContext(cmd.Context(), "All repositories updated")
	return nil
}

func runAgentsSources(cmd *cobra.Command, args []string) error {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return fmt.Errorf("config not available in context")
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	if len(cfg.Agents.Repositories) > 0 {
		_, _ = fmt.Fprintln(w, "REPOSITORIES:")
		_, _ = fmt.Fprintln(w, "NAME\tURL\tREF")
		for name, spec := range cfg.Agents.Repositories {
			ref := spec.Ref
			if ref == "" {
				ref = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", name, spec.URL, ref)
		}
		_, _ = fmt.Fprintln(w)
	}

	if len(cfg.Agents.LocalPaths) > 0 {
		_, _ = fmt.Fprintln(w, "LOCAL PATHS:")
		for _, path := range cfg.Agents.LocalPaths {
			_, _ = fmt.Fprintln(w, path)
		}
	}

	if len(cfg.Agents.Repositories) == 0 && len(cfg.Agents.LocalPaths) == 0 {
		_, _ = fmt.Fprintln(w, "No sources configured. Run 'squad agents add <url>' to add one.")
	}

	return w.Flush()
}

func guessRepoName(gitURL string) string {
	if idx := strings.LastIndex(gitURL, ":"); idx != -1 && !strings.HasPrefix(gitURL, "http") {
		gitURL = gitURL[idx+1:]
	}
	if idx := strings.LastIndex(gitURL, "/"); idx != -1 {
		gitURL = gitURL[idx+1:]
	}
	gitURL = strings.TrimSuffix(gitURL, ".git")
	return gitURL
}

func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !filepath.IsAbs(rel) {
		return "~/" + rel
	}
	return path
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
