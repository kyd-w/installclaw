//go:build darwin

package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessMonitor monitors a process for CPU and network activity on macOS
type ProcessMonitor struct {
	pid        int
	lastCPU    float64
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

	// Get CPU usage using ps command
	cpuPercent, err = pm.getCPUUsage()
	if err != nil {
		cpuPercent = 0
	}

	// For network, we'll use output activity as a proxy
	// since per-process network stats are complex on macOS
	netBytesPerSec = 0

	pm.lastTime = now
	return cpuPercent, netBytesPerSec, nil
}

// getCPUUsage calculates CPU usage for the process using ps command
func (pm *ProcessMonitor) getCPUUsage() (float64, error) {
	// Use ps to get CPU percentage
	cmd := exec.Command("ps", "-p", strconv.Itoa(pm.pid), "-o", "%cpu")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse output: "%CPU\n 12.5"
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return 0, fmt.Errorf("invalid ps output")
	}

	cpuStr := strings.TrimSpace(lines[1])
	cpu, err := strconv.ParseFloat(cpuStr, 64)
	if err != nil {
		return 0, err
	}

	return cpu, nil
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
	cmd := exec.CommandContext(ctx, "sh", "-c", e.command)

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
	outputDone := make(chan struct{})

	// Read stdout and stderr in goroutines
	go func() {
		defer close(outputChan)
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			outputChan <- scanner.Text()
		}
	}()

	go func() {
		defer close(outputDone)
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
	lastOutputTime := time.Now()

	// Hard timeout timer
	hardTimeout := time.NewTimer(e.timeout)
	defer hardTimeout.Stop()

	// Activity check ticker
	checkTicker := time.NewTicker(e.checkInterval)
	defer checkTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.killProcessTree(pid)
			result.Error = "context cancelled"
			result.TimedOut = true
			result.KillReason = "context_cancelled"
			goto done

		case <-hardTimeout.C:
			e.killProcessTree(pid)
			timedOut = true
			killReason = "hard_timeout"
			goto done

		case <-checkTicker.C:
			// Check if process is still running
			if cmd.ProcessState != nil {
				goto done
			}

			// Check activity using ps command
			cpu, _, err := monitor.GetActivity()
			if err != nil {
				// Process might have finished
				if cmd.ProcessState != nil {
					goto done
				}
				// If ps fails, rely on output activity
				continue
			}

			// Check if process is active
			timeSinceOutput := time.Since(lastOutputTime)
			isActive := cpu > e.activityThreshold ||
				timeSinceOutput < 5*time.Second

			if isActive {
				idleStart = time.Now()
			} else {
				idleDuration := time.Since(idleStart)
				if idleDuration >= e.idleTimeout {
					e.killProcessTree(pid)
					timedOut = true
					killReason = fmt.Sprintf("idle_timeout (no activity for %v, cpu=%.2f%%, last_output=%v ago)",
						idleDuration, cpu, timeSinceOutput)
					goto done
				}
			}

		case line, ok := <-outputChan:
			if ok {
				outputLines = append(outputLines, line)
				lastOutputTime = time.Now()
				idleStart = time.Now()
			}
		}
	}

done:
	// Wait for process to finish
	cmd.Wait()

	// Wait for stderr to finish
	<-outputDone

	// Collect remaining output with timeout
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

// killProcessTree kills a process and all its children on macOS
func (e *SmartCommandExecutor) killProcessTree(pid int) error {
	// Use pkill to kill process tree
	exec.Command("pkill", "-P", strconv.Itoa(pid)).Run()

	// Send SIGTERM to main process
	process, err := os.FindProcess(pid)
	if err == nil {
		process.Signal(syscall.SIGTERM)
	}

	// Wait a bit for graceful shutdown
	time.Sleep(500 * time.Millisecond)

	// Force kill if still running
	process.Signal(syscall.SIGKILL)

	return nil
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
