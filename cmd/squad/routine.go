package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cowdogmoo/squad/routine"
	"github.com/cowdogmoo/squad/routine/daemon"
	"github.com/cowdogmoo/squad/routine/service"
	"github.com/spf13/cobra"
)

// jsonUnmarshal is a thin alias so I don't pull encoding/json in every file
// that touches session meta and to keep import discovery readable.
var jsonUnmarshal = json.Unmarshal

// newRoutineCmd builds the `squad routine` command tree.
func newRoutineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routine",
		Short: "Manage scheduled, unattended agent runs",
		Long: `Routines are saved agent invocations on a cron schedule.

A routine fires automatically via the squad routines daemon (squad routined),
which is installed as a per-user OS service so you don't need a terminal open.
Manifests live in two scopes:

  - global:  $XDG_CONFIG_HOME/squad/routines/<id>.yaml
  - per-repo: <repo>/.squad/routines/<id>.yaml (checked into git)

See "squad routine create --help" for fields.`,
	}
	cmd.AddCommand(
		newRoutineCreateCmd(),
		newRoutineListCmd(),
		newRoutineShowCmd(),
		newRoutineDeleteCmd(),
		newRoutineEnableCmd(true),
		newRoutineEnableCmd(false),
		newRoutineRunNowCmd(),
		newRoutineWatchCmd(),
		newRoutineUnwatchCmd(),
		newRoutineRootsCmd(),
		newRoutineDoctorCmd(),
		newRoutineRepairCmd(),
		newRoutineLogsCmd(),
		newRoutineHistoryCmd(),
	)
	return cmd
}

func newRoutineCreateCmd() *cobra.Command {
	var (
		agent        string
		schedule     string
		prompt       string
		workingDir   string
		provider     string
		model        string
		maxCost      float64
		maxIter      int
		vars         []string
		disabled     bool
		scope        string
		repoOverride string
		catchup      string
		wakeSystem   bool
	)
	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new routine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			id := args[0]
			if err := routine.ValidateID(id); err != nil {
				return err
			}
			ref, err := resolveCreateRef(id, scope, repoOverride)
			if err != nil {
				return err
			}
			r := &routine.Routine{
				ID:            id,
				Agent:         agent,
				Schedule:      schedule,
				Prompt:        prompt,
				WorkingDir:    workingDir,
				Provider:      provider,
				Model:         model,
				MaxCost:       maxCost,
				MaxIterations: maxIter,
				Vars:          parseVars(vars),
				Enabled:       !disabled,
				WakeSystem:    wakeSystem,
				Catchup:       routine.CatchupPolicy(catchup),
				CreatedAt:     time.Now().UTC(),
			}
			store := routine.NewStore()
			entry, err := store.Create(ref, r)
			if err != nil {
				return err
			}
			// For per-repo routines, ensure the containing root is watched so
			// the daemon picks it up without manual `routine watch`.
			if ref.Scope == routine.ScopeRepo {
				if _, _, err := routine.AddRoot(ref.Root); err != nil {
					return fmt.Errorf("auto-watch repo root: %w", err)
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created %s (%s)\n  manifest: %s\n",
				ref.Qualified(), ref.Display(), entry.ManifestPath)
			// First-routine UX: install the OS service so the user doesn't
			// have to think about plists / unit files / Task Scheduler. If
			// the platform isn't supported yet, warn but don't fail — the
			// manifest is still valid and `squad routined` can be run
			// manually until that platform's installer lands. The install is
			// idempotent and uses the union wake_system across all routines,
			// so creating any routine with wake_system: true reconciles the
			// daemon artifact automatically.
			if msg, err := ensureServiceInstalled(store); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
			} else if msg != "" {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), msg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Agent name (required)")
	cmd.Flags().StringVar(&schedule, "schedule", "", "Cron expression or '@every <duration>' (required)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "Prompt to pass to the agent")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory (global: required absolute path; repo: defaults to repo root)")
	cmd.Flags().StringVar(&provider, "provider", "", "Provider override")
	cmd.Flags().StringVar(&model, "model", "", "Model override")
	cmd.Flags().Float64Var(&maxCost, "max-cost", 0, "Per-fire cost cap in USD (0 = inherit)")
	cmd.Flags().IntVar(&maxIter, "max-iterations", 0, "Per-fire iteration cap (0 = inherit)")
	cmd.Flags().StringArrayVar(&vars, "var", nil, "Template variable KEY=VALUE (repeatable)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Create the routine disabled")
	cmd.Flags().StringVar(&scope, "scope", "", "Scope: global | repo (default: repo when cwd is inside .squad/, else global)")
	cmd.Flags().StringVar(&repoOverride, "repo", "", "Repo root for --scope=repo (default: current working directory)")
	cmd.Flags().StringVar(&catchup, "catchup", "", "Missed-fire policy: fire-once (default) | skip")
	cmd.Flags().BoolVar(&wakeSystem, "wake-system", false, "Ask the OS to wake the machine from sleep to keep the daemon supervised (macOS/Windows only)")
	_ = cmd.MarkFlagRequired("agent")
	_ = cmd.MarkFlagRequired("schedule")
	return cmd
}

func newRoutineListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all routines across global and watched repo roots",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			store := routine.NewStore()
			entries, err := store.LoadAll()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No routines. Create one with `squad routine create`.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "SCOPE\tID\tSCHEDULE\tAGENT\tENABLED\tNEXT-FIRE\tLAST-STATUS")
			now := time.Now()
			for _, e := range entries {
				st, _ := routine.LoadState(e.StatePath)
				nf := routine.NextFire(e.Routine.Schedule, now)
				next := "-"
				if !nf.IsZero() {
					next = nf.Format(time.RFC3339)
				}
				enabled := "yes"
				if !e.Routine.Enabled {
					enabled = "no"
				}
				status := "-"
				if st != nil && st.LastStatus != "" {
					status = string(st.LastStatus)
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					e.Ref.Display(), e.Ref.ID, e.Routine.Schedule, e.Routine.Agent, enabled, next, status)
			}
			return tw.Flush()
		},
	}
}

func newRoutineShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show full details for a routine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			entry, err := resolveExistingRef(store, args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printRoutineSummary(out, entry)
			printRoutineOptional(out, entry.Routine)
			printRoutineFooter(out, entry)
			st, _ := routine.LoadState(entry.StatePath)
			printRoutineState(out, st)
			printNextFire(out, entry.Routine.Schedule)
			return nil
		},
	}
}

// printRoutineSummary prints the fixed-position fields every routine has.
func printRoutineSummary(out io.Writer, entry routine.Entry) {
	r := entry.Routine
	_, _ = fmt.Fprintf(out, "ID:          %s\n", r.ID)
	_, _ = fmt.Fprintf(out, "Scope:       %s\n", entry.Ref.Display())
	_, _ = fmt.Fprintf(out, "Agent:       %s\n", r.Agent)
	_, _ = fmt.Fprintf(out, "Schedule:    %s\n", r.Schedule)
	_, _ = fmt.Fprintf(out, "Enabled:     %v\n", r.Enabled)
	_, _ = fmt.Fprintf(out, "Working dir: %s\n", workingDirForDisplay(entry))
}

// printRoutineOptional prints fields that only appear when set.
func printRoutineOptional(out io.Writer, r *routine.Routine) {
	if r.Prompt != "" {
		_, _ = fmt.Fprintf(out, "Prompt:      %s\n", r.Prompt)
	}
	if r.Provider != "" {
		_, _ = fmt.Fprintf(out, "Provider:    %s\n", r.Provider)
	}
	if r.Model != "" {
		_, _ = fmt.Fprintf(out, "Model:       %s\n", r.Model)
	}
	if r.MaxCost > 0 {
		_, _ = fmt.Fprintf(out, "Max cost:    $%.2f\n", r.MaxCost)
	}
	if r.MaxIterations > 0 {
		_, _ = fmt.Fprintf(out, "Max iter:    %d\n", r.MaxIterations)
	}
	if len(r.Vars) > 0 {
		keys := make([]string, 0, len(r.Vars))
		for k := range r.Vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		_, _ = fmt.Fprintln(out, "Vars:")
		for _, k := range keys {
			_, _ = fmt.Fprintf(out, "  %s=%s\n", k, r.Vars[k])
		}
	}
}

// printRoutineFooter prints the static-position footer fields.
func printRoutineFooter(out io.Writer, entry routine.Entry) {
	r := entry.Routine
	_, _ = fmt.Fprintf(out, "Catchup:     %s\n", r.EffectiveCatchup())
	if r.WakeSystem {
		_, _ = fmt.Fprintln(out, "Wake system: yes")
	}
	_, _ = fmt.Fprintf(out, "Manifest:    %s\n", entry.ManifestPath)
	_, _ = fmt.Fprintf(out, "State file:  %s\n", entry.StatePath)
}

// printRoutineState prints the most recent fire's outcome when state has been
// recorded; a freshly created routine has none and the section is skipped.
func printRoutineState(out io.Writer, st *routine.State) {
	if st == nil || st.LastRun.IsZero() {
		return
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintf(out, "Last run:      %s (%s)\n", st.LastRun.Format(time.RFC3339), st.LastStatus)
	if st.LastError != "" {
		_, _ = fmt.Fprintf(out, "Last error:    %s\n", st.LastError)
	}
	if st.LastSessionID != "" {
		_, _ = fmt.Fprintf(out, "Last session:  %s\n", st.LastSessionID)
	}
	if st.LastDurationMs > 0 {
		_, _ = fmt.Fprintf(out, "Last duration: %dms\n", st.LastDurationMs)
	}
}

func printNextFire(out io.Writer, schedule string) {
	nf := routine.NextFire(schedule, time.Now())
	if nf.IsZero() {
		return
	}
	_, _ = fmt.Fprintf(out, "Next fire:     %s\n", nf.Format(time.RFC3339))
}

func newRoutineDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "delete <id>",
		Aliases: []string{"rm"},
		Short:   "Delete a routine and its state file",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			entry, err := resolveExistingRef(store, args[0])
			if err != nil {
				return err
			}
			if err := store.Delete(entry.Ref); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s\n", entry.Ref.Qualified())
			return nil
		},
	}
}

func newRoutineEnableCmd(enable bool) *cobra.Command {
	verb := "enable"
	if !enable {
		verb = "disable"
	}
	return &cobra.Command{
		Use:   verb + " <id>",
		Short: strings.ToUpper(verb[:1]) + verb[1:] + " a routine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			entry, err := resolveExistingRef(store, args[0])
			if err != nil {
				return err
			}
			r := *entry.Routine
			r.Enabled = enable
			if _, err := store.Update(entry.Ref, &r); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%sd %s\n", strings.Title(verb), entry.Ref.Qualified()) //nolint:staticcheck // basic capitalization
			return nil
		},
	}
}

func newRoutineRunNowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run-now <id>",
		Short: "Fire a routine immediately, bypassing its schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cfg := configFromContext(cmd)
			if cfg == nil {
				return fmt.Errorf("config not available")
			}
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			entry, err := resolveExistingRef(store, args[0])
			if err != nil {
				return err
			}
			// Use the same fire path as the daemon — guarantees state file
			// bookkeeping matches scheduled fires.
			sched, err := routine.NewScheduler(store, daemon.BuildFireFn(cfg), routine.SchedulerOptions{})
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Firing %s...\n", entry.Ref.Qualified())
			return sched.RunNow(cmd.Context(), entry.Ref)
		},
	}
}

func newRoutineWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch [path]",
		Short: "Add a repo root to the daemon's watched-roots registry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			target := ""
			if len(args) == 1 {
				target = args[0]
			} else {
				cwd, err := os.Getwd()
				if err != nil {
					return err
				}
				target = cwd
			}
			abs, changed, err := routine.AddRoot(target)
			if err != nil {
				return err
			}
			if !changed {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already watching %s\n", abs)
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Watching %s\n", abs)
			return nil
		},
	}
}

func newRoutineUnwatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unwatch <path>",
		Short: "Remove a repo root from the daemon's watched-roots registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			changed, err := routine.RemoveRoot(args[0])
			if err != nil {
				return err
			}
			if !changed {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s was not in the registry\n", args[0])
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Unwatched %s\n", args[0])
			return nil
		},
	}
}

func newRoutineRootsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "roots",
		Short: "List the repo roots watched by the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			roots, err := routine.LoadRoots()
			if err != nil {
				return err
			}
			if len(roots) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(none)")
				return nil
			}
			for _, r := range roots {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), r)
			}
			return nil
		},
	}
}

// resolveCreateRef figures out where a `routine create` should write the
// manifest. Precedence:
//
//  1. Explicit --scope=repo (with optional --repo <path>)
//  2. Explicit --scope=global
//  3. Default: repo when cwd is inside a `.squad/` directory, else global
func resolveCreateRef(id, scope, repoOverride string) (routine.Ref, error) {
	wantRepo := false
	switch scope {
	case "":
		cwd, err := os.Getwd()
		if err != nil {
			return routine.Ref{}, err
		}
		wantRepo = routine.HasRepoRoutinesDir(cwd) || hasSquadDir(cwd)
	case "global":
		wantRepo = false
	case "repo":
		wantRepo = true
	default:
		return routine.Ref{}, fmt.Errorf("invalid scope %q (must be global or repo)", scope)
	}
	if !wantRepo {
		return routine.Ref{Scope: routine.ScopeGlobal, ID: id}, nil
	}
	root := repoOverride
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return routine.Ref{}, err
		}
		root = cwd
	}
	abs, err := absolutePath(root)
	if err != nil {
		return routine.Ref{}, err
	}
	return routine.Ref{Scope: routine.ScopeRepo, Root: abs, ID: id}, nil
}

// resolveExistingRef parses arg as either a qualified id (`scope:id`) or a
// bare id and looks up the corresponding entry in the store. Bare IDs are
// resolved with bias toward the watched repo containing cwd.
func resolveExistingRef(store *routine.Store, arg string) (routine.Entry, error) {
	if ref, ok := routine.ParseQualified(arg); ok {
		// We may need to fill in Root for repo-scoped qualified refs.
		candidates := store.FindByID(ref.ID)
		for _, c := range candidates {
			if c.Ref.Scope == ref.Scope {
				return c, nil
			}
		}
		return routine.Entry{}, fmt.Errorf("routine %s not found", ref.Qualified())
	}
	if err := routine.ValidateID(arg); err != nil {
		return routine.Entry{}, err
	}
	candidates := store.FindByID(arg)
	if len(candidates) == 0 {
		return routine.Entry{}, fmt.Errorf("routine %q not found", arg)
	}
	cwd, _ := os.Getwd()
	roots, _ := routine.LoadRoots()
	root, _ := routine.ContainingRoot(cwd, roots)
	refs := make([]routine.Ref, 0, len(candidates))
	for _, c := range candidates {
		refs = append(refs, c.Ref)
	}
	chosen, err := routine.Resolve(refs, root)
	if err != nil {
		return routine.Entry{}, err
	}
	for _, c := range candidates {
		if c.Ref == chosen {
			return c, nil
		}
	}
	return routine.Entry{}, fmt.Errorf("routine %q not found after resolution", arg)
}

func hasSquadDir(cwd string) bool {
	info, err := os.Stat(filepath.Join(cwd, ".squad"))
	return err == nil && info.IsDir()
}

func absolutePath(p string) (string, error) {
	if p == "" {
		return os.Getwd()
	}
	return filepath.Abs(p)
}

func newRoutineHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <id>",
		Short: "List sessions produced by a routine",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			store := routine.NewStore()
			if _, err := store.LoadAll(); err != nil {
				return err
			}
			entry, err := resolveExistingRef(store, args[0])
			if err != nil {
				return err
			}
			workingDir := entry.Routine.WorkingDir
			if workingDir == "" && entry.Ref.Scope == routine.ScopeRepo {
				workingDir = entry.Ref.Root
			}
			if workingDir == "" {
				return fmt.Errorf("routine has no working_dir set; nothing to scan")
			}
			sessionsDir := filepath.Join(workingDir, ".squad", "sessions")
			sessions, err := listSessionsForRoutine(sessionsDir, entry.Ref.Qualified(), entry.Routine.Agent)
			if err != nil {
				return err
			}
			st, _ := routine.LoadState(entry.StatePath)
			lastID := ""
			if st != nil {
				lastID = st.LastSessionID
			}
			if len(sessions) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No sessions found for this routine.")
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sessions dir: %s\n", sessionsDir)
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(tw, "SESSION\tCREATED\tSTATUS\tCOST\tITER\tNOTE")
			for _, s := range sessions {
				note := ""
				if s.ID == lastID {
					note = "(last recorded fire)"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t$%.4f\t%d\t%s\n",
					s.ID, s.Created.Format(time.RFC3339), s.Status, s.Cost, s.Iterations, note)
			}
			return tw.Flush()
		},
	}
}

// sessionEntry is a thin view of a session meta.json suitable for history
// listing without depending on the session package's full Meta type.
type sessionEntry struct {
	ID         string
	Created    time.Time
	Status     string
	Cost       float64
	Iterations int
}

// listSessionsForRoutine scans sessionsDir for sessions tied to qualifiedID.
// Matching is precise when meta.json carries routine_id (the daemon sets this
// for every fire); pre-routine_id sessions fall back to matching by
// agentName, so history works for sessions created before this feature
// landed.
func listSessionsForRoutine(sessionsDir, qualifiedID, agentName string) ([]sessionEntry, error) {
	items, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []sessionEntry
	for _, item := range items {
		if !item.IsDir() {
			continue
		}
		metaPath := filepath.Join(sessionsDir, item.Name(), "meta.json")
		entry, ok := readSessionMeta(metaPath)
		if !ok {
			continue
		}
		switch {
		case entry.routineID != "":
			if entry.routineID != qualifiedID {
				continue
			}
		case entry.agent != agentName:
			continue
		}
		out = append(out, sessionEntry{
			ID:         entry.id,
			Created:    entry.created,
			Status:     entry.status,
			Cost:       entry.cost,
			Iterations: entry.iterations,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// metaJSON is a minimal subset of session.Meta that history needs.
type metaJSON struct {
	id         string
	agent      string
	routineID  string
	created    time.Time
	status     string
	cost       float64
	iterations int
}

func readSessionMeta(path string) (metaJSON, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return metaJSON{}, false
	}
	var raw struct {
		SessionID  string    `json:"session_id"`
		Agent      string    `json:"agent"`
		RoutineID  string    `json:"routine_id"`
		Created    time.Time `json:"created"`
		Status     string    `json:"status"`
		Cost       float64   `json:"cost"`
		Iterations int       `json:"iterations"`
	}
	if err := jsonUnmarshal(data, &raw); err != nil {
		return metaJSON{}, false
	}
	return metaJSON{
		id:         raw.SessionID,
		agent:      raw.Agent,
		routineID:  raw.RoutineID,
		created:    raw.Created,
		status:     raw.Status,
		cost:       raw.Cost,
		iterations: raw.Iterations,
	}, true
}

func newRoutineLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail the routines daemon log",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			svc := service.New()
			return svc.TailLogs(cmd.Context(), cmd.OutOrStdout(), follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Stream new log lines as they arrive (Ctrl-C to stop)")
	return cmd
}

func newRoutineDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report daemon and service health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.SilenceUsage = true
			svc := service.New()
			st, err := svc.Status()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Service:    %s\n", st.State)
			_, _ = fmt.Fprintf(out, "Manifest:   %s\n", st.ServicePath)
			_, _ = fmt.Fprintf(out, "Daemon:     %s\n", nonEmptyOr(st.DaemonBinary, "(not configured)"))
			_, _ = fmt.Fprintf(out, "Logs:       %s\n", st.LogPath)

			roots, _ := routine.LoadRoots()
			_, _ = fmt.Fprintf(out, "Roots:      %d watched\n", len(roots))
			for _, r := range roots {
				_, _ = fmt.Fprintf(out, "  %s\n", r)
			}

			store := routine.NewStore()
			entries, _ := store.LoadAll()
			_, _ = fmt.Fprintf(out, "Routines:   %d configured\n", len(entries))

			if st.State == service.StateNotInstalled {
				_, _ = fmt.Fprintln(out)
				_, _ = fmt.Fprintln(out, "Run `squad routine repair` to install the service.")
			}
			return nil
		},
	}
}

// anyRoutineWantsWake reports whether any enabled routine has wake_system
// set. Disabled routines don't fire, so their wake preference doesn't matter.
func anyRoutineWantsWake(store *routine.Store) bool {
	for _, e := range store.Entries() {
		if e.Routine.Enabled && e.Routine.WakeSystem {
			return true
		}
	}
	return false
}

// daemonBinaryPath returns the path the OS service should invoke. By default
// it's the running squad binary. On Windows, prefer a sibling
// `squad-routined.exe` (GUI-subsystem, no flashing console) when it exists.
func daemonBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate squad binary: %w", err)
	}
	return preferDaemonBinary(exe), nil
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// workingDirForDisplay returns a printable working dir, surfacing the implicit
// repo-root default so users can see when it was not set explicitly.
func workingDirForDisplay(entry routine.Entry) string {
	wd := entry.Routine.WorkingDir
	if wd != "" {
		return wd
	}
	if entry.Ref.Scope == routine.ScopeRepo {
		return entry.Ref.Root + " (repo root)"
	}
	return "(unset)"
}
