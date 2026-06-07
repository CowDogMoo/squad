//go:build windows

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// taskPath is the Task Scheduler folder + task name. Per-user task lives in
// the per-user task store; no admin rights are required to register it.
const taskPath = `\Squad\routined`

// taskNameLeaf is the leaf task name used in Get-/Stop-/Unregister-ScheduledTask
// commands (those cmdlets take -TaskName and -TaskPath separately).
const (
	taskNameLeaf = "routined"
	taskFolder   = `\Squad\`
)

type taskService struct {
	home    string
	logPath string
}

// New returns the Windows Task Scheduler service implementation.
func New() Service {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "squad", "Logs")
	if logDir == filepath.Join("", "squad", "Logs") { // LOCALAPPDATA unset
		logDir = filepath.Join(home, "AppData", "Local", "squad", "Logs")
	}
	return &taskService{
		home:    home,
		logPath: filepath.Join(logDir, "routined.log"),
	}
}

func (s *taskService) Install(daemonBinary string, opts InstallOptions) error {
	if daemonBinary == "" {
		return errors.New("daemon binary path required")
	}
	abs, err := filepath.Abs(daemonBinary)
	if err != nil {
		return fmt.Errorf("resolve daemon binary: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("daemon binary %s not executable: %w", abs, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.logPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Register the task. -Force replaces any prior registration so this is
	// idempotent. The settings combo (-Hidden, -StartWhenAvailable,
	// -RestartCount, -RestartInterval, -ExecutionTimeLimit 0) approximates
	// what launchd KeepAlive and systemd Restart=always give us elsewhere.
	script := s.installScript(abs, opts.WakeSystem)
	if out, err := runPowerShell(script); err != nil {
		return fmt.Errorf("Register-ScheduledTask: %w (output: %s)", err, out)
	}
	// Kick off the task immediately so the daemon starts before next logon.
	startScript := fmt.Sprintf(`Start-ScheduledTask -TaskPath '%s' -TaskName '%s'`, taskFolder, taskNameLeaf)
	if out, err := runPowerShell(startScript); err != nil {
		return fmt.Errorf("Start-ScheduledTask: %w (output: %s)", err, out)
	}
	return nil
}

func (s *taskService) Uninstall() error {
	stop := fmt.Sprintf(`Stop-ScheduledTask -TaskPath '%s' -TaskName '%s' -ErrorAction SilentlyContinue`,
		taskFolder, taskNameLeaf)
	_, _ = runPowerShell(stop)
	rm := fmt.Sprintf(`Unregister-ScheduledTask -TaskPath '%s' -TaskName '%s' -Confirm:$false -ErrorAction SilentlyContinue`,
		taskFolder, taskNameLeaf)
	if _, err := runPowerShell(rm); err != nil {
		return fmt.Errorf("Unregister-ScheduledTask: %w", err)
	}
	return nil
}

func (s *taskService) Status() (Status, error) {
	st := Status{
		ServicePath: taskPath,
		LogPath:     s.logPath,
		State:       StateNotInstalled,
	}
	// `(Get-ScheduledTask ...).State` returns Ready/Running/Disabled, or the
	// command errors when the task is missing. -ErrorAction SilentlyContinue
	// converts the missing-task case to empty output.
	probe := fmt.Sprintf(`$t = Get-ScheduledTask -TaskPath '%s' -TaskName '%s' -ErrorAction SilentlyContinue; if ($t) { $t.State; $t.Actions[0].Execute }`,
		taskFolder, taskNameLeaf)
	out, err := runPowerShell(probe)
	if err != nil {
		return st, nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return st, nil
	}
	state := strings.TrimSpace(lines[0])
	switch strings.ToLower(state) {
	case "running":
		st.State = StateInstalledRunning
	case "ready", "queued":
		// Ready = waiting for a trigger; the daemon process may not currently
		// be alive but the schedule is armed. We report stopped so users see
		// when the daemon needs to be started manually.
		st.State = StateInstalledStopped
	case "disabled":
		st.State = StateInstalledStopped
	default:
		st.State = StateInstalledStopped
	}
	if len(lines) > 1 {
		st.DaemonBinary = strings.TrimSpace(lines[1])
	}
	return st, nil
}

// TailLogs streams the daemon log file under %LOCALAPPDATA%\squad\Logs\.
func (s *taskService) TailLogs(ctx context.Context, w io.Writer, follow bool) error {
	return tailFile(ctx, w, s.logPath, follow)
}

// installScript returns the PowerShell payload that Register-ScheduledTasks
// the routines daemon. Kept as a method so tests can assert on its content
// without shelling out.
//
// When wakeSystem is true, -WakeToRun is added to the settings so the OS
// will wake the machine to keep the daemon supervised. As on macOS, this
// covers daemon liveness — not per-routine wake-to-fire.
func (s *taskService) installScript(daemonBinary string, wakeSystem bool) string {
	var b bytes.Buffer
	fmt.Fprintf(&b, `$action = New-ScheduledTaskAction -Execute '%s' -Argument 'routined --log-file "%s"'`+"\n",
		psEscape(daemonBinary), psEscape(s.logPath))
	fmt.Fprintln(&b, `$trigger = New-ScheduledTaskTrigger -AtLogon -User $env:USERNAME`)
	settings := `-Hidden -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Days 0) -MultipleInstances IgnoreNew -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries`
	if wakeSystem {
		settings += ` -WakeToRun`
	}
	fmt.Fprintln(&b, `$settings = New-ScheduledTaskSettingsSet `+settings)
	fmt.Fprintf(&b, `Register-ScheduledTask -TaskPath '%s' -TaskName '%s' -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null`+"\n",
		taskFolder, taskNameLeaf)
	return b.String()
}

// psEscape escapes a PowerShell single-quoted string by doubling single
// quotes. Paths from os.UserHomeDir() rarely contain quotes but the install
// path may pass user-supplied values in the future.
func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// runPowerShell executes script via `powershell.exe -NoProfile -Command`.
// Returns combined stdout+stderr so callers can include it in error messages.
func runPowerShell(script string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	return cmd.CombinedOutput()
}
