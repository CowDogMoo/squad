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

var agentsAddCmd = &cobra.Command{
	Use:   "add [name] <url-or-path>",
	Short: "Add an agent source",
	Long: `Add a git repository or local directory as an agent source.

For git repositories:
  squad agents add https://github.com/user/agents.git
  squad agents add myrepo https://github.com/user/agents.git

For local directories:
  squad agents add /path/to/agents`,
	RunE: runAgentsAdd,
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
	agentsCmd.AddCommand(agentsListCmd)
	agentsCmd.AddCommand(agentsAddCmd)
	agentsCmd.AddCommand(agentsRemoveCmd)
	agentsCmd.AddCommand(agentsUpdateCmd)
	agentsCmd.AddCommand(agentsSourcesCmd)
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
		fmt.Fprintln(cmd.OutOrStdout(), "No agents found. Run 'squad agents update' to fetch from configured sources.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tDESCRIPTION\tSOURCE")
	for _, agent := range agents {
		source := shortenPath(agent.Source)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
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
			// Generate name from URL
			name = guessRepoName(urlOrPath)
		}
		if err := manager.AddRepository(name, urlOrPath); err != nil {
			return err
		}
		logging.InfoContext(ctx, "Added repository %s: %s", name, urlOrPath)
	} else {
		if err := manager.AddLocalPath(urlOrPath); err != nil {
			return err
		}
		logging.InfoContext(ctx, "Added local path: %s", urlOrPath)
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

	if err := manager.UpdateRepositories(); err != nil {
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
		fmt.Fprintln(w, "REPOSITORIES:")
		fmt.Fprintln(w, "NAME\tURL")
		for name, url := range cfg.Agents.Repositories {
			fmt.Fprintf(w, "%s\t%s\n", name, url)
		}
		fmt.Fprintln(w)
	}

	if len(cfg.Agents.LocalPaths) > 0 {
		fmt.Fprintln(w, "LOCAL PATHS:")
		for _, path := range cfg.Agents.LocalPaths {
			fmt.Fprintln(w, path)
		}
	}

	if len(cfg.Agents.Repositories) == 0 && len(cfg.Agents.LocalPaths) == 0 {
		fmt.Fprintln(w, "No sources configured. Run 'squad agents add <url>' to add one.")
	}

	return w.Flush()
}

func guessRepoName(gitURL string) string {
	// Extract repo name from URL
	// Handle git@github.com:user/repo.git format
	if idx := strings.LastIndex(gitURL, ":"); idx != -1 && !strings.HasPrefix(gitURL, "http") {
		gitURL = gitURL[idx+1:]
	}
	// Handle https://github.com/user/repo.git format
	if idx := strings.LastIndex(gitURL, "/"); idx != -1 {
		gitURL = gitURL[idx+1:]
	}
	// Remove .git suffix
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
