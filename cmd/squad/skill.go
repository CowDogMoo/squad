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
	"github.com/cowdogmoo/squad/source"
	"github.com/spf13/cobra"
)

// newSkillCmd builds the `squad skill` command tree.
func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Inspect, validate, and manage Agent Skills",
		Long: `Manage skills — single-directory capabilities a running agent loads on demand.

Skills follow the Anthropic Agent Skills open standard. Each skill is a
directory containing a SKILL.md file with YAML frontmatter (name + description)
and a markdown body. Skills live in three scopes:

  - repo:    <cwd>/.squad/skills/<name>/SKILL.md (checked into git)
  - global:  $XDG_CONFIG_HOME/squad/skills/<name>/SKILL.md (per-user)
  - catalog: cloned git repos or registered local paths under cfg.Skills

Precedence: repo > global > catalog. Names that collide are shadowed at the
lower-precedence scope.`,
	}
	cmd.AddCommand(
		newSkillListCmd(),
		newSkillShowCmd(),
		newSkillValidateCmd(),
		newSkillAddCmd(),
		newSkillRemoveCmd(),
		newSkillUpdateCmd(),
		newSkillSourcesCmd(),
	)
	return cmd
}

// discoverSkills builds a catalog using the current cwd plus every catalog
// source configured in cfg.Skills. Returns the catalog and the search path
// label so callers can render `Origin` correctly.
func discoverSkills(cmd *cobra.Command, repoOverride string) (*skill.Catalog, error) {
	repo, err := resolveSkillRepoRoot(repoOverride)
	if err != nil {
		return nil, err
	}
	cat, err := skill.Discover(repo, skillCatalogPaths(cmd)...)
	if err != nil {
		return nil, err
	}
	reportLoadErrors(cmd, cat)
	return cat, nil
}

// skillCatalogPaths returns the local + cached-repo paths registered in
// cfg.Skills. Errors building the manager are downgraded to warnings so the
// CLI still works when XDG paths can't be created.
func skillCatalogPaths(cmd *cobra.Command) []string {
	cfg := configFromContext(cmd)
	if cfg == nil {
		return nil
	}
	mgr, err := source.NewSkillsManager(cfg)
	if err != nil {
		logging.WarnContext(cmd.Context(), "failed to build skills manager: %v", err)
		return nil
	}
	return mgr.CatalogPaths()
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
			cat, err := discoverSkills(cmd, repoOverride)
			if err != nil {
				return err
			}
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
			cat, err := discoverSkills(cmd, repoOverride)
			if err != nil {
				return err
			}
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

func newSkillAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <alias> <git-url-or-local-path>",
		Short: "Register a skill catalog source (git repo or local directory)",
		Long: `Register a new skills catalog. Two forms are accepted:

  squad skill add myteam https://github.com/me/squad-skills.git
  squad skill add local /opt/shared/skills

Git repositories are cloned into the skills cache on registration so the
catalog is immediately discoverable. Local paths are added as-is.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			alias, target := args[0], args[1]
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available in context")
			}
			mgr, err := source.NewSkillsManager(cfg)
			if err != nil {
				return err
			}
			if source.IsGitURL(target) {
				if err := mgr.AddRepository(alias, target); err != nil {
					return err
				}
				logging.InfoContext(cmd.Context(), "registered skill repository %s → %s", alias, target)
				if err := mgr.EnsureRepositoriesCloned(); err != nil {
					logging.WarnContext(cmd.Context(), "clone failed (you can retry with `squad skill update`): %v", err)
				}
				return nil
			}
			if err := mgr.AddLocalPath(target); err != nil {
				return err
			}
			logging.InfoContext(cmd.Context(), "registered skill local path %s", target)
			return nil
		},
	}
}

func newSkillRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <alias-or-path>",
		Aliases: []string{"rm"},
		Short:   "Unregister a skill catalog source",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available in context")
			}
			mgr, err := source.NewSkillsManager(cfg)
			if err != nil {
				return err
			}
			if err := mgr.RemoveSource(args[0]); err != nil {
				return err
			}
			logging.InfoContext(cmd.Context(), "unregistered skill source: %s", args[0])
			return nil
		},
	}
}

func newSkillUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "git pull every registered skill catalog repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available in context")
			}
			mgr, err := source.NewSkillsManager(cfg)
			if err != nil {
				return err
			}
			if err := mgr.UpdateRepositories(); err != nil {
				return err
			}
			logging.InfoContext(cmd.Context(), "all skill repositories updated")
			return nil
		},
	}
}

func newSkillSourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List configured skill catalog sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available in context")
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			if len(cfg.Skills.Repositories) > 0 {
				_, _ = fmt.Fprintln(w, "REPOSITORIES:")
				_, _ = fmt.Fprintln(w, "NAME\tURL")
				for name, url := range cfg.Skills.Repositories {
					_, _ = fmt.Fprintf(w, "%s\t%s\n", name, url)
				}
				_, _ = fmt.Fprintln(w)
			}
			if len(cfg.Skills.LocalPaths) > 0 {
				_, _ = fmt.Fprintln(w, "LOCAL PATHS:")
				for _, path := range cfg.Skills.LocalPaths {
					_, _ = fmt.Fprintln(w, path)
				}
			}
			if len(cfg.Skills.Repositories) == 0 && len(cfg.Skills.LocalPaths) == 0 {
				_, _ = fmt.Fprintln(w, "No sources configured. Run 'squad skill add <alias> <url>' to add one.")
			}
			return w.Flush()
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
