//go:build linux

package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ProcessMonitor monitors a process for CPU and network activity
type ProcessMonitor struct {
	pid        int
	lastCPU    uint64
	lastTime   time.Time
	lastRxBytes uint64
	lastTxBytes uint64
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

	// Get CPU usage
	cpuPercent, err = pm.getCPUUsage(elapsed)
	if err != nil {
		cpuPercent = 0
	}

	// Get network usage
	netBytesPerSec, err = pm.getNetworkUsage(elapsed)
	if err != nil {
		netBytesPerSec = 0
	}

	pm.lastTime = now
	return cpuPercent, netBytesPerSec, nil
}

// getCPUUsage calculates CPU usage for the process and its children
func (pm *ProcessMonitor) getCPUUsage(elapsed float64) (float64, error) {
	// Get all PIDs (process + children)
	pids := pm.getAllChildren(pm.pid)
	pids = append([]int{pm.pid}, pids...)

	var totalCPU uint64
	for _, pid := range pids {
		cpu, err := pm.readProcessCPU(pid)
		if err == nil {
			totalCPU += cpu
		}
	}

	// Calculate percentage
	// On Linux, /proc/stat values are in "jiffies" (usually 100 per second)
	// We use the difference from last measurement
	cpuDiff := totalCPU - pm.lastCPU
	pm.lastCPU = totalCPU

	// Convert to percentage (assuming 100 Hz clock)
	hz := 100.0
	cpuPercent := (float64(cpuDiff) / hz) / elapsed * 100

	// Normalize by number of CPU cores
	cpuPercent = cpuPercent / float64(runtime.NumCPU())

	return cpuPercent, nil
}

// readProcessCPU reads CPU time from /proc/[pid]/stat
func (pm *ProcessMonitor) readProcessCPU(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	// Parse the stat file - fields 14 and 15 are utime and stime
	fields := strings.Fields(string(data))
	if len(fields) < 17 {
		return 0, fmt.Errorf("invalid stat format")
	}

	utime, _ := strconv.ParseUint(fields[13], 10, 64)
	stime, _ := strconv.ParseUint(fields[14], 10, 64)

	return utime + stime, nil
}

// getAllChildren recursively gets all child PIDs
func (pm *ProcessMonitor) getAllChildren(pid int) []int {
	var children []int

	// Read /proc/[pid]/task/[tid]/children or scan /proc for ppid
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return children
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		childPID, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Read the parent PID from /proc/[pid]/stat
		ppid := pm.getParentPID(childPID)
		if ppid == pid {
			children = append(children, childPID)
			// Recursively get children of this child
			children = append(children, pm.getAllChildren(childPID)...)
		}
	}

	return children
}

// getParentPID gets the parent PID of a process
func (pm *ProcessMonitor) getParentPID(pid int) int {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}

	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return 0
	}

	ppid, _ := strconv.Atoi(fields[3])
	return ppid
}

// getNetworkUsage calculates network usage for the process
// Note: This is an approximation based on system-wide network activity
// For per-process network stats, we would need to use /proc/net/tcp or cgroups
func (pm *ProcessMonitor) getNetworkUsage(elapsed float64) (uint64, error) {
	// Read system-wide network stats from /proc/net/dev
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, err
	}

	var totalRx, totalTx uint64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Inter-") || strings.HasPrefix(line, "face") {
			continue
		}

		// Parse: "eth0: rx_bytes rx_packets ... tx_bytes tx_packets ..."
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 17 {
			continue
		}

		// Skip loopback
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}

		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		totalRx += rx
		totalTx += tx
	}

	// Calculate bytes per second
	rxDiff := totalRx - pm.lastRxBytes
	txDiff := totalTx - pm.lastTxBytes
	pm.lastRxBytes = totalRx
	pm.lastTxBytes = totalTx

	totalDiff := rxDiff + txDiff
	bytesPerSec := uint64(float64(totalDiff) / elapsed)

	return bytesPerSec, nil
}

// SmartCommandExecutor executes commands with smart timeout based on activity
type SmartCommandExecutor struct {
	command           string
	timeout           time.Duration
	idleTimeout       time.Duration // Time without activity before timeout
	checkInterval     time.Duration
	activityThreshold float64 // CPU % threshold to consider as "active"
	netThreshold      uint64  // Network bytes/sec threshold
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
		cfg.ActivityThreshold = 0.5 // 0.5% CPU
	}
	if cfg.NetThreshold == 0 {
		cfg.NetThreshold = 1024 // 1KB/s
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
	Success   bool
	Output    string
	Error     string
	Duration  time.Duration
	TimedOut  bool
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
			e.killProcessTree(pid)
			result.Error = "context cancelled"
			result.TimedOut = true
			result.KillReason = "context_cancelled"
			goto done

		case <-hardTimeout.C:
			// Hard timeout reached
			e.killProcessTree(pid)
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
					e.killProcessTree(pid)
					timedOut = true
					killReason = fmt.Sprintf("idle_timeout (no activity for %v, cpu=%.2f%%, net=%d B/s, last_output=%v ago)",
						idleDuration, cpu, netBytes, timeSinceOutput)
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

// killProcessTree kills a process and all its children
func (e *SmartCommandExecutor) killProcessTree(pid int) error {
	// First, get all children
	monitor := NewProcessMonitor(pid)
	children := monitor.getAllChildren(pid)

	// Send SIGTERM to all
	pids := append([]int{pid}, children...)
	for _, p := range pids {
		process, err := os.FindProcess(p)
		if err == nil {
			process.Signal(syscall.SIGTERM)
		}
	}

	// Wait a bit for graceful shutdown
	time.Sleep(500 * time.Millisecond)

	// Force kill if still running
	for _, p := range pids {
		process, err := os.FindProcess(p)
		if err == nil {
			process.Signal(syscall.SIGKILL)
		}
	}

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
