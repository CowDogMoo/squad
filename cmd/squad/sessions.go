package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/cowdogmoo/squad/runner"
	"github.com/cowdogmoo/squad/session"
	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Aliases: []string{"session"},
	Short:   "List sessions for the current repository",
	Long: "List squad sessions for the current repository. Sessions live under " +
		"$XDG_STATE_HOME/squad/sessions, so this is the way to find them.",
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath, err := currentRepoPath()
		if err != nil {
			return err
		}

		sessions, err := session.List(repoPath)
		if err != nil {
			return err
		}
		if len(sessions) == 0 {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "No sessions found for this repository.")
			return err
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tSTATUS\tCREATED\tWORKTREE")
		for _, s := range sessions {
			wt := s.WorktreePath
			if wt != "" && strings.HasPrefix(wt, repoPath) {
				if rel, err := filepath.Rel(repoPath, wt); err == nil {
					wt = rel
				}
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.SessionID, s.Status, s.Timestamp, wt)
		}
		return w.Flush()
	},
}

var openSessionCmd = &cobra.Command{
	Use:   "open [id]",
	Short: "Print the directory of a session",
	Long:  "Print the on-disk directory of a session. With no id, the latest session is used.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoPath, err := currentRepoPath()
		if err != nil {
			return err
		}

		id := ""
		if len(args) > 0 {
			id = args[0]
		}
		if id == "" {
			sessions, err := session.List(repoPath)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				return fmt.Errorf("no sessions found for %s", repoPath)
			}
			id = sessions[0].SessionID
		}

		dir := session.ResolveDir(repoPath, id)
		if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
			return fmt.Errorf("session %q not found", id)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), dir)
		return err
	},
}

// currentRepoPath resolves the canonical repository path for the working
// directory, mirroring how runs record their canonical path.
func currentRepoPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return runner.ResolveGitToplevel(wd), nil
}

func init() {
	sessionsCmd.AddCommand(openSessionCmd)
}
