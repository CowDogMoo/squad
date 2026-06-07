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
	"github.com/cowdogmoo/squad/scaffold"
	"github.com/spf13/cobra"
)

// newInitAgentCmd constructs the agent initialization subcommand.
func newInitAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent NAME",
		Short: "Create a new agent from templates",
		Long: `Create a new agent directory with scaffolded prompt files.

This command creates a new agent in the agents directory with:
- agent.yaml (manifest)
- system.md (system prompt with mode conditionals)
- agent.md (execution wrapper)
- task.md (task instructions)
- README.md (documentation)
- references/<name>-guide.md (knowledge base template)

Examples:
  # Create a generic agent
  squad init agent xss-testing

  # Create a Bash script review agent
  squad init agent bash-review --lang bash

  # Create from an existing agent
  squad init agent my-review --from go-review

  # Force overwrite existing agent
  squad init agent my-agent --force`,
		Args: cobra.ExactArgs(1),
		RunE: runInitAgent,
	}

	cmd.Flags().StringP("lang", "l", "generic", "Language template (go, python, bash, ansible, generic)")
	cmd.Flags().String("from", "", "Copy from existing agent instead of template")
	cmd.Flags().StringP("description", "d", "", "Agent description (default: auto-generated)")
	cmd.Flags().String("agents-dir", "agents", "Directory containing agents")
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing agent directory")

	_ = cmd.RegisterFlagCompletionFunc("lang", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"go", "python", "bash", "ansible", "generic"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}

// runInitAgent creates a new agent from templates or copies an existing one.
func runInitAgent(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx := cmd.Context()

	lang, _ := cmd.Flags().GetString("lang")
	from, _ := cmd.Flags().GetString("from")
	description, _ := cmd.Flags().GetString("description")
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	force, _ := cmd.Flags().GetBool("force")

	if from != "" {
		return scaffold.CopyAgent(ctx, agentsDir, from, name, force)
	}

	return scaffold.CreateAgent(ctx, scaffold.CreateOptions{
		Name:        name,
		Lang:        lang,
		Description: description,
		AgentsDir:   agentsDir,
		Force:       force,
	})
}
