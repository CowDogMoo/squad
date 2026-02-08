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
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cowdogmoo/squad/grading"
	"github.com/spf13/cobra"
)

func newGradeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grade [output.md]",
		Short: "Grade an agent run output",
		Long: `Grade an agent run based on the quality rubric in docs/agent-quality.md.

Computes automated scores for:
  - Report Quality (10%): Are required sections present?
  - Iteration Efficiency (15%): Did the agent stay within budget?

Finding Quality (50%) and Skip Discipline (25%) require manual review.

Examples:
  # Grade from a file
  squad grade output.md --agent go-review --iterations 15 --files 12

  # Grade from stdin
  cat output.md | squad grade - --agent go-review --iterations 15

  # View grade history
  squad grade --history --agent go-review

  # View stats for an agent
  squad grade --stats --agent go-review`,
		Args: cobra.MaximumNArgs(1),
		RunE: runGrade,
	}

	cmd.Flags().StringP("agent", "a", "", "Agent name (required for grading)")
	cmd.Flags().IntP("iterations", "i", 0, "Number of iterations used")
	cmd.Flags().IntP("files", "f", 0, "Number of source files in codebase")
	cmd.Flags().String("run-id", "", "Optional run identifier for tracking")
	cmd.Flags().Bool("save", true, "Save grade to history")
	cmd.Flags().Bool("history", false, "Show grade history")
	cmd.Flags().Bool("stats", false, "Show aggregate statistics")
	cmd.Flags().Int("limit", 10, "Limit history results")
	cmd.Flags().Bool("json", false, "Output as JSON")

	return cmd
}

func runGrade(cmd *cobra.Command, args []string) error {
	showHistory, _ := cmd.Flags().GetBool("history")
	showStats, _ := cmd.Flags().GetBool("stats")
	agentName, _ := cmd.Flags().GetString("agent")
	limit, _ := cmd.Flags().GetInt("limit")
	asJSON, _ := cmd.Flags().GetBool("json")

	store, err := grading.NewStore()
	if err != nil {
		return fmt.Errorf("failed to initialize grade store: %w", err)
	}

	// History mode
	if showHistory {
		return displayHistory(cmd, store, agentName, limit, asJSON)
	}

	// Stats mode
	if showStats {
		if agentName == "" {
			return fmt.Errorf("--agent is required for --stats")
		}
		return displayStats(cmd, store, agentName, asJSON)
	}

	// Grading mode
	if len(args) == 0 {
		return fmt.Errorf("output file required (use - for stdin)")
	}

	if agentName == "" {
		return fmt.Errorf("--agent is required")
	}

	iterations, _ := cmd.Flags().GetInt("iterations")
	fileCount, _ := cmd.Flags().GetInt("files")
	runID, _ := cmd.Flags().GetString("run-id")
	save, _ := cmd.Flags().GetBool("save")

	// Read input
	var content []byte
	if args[0] == "-" {
		content, err = readStdin()
	} else {
		content, err = os.ReadFile(args[0])
	}
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	// Parse and grade
	parser := grading.NewParser()
	parsed := parser.Parse(string(content))

	result := grading.ComputeGrade(parsed, grading.GradeOptions{
		Agent:      agentName,
		Iterations: iterations,
		FileCount:  fileCount,
		RunID:      runID,
	})

	// Save if requested
	if save {
		if err := store.Save(result); err != nil {
			return fmt.Errorf("failed to save grade: %w", err)
		}
	}

	// Output
	if asJSON {
		return outputJSON(cmd, result)
	}

	fmt.Fprint(cmd.OutOrStdout(), grading.FormatResult(result))
	return nil
}

func displayHistory(cmd *cobra.Command, store *grading.Store, agent string, limit int, asJSON bool) error {
	grades, err := store.List(agent, limit)
	if err != nil {
		return err
	}

	if len(grades) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No grades found.")
		return nil
	}

	if asJSON {
		return outputJSON(cmd, grades)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Grade History (showing %d of %d):\n\n", len(grades), len(grades))
	for _, g := range grades {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s  %-12s  Grade: %s  Score: %.0f%%  Iter: %d\n",
			g.Timestamp.Format("2006-01-02 15:04"),
			g.Agent,
			g.Grade,
			g.TotalScore,
			g.Iterations)
	}

	return nil
}

func displayStats(cmd *cobra.Command, store *grading.Store, agent string, asJSON bool) error {
	stats, err := store.Stats(agent)
	if err != nil {
		return err
	}

	if asJSON {
		return outputJSON(cmd, stats)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Statistics for %s:\n\n", stats.Agent)
	fmt.Fprintf(cmd.OutOrStdout(), "  Total Runs:    %d\n", stats.TotalRuns)
	fmt.Fprintf(cmd.OutOrStdout(), "  Latest Grade:  %s\n", stats.LatestGrade)
	fmt.Fprintf(cmd.OutOrStdout(), "  Avg Score:     %.1f%%\n", stats.AvgScore)
	fmt.Fprintf(cmd.OutOrStdout(), "  Avg Report:    %.1f%%\n", stats.AvgReportQuality)
	fmt.Fprintf(cmd.OutOrStdout(), "  Avg Efficiency: %.1f%%\n\n", stats.AvgIterationEfficiency)

	fmt.Fprintln(cmd.OutOrStdout(), "  Grade Distribution:")
	for _, grade := range []string{"A+", "A", "A-", "B+", "B", "B-", "C", "D", "F"} {
		if count, ok := stats.GradeCounts[grade]; ok && count > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s: %d\n", grade, count)
		}
	}

	return nil
}

func readStdin() ([]byte, error) {
	return os.ReadFile("/dev/stdin")
}

func outputJSON(cmd *cobra.Command, v any) error {
	return writeJSON(cmd.OutOrStdout(), v)
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
