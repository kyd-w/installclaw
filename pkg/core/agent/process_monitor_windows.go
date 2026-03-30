//go:build windows

package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ProcessMonitor monitors a process for CPU and network activity (Windows)
type ProcessMonitor struct {
	pid        int
	lastCPU    int64
	lastTime   time.Time
	mu         sync.Mutex
}

// NewProcessMonitor creates a new process monitor
func NewProcessMonitor(pid int) *ProcessMonitor {
	return &ProcessMonitor{
		pid:      pid,
		lastTime: time.Now(),
	}
}

// GetActivity returns the CPU usage percentage and network bytes per second
func (pm *ProcessMonitor) GetActivity() (cpuPercent float64, netBytesPerSec uint64, err error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(pm.lastTime).Seconds()
	if elapsed < 0.1 {
		elapsed = 0.1
	}

	// Get CPU usage using Windows API (simplified - uses tasklist as fallback)
	cpuPercent, err = pm.getCPUUsage(elapsed)
	if err != nil {
		cpuPercent = 0
	}

	// Network usage - simplified on Windows, just check if process is running
	netBytesPerSec = 0

	pm.lastTime = now
	return cpuPercent, netBytesPerSec, nil
}

// getCPUUsage calculates CPU usage using tasklist command
func (pm *ProcessMonitor) getCPUUsage(elapsed float64) (float64, error) {
	// Use tasklist to get CPU time (simplified approach)
	cmd := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pm.pid), "/FO", "CSV", "/V")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse output to find CPU time
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, fmt.Sprintf(`"%d"`, pm.pid)) {
			// Found the process, assume it's active
			return 1.0, nil // Report low activity
		}
	}

	return 0, nil
}

// SmartCommandExecutor executes commands with smart timeout based on activity
type SmartCommandExecutor struct {
	command           string
	timeout           time.Duration
	idleTimeout       time.Duration
	checkInterval     time.Duration
	activityThreshold float64
	netThreshold      uint64
}

// SmartCommandConfig configures the smart command executor
type SmartCommandConfig struct {
	Command           string
	Timeout           time.Duration
	IdleTimeout       time.Duration
	CheckInterval     time.Duration
	ActivityThreshold float64
	NetThreshold      uint64
}

// NewSmartCommandExecutor creates a new smart command executor
func NewSmartCommandExecutor(cfg SmartCommandConfig) *SmartCommandExecutor {
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 30 * time.Second
	}
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = 2 * time.Second
	}
	if cfg.ActivityThreshold == 0 {
		cfg.ActivityThreshold = 0.5
	}
	if cfg.NetThreshold == 0 {
		cfg.NetThreshold = 1024
	}

	return &SmartCommandExecutor{
		command:           cfg.Command,
		timeout:           cfg.Timeout,
		idleTimeout:       cfg.IdleTimeout,
		checkInterval:     cfg.CheckInterval,
		activityThreshold: cfg.ActivityThreshold,
		netThreshold:      cfg.NetThreshold,
	}
}

// ExecuteResult contains the result of command execution
type ExecuteResult struct {
	Success    bool
	Output     string
	Error      string
	Duration   time.Duration
	TimedOut   bool
	KillReason string
}

// Execute runs the command with smart timeout
func (e *SmartCommandExecutor) Execute(ctx context.Context) *ExecuteResult {
	start := time.Now()
	result := &ExecuteResult{}

	// Create command
	cmd := exec.CommandContext(ctx, "cmd", "/c", e.command)

	// Get output pipes
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		result.Error = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return result
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		result.Error = fmt.Sprintf("failed to create stderr pipe: %v", err)
		return result
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		result.Error = fmt.Sprintf("failed to start command: %v", err)
		return result
	}

	pid := cmd.Process.Pid
	monitor := NewProcessMonitor(pid)

	// Channel to collect output
	outputChan := make(chan string, 100)

	// Read stdout and stderr in goroutines
	go func() {
		defer close(outputChan)
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			outputChan <- scanner.Text()
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			outputChan <- "[stderr] " + scanner.Text()
		}
	}()

	// Monitor loop
	idleStart := time.Now()
	var outputLines []string
	timedOut := false
	killReason := ""
	lastOutputTime := time.Now() // Track last output time as activity indicator

	// Hard timeout timer
	hardTimeout := time.NewTimer(e.timeout)
	defer hardTimeout.Stop()

	// Activity check ticker
	checkTicker := time.NewTicker(e.checkInterval)
	defer checkTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled
			killProcessWindows(cmd.Process)
			result.Error = "context cancelled"
			result.TimedOut = true
			result.KillReason = "context_cancelled"
			goto done

		case <-hardTimeout.C:
			// Hard timeout reached
			killProcessWindows(cmd.Process)
			timedOut = true
			killReason = "hard_timeout"
			goto done

		case <-checkTicker.C:
			// Check activity
			cpu, netBytes, err := monitor.GetActivity()
			if err != nil {
				// Process might have finished
				if cmd.ProcessState != nil {
					goto done
				}
				continue
			}

			// Check if process is active
			// Consider output as activity - if we received output recently, process is alive
			timeSinceOutput := time.Since(lastOutputTime)
			isActive := cpu > e.activityThreshold ||
				netBytes > e.netThreshold ||
				timeSinceOutput < 5*time.Second // Any output in last 5 seconds = active

			if isActive {
				// Reset idle timer
				idleStart = time.Now()
			} else {
				// Check if idle timeout reached
				idleDuration := time.Since(idleStart)
				if idleDuration >= e.idleTimeout {
					// Process is idle for too long
					killProcessWindows(cmd.Process)
					timedOut = true
					killReason = fmt.Sprintf("idle_timeout (no activity for %v, last_output=%v ago)",
						idleDuration, timeSinceOutput)
					goto done
				}
			}

		case line, ok := <-outputChan:
			if ok {
				outputLines = append(outputLines, line)
				// Any output means process is active
				lastOutputTime = time.Now()
				idleStart = time.Now()
			}
		}
	}

done:
	// Wait for process to finish
	cmd.Wait()

	// Collect remaining output
	timeout := time.After(1 * time.Second)
collect:
	for {
		select {
		case line, ok := <-outputChan:
			if !ok {
				break collect
			}
			outputLines = append(outputLines, line)
		case <-timeout:
			break collect
		}
	}

	result.Output = strings.Join(outputLines, "\n")
	result.Duration = time.Since(start)
	result.TimedOut = timedOut
	result.KillReason = killReason

	if cmd.ProcessState != nil {
		result.Success = cmd.ProcessState.Success()
		if !result.Success && result.Error == "" {
			result.Error = fmt.Sprintf("exit code: %d", cmd.ProcessState.ExitCode())
		}
	}

	return result
}

// killProcessWindows kills a process on Windows
func killProcessWindows(process *os.Process) error {
	if process == nil {
		return nil
	}

	// Use taskkill to kill the process tree
	exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", process.Pid)).Run()
	return process.Kill()
}

// ExecuteWithSmartTimeout is a convenience function for smart command execution
func ExecuteWithSmartTimeout(ctx context.Context, command string, timeout, idleTimeout time.Duration) *ExecuteResult {
	executor := NewSmartCommandExecutor(SmartCommandConfig{
		Command:     command,
		Timeout:     timeout,
		IdleTimeout: idleTimeout,
	})
	return executor.Execute(ctx)
}
