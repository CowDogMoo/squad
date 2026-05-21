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
	"text/tabwriter"

	"github.com/cowdogmoo/squad/logging"
	"github.com/cowdogmoo/squad/skill"
	"github.com/spf13/cobra"
)

// newSkillCmd builds the `squad skill` command tree. Phase 1 covers list,
// show, and validate; later phases add new, add, update, remove.
func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Inspect and validate Agent Skills",
		Long: `Manage skills — single-directory capabilities a running agent loads on demand.

Skills follow the Anthropic Agent Skills open standard. Each skill is a
directory containing a SKILL.md file with YAML frontmatter (name + description)
and a markdown body. Skills live in two scopes:

  - repo:   <cwd>/.squad/skills/<name>/SKILL.md (checked into git)
  - global: $XDG_CONFIG_HOME/squad/skills/<name>/SKILL.md (per-user)

A repo-scoped skill shadows a global one with the same name.`,
	}
	cmd.AddCommand(
		newSkillListCmd(),
		newSkillShowCmd(),
		newSkillValidateCmd(),
	)
	return cmd
}

func newSkillListCmd() *cobra.Command {
	var (
		repoOverride string
		showAll      bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			repo, err := resolveSkillRepoRoot(repoOverride)
			if err != nil {
				return err
			}
			cat, err := skill.Discover(repo)
			if err != nil {
				return err
			}
			reportLoadErrors(cmd, cat)
			entries := cat.Visible()
			if showAll {
				entries = cat.All()
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no skills found)")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSCOPE\tDESCRIPTION\tORIGIN")
			for _, e := range entries {
				name := e.Name()
				if e.Shadowed {
					name += " (shadowed)"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					name,
					e.Scope.String(),
					truncate(e.Manifest.Description, 60),
					shortenPath(e.Origin),
				)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&repoOverride, "repo", "", "Repo root for repo-scoped discovery (default: current working directory)")
	cmd.Flags().BoolVar(&showAll, "all", false, "Include shadowed skills in the output")
	return cmd
}

func newSkillShowCmd() *cobra.Command {
	var repoOverride string
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Print a skill's full SKILL.md with metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			repo, err := resolveSkillRepoRoot(repoOverride)
			if err != nil {
				return err
			}
			cat, err := skill.Discover(repo)
			if err != nil {
				return err
			}
			reportLoadErrors(cmd, cat)
			entry, ok := cat.Find(args[0])
			if !ok {
				return fmt.Errorf("skill %q not found", args[0])
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Name:        %s\n", entry.Manifest.Name)
			_, _ = fmt.Fprintf(out, "Scope:       %s\n", entry.Scope.String())
			_, _ = fmt.Fprintf(out, "Directory:   %s\n", entry.Dir)
			_, _ = fmt.Fprintf(out, "Manifest:    %s\n", entry.ManifestPath)
			_, _ = fmt.Fprintf(out, "Description: %s\n", entry.Manifest.Description)
			_, _ = fmt.Fprintln(out, "---")
			_, _ = fmt.Fprintln(out, entry.Manifest.Body)
			return nil
		},
	}
	cmd.Flags().StringVar(&repoOverride, "repo", "", "Repo root for repo-scoped discovery (default: current working directory)")
	return cmd
}

func newSkillValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <path>",
		Short: "Run spec-conformance checks on a skill directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			report, err := skill.Validate(args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, f := range report.Findings {
				_, _ = fmt.Fprintf(out, "%s: %s: %s\n", f.Severity, f.Path, f.Message)
			}
			if report.HasErrors() {
				return fmt.Errorf("validation failed: %d error(s)", len(report.Errors()))
			}
			if len(report.Warnings()) == 0 {
				_, _ = fmt.Fprintln(out, "OK")
			}
			return nil
		},
	}
}

// resolveSkillRepoRoot returns the directory whose .squad/skills/ should be
// scanned for repo-scoped skills. An explicit override wins; otherwise we use
// the current working directory.
func resolveSkillRepoRoot(override string) (string, error) {
	if override != "" {
		abs, err := absolutePath(override)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	return os.Getwd()
}

// reportLoadErrors logs (as warnings) every skill that failed to load. We
// don't fail the command — the rest of the catalog is still useful.
func reportLoadErrors(cmd *cobra.Command, cat *skill.Catalog) {
	for _, le := range cat.LoadErrors() {
		logging.WarnContext(cmd.Context(), "skipping invalid skill %s: %v", le.Path, le.Err)
	}
}
