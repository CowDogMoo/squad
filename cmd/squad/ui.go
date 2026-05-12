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
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/cowdogmoo/squad/config"
	"github.com/cowdogmoo/squad/metrics"
	"github.com/cowdogmoo/squad/session"
	"github.com/cowdogmoo/squad/source"
	"github.com/cowdogmoo/squad/ui/app"
	"github.com/cowdogmoo/squad/ui/presets"
)

func newUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Interactive TUI for launching and monitoring agent runs",
		Long: `Launch the squad TUI: a Bubble Tea interface that shows active and
recent agent runs in a sidebar, a focused-run panel with live event tail,
and a polymorphic bottom pane for composing prompts, picking presets, or
launching new runs.

By default the TUI discovers + tails .squad/sessions/ under the current
working directory. Use --mock for a hand-crafted demo, or --sessions-dir
to point at a different sessions root.`,
		RunE: runUI,
	}
	cmd.Flags().Bool("mock", false, "Render hand-crafted mock runs instead of discovering disk sessions")
	cmd.Flags().String("sessions-dir", "", "Sessions root to watch (default: <cwd>/.squad/sessions)")
	cmd.Flags().String("working-dir", "", "Working directory for launched subprocesses (default: cwd)")
	cmd.Flags().String("agents-dir", "", "Extra directory to search for agents (prepended to configured sources)")
	return cmd
}

func runUI(cmd *cobra.Command, _ []string) error {
	useMock, _ := cmd.Flags().GetBool("mock")
	sessionsDir, _ := cmd.Flags().GetString("sessions-dir")
	workingDir, _ := cmd.Flags().GetString("working-dir")
	agentsDir, _ := cmd.Flags().GetString("agents-dir")
	if workingDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		workingDir = cwd
	}

	// Kick the LiteLLM pricing fetch off in the background so the launch
	// form's model typeahead has the live registry ready to suggest from.
	metrics.WarmPricing()

	var model app.App
	if useMock {
		model = app.New(app.MockRuns())
	} else {
		root := sessionsDir
		if root == "" {
			root = filepath.Join(workingDir, session.SessionsRoot)
		}
		var err error
		model, err = app.NewWithSessions(root, workingDir)
		if err != nil {
			return fmt.Errorf("discover sessions: %w", err)
		}
	}

	// Attach a presets store; missing file is fine, store starts empty.
	if path, err := presets.DefaultPath(); err == nil {
		if store, err := presets.Load(path); err == nil {
			model = model.WithPresets(store)
		}
	}

	// Discover available agents for the launch form typeahead. Failures
	// here are non-fatal — the form falls back to free-text entry.
	if names := discoverAgents(agentsDir); len(names) > 0 {
		model = model.WithAgents(names)
	}

	prog := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(cmd.Context()),
	)
	_, err := prog.Run()
	return err
}

// discoverAgents returns sorted names of all agents reachable through the
// configured source manager. Errors are swallowed — the launch form falls
// back to free-text entry when the list is empty.
func discoverAgents(extraDir string) []string {
	cfg, _, err := config.Load()
	if err != nil || cfg == nil {
		return nil
	}
	if extraDir != "" {
		if abs, err := filepath.Abs(extraDir); err == nil {
			cfg.Agents.LocalPaths = append([]string{abs}, cfg.Agents.LocalPaths...)
		}
	}
	mgr, err := source.NewManager(cfg)
	if err != nil {
		return nil
	}
	infos, err := mgr.ListAgents()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	sort.Strings(names)
	return names
}
