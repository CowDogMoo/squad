package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/watch"
	"github.com/spf13/cobra"
)

// watchPollInterval is how often `squad watch` re-reads a session it
// follows.
const watchPollInterval = 500 * time.Millisecond

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch [session-id | session-dir]",
		Short: "Print a session's events as the agent works",
		Long: "Print one line per event from a session's events.jsonl: model turns " +
			"with the assistant's text, tool calls with their arguments, tool results " +
			"with size, duration and errors, and refused tool calls. With no argument " +
			"the latest session for the current repository is used. squad watch keeps " +
			"following the session until it ends; pass --once to print what is there " +
			"now and exit.",
		Args: cobra.MaximumNArgs(1),
		RunE: runWatch,
	}
	cmd.Flags().Bool("once", false, "Print the events recorded so far and exit")
	return cmd
}

func runWatch(cmd *cobra.Command, args []string) error {
	once, _ := cmd.Flags().GetBool("once")
	target := ""
	if len(args) > 0 {
		target = args[0]
	}
	dir, err := resolveWatchDir(target)
	if err != nil {
		return err
	}
	return followSession(cmd.Context(), cmd.OutOrStdout(), watch.NewTailer(dir), once, watchPollInterval)
}

// resolveWatchDir maps the command argument to a session directory: an
// existing directory holding meta.json is used as is, an id is looked up
// for the current repository, and no argument means the latest session.
func resolveWatchDir(target string) (string, error) {
	if target != "" {
		if _, err := os.Stat(filepath.Join(target, "meta.json")); err == nil {
			return target, nil
		}
	}
	repoPath, err := currentRepoPath()
	if err != nil {
		return "", err
	}
	if target == "" || target == "latest" {
		sessions, err := session.List(repoPath)
		if err != nil {
			return "", err
		}
		if len(sessions) == 0 {
			return "", fmt.Errorf("no sessions found for %s", repoPath)
		}
		target = sessions[0].SessionID
	}
	dir := session.ResolveDir(repoPath, target)
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		return "", fmt.Errorf("session %q not found", target)
	}
	return dir, nil
}

// followSession prints the session header once meta.json is readable, then
// every event as it is appended. It returns after one pass with once set, or
// once the session has left the running state and every event is printed.
func followSession(ctx context.Context, w io.Writer, t *watch.Tailer, once bool, interval time.Duration) error {
	t.SetEventCap(0)
	printed := 0
	headerDone := false
	for {
		st, err := t.Refresh()
		if err != nil {
			return err
		}
		if !headerDone && st.Meta.SessionID != "" {
			if err := printWatchHeader(w, st.Meta); err != nil {
				return err
			}
			headerDone = true
		}
		for ; printed < len(st.Events); printed++ {
			ev := st.Events[printed]
			if _, err := fmt.Fprintf(w, "%s  %-13s %s\n", ev.Ts.Local().Format("15:04:05"), ev.Type, ev.Summary); err != nil {
				return err
			}
		}
		if once || (st.Meta.Status != "" && st.Meta.Status != session.StatusRunning) {
			return printWatchFooter(w, st)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func printWatchHeader(w io.Writer, m session.Meta) error {
	if _, err := fmt.Fprintf(w, "session %s · agent=%s provider=%s model=%s\n", m.SessionID, m.Agent, m.Provider, m.Model); err != nil {
		return err
	}
	if m.TraceID != "" {
		if _, err := fmt.Fprintf(w, "trace_id %s\n", m.TraceID); err != nil {
			return err
		}
	}
	if m.ClaudeSessionID != "" {
		if _, err := fmt.Fprintf(w, "claude_session_id %s\n", m.ClaudeSessionID); err != nil {
			return err
		}
	}
	return nil
}

func printWatchFooter(w io.Writer, st watch.State) error {
	c := st.Counts
	_, err := fmt.Fprintf(w, "status=%s · %d turns · %d tool calls (%d failed, %d denied) · %d errors\n",
		st.Meta.Status, c.Iterations, c.ToolCalls, c.ToolErrors, c.ToolDenied, c.Errors)
	return err
}
