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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/cowdogmoo/squad/ui/app"
)

func newUICmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Interactive TUI for launching and monitoring agent runs",
		Long: `Launch the squad TUI: a Bubble Tea interface that shows active and
recent agent runs in a sidebar, a focused-run panel with live event tail,
and a polymorphic bottom pane for composing prompts, picking presets, or
launching new runs.

This early build ships with mock runs so the layout is visible. Live
session tailing and subprocess launch land in subsequent commits.`,
		RunE: runUI,
	}
	return cmd
}

func runUI(cmd *cobra.Command, _ []string) error {
	model := app.New(app.MockRuns())
	prog := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(cmd.Context()),
	)
	_, err := prog.Run()
	return err
}
