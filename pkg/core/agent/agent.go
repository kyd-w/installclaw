package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/kyd-w/installclaw/pkg/core/dependencies"
)

// NewInstallAgent creates a new installation agent
func NewInstallAgent(provider AIProvider, cfg *AgentConfig) *InstallAgent {
	if cfg == nil {
		cfg = DefaultAgentConfig()
	}

	// Initialize dependency loader
	depLoader, err := dependencies.NewLoader(cfg.MemoryDir)
	if err != nil {
		depLoader, _ = dependencies.NewLoader(".installer/memory/dependencies")
	}

	return &InstallAgent{
		config:          cfg,
		provider:        provider,
		tools:           NewDefaultToolRegistry(),
		context:         &InstallContext{},
		conversation:    &Conversation{},
		state:           StateIdle,
		depLoader:       depLoader,
		depValidator:    dependencies.NewValidator(depLoader),
		depLearner:      dependencies.NewLearner(cfg.MemoryDir, depLoader),
		safetyValidator: NewCommandSafetyValidator(),
	}
}

// SetProgressCallback sets the progress callback
func (a *InstallAgent) SetProgressCallback(fn func(step, total int, message string)) {
	a.onProgress = fn
}

// SetToolCallCallback sets the tool call callback
func (a *InstallAgent) SetToolCallCallback(fn func(tool string, args map[string]interface{})) {
	a.onToolCall = fn
}

// SetCompleteCallback sets the completion callback
func (a *InstallAgent) SetCompleteCallback(fn func(ctx *InstallContext)) {
	a.onComplete = fn
}

// SetErrorCallback sets the error analysis callback
// The callback receives error details and analysis, and returns a decision on how to proceed
func (a *InstallAgent) SetErrorCallback(fn func(stepName, command, output, errMsg string, analysis *ErrorAnalysis) ErrorHandlingDecision) {
	a.onError = fn
}

// SetDangerousCommandCallback sets the callback for dangerous command confirmation
// The callback receives the command and reason, and returns true to allow execution
func (a *InstallAgent) SetDangerousCommandCallback(fn func(command, reason string) bool) {
	a.onDangerousCommand = fn
}

// SetNaturalInputCallbacks sets callbacks for multi-turn natural input handling
func (a *InstallAgent) SetNaturalInputCallbacks(
	clarificationFn func(stepName, question string) string,
	executionFailedFn func(stepName, cmd, output, errMsg string) NaturalInputFailedResponse,
) {
	a.onNaturalInputClarification = clarificationFn
	a.onNaturalInputExecutionFailed = executionFailedFn
}

// Install performs intelligent installation of a package
func (a *InstallAgent) Install(ctx context.Context, packageName string, options InstallOptions) (*InstallContext, error) {
	// Initialize context
	a.context = &InstallContext{
		PackageName:    packageName,
		Options:        options,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		CurrentStep:    0,
		Steps:          []InstallStep{},
		ExecutedSteps:  []StepResult{},
		FixHistory:     []FixAttempt{},
	}

	// Detect Linux distribution if on Linux
	if runtime.GOOS == "linux" {
		a.context.LinuxDistro = detectLinuxDistro()
		a.context.PackageManager = getPackageManager(a.context.LinuxDistro)
	}

	// ====== NEW: Environment Pre-validation ======
	if !options.SkipValidation && a.depValidator != nil {
		a.state = StateValidating

		// Validate dependencies recursively
		validationResult, err := a.depValidator.Validate(ctx, packageName, options.Version)
		if err != nil {
			// Validation error - continue with installation but log warning
			a.context.Warnings = append(a.context.Warnings, fmt.Sprintf("环境验证失败: %v", err))
		} else {
			// Handle validation result
			if !validationResult.CanProceed {
				a.state = StateBlocked
				a.context.Error = formatBlockers(validationResult.Blockers)
				return a.context, fmt.Errorf("环境不兼容:\n%s", a.context.Error)
			}

			// Add warnings to context
			a.context.Warnings = append(a.context.Warnings, validationResult.Warnings...)

			// Store dependencies to install
			a.context.DepsToInstall = validationResult.InstallOrder

			// Log system info
			if validationResult.SystemInfo != nil {
				a.context.SystemInfo = validationResult.SystemInfo
			}
		}
	}
	// ====== End Environment Pre-validation ======

	// Reset conversation
	a.conversation = &Conversation{}

	// Build system prompt
	systemPrompt := a.buildSystemPrompt()
	a.conversation.AddMessage("system", systemPrompt)

	// Build user request
	userPrompt := a.buildUserPrompt(packageName, options)
	a.conversation.AddMessage("user", userPrompt)

	// Single AI call to get complete installation plan with commands
	a.state = StateThinking
	response, err := a.provider.QueryWithHistory(ctx, a.conversation.Messages)
	if err != nil {
		return a.context, fmt.Errorf("AI query failed: %w", err)
	}

	// Add assistant response to history
	a.conversation.AddMessage("assistant", response)

	// Parse the complete installation response
	installPlan, err := a.parseInstallPlan(response)
	if err != nil {
		return a.context, fmt.Errorf("failed to parse installation plan: %w", err)
	}

	// Execute the plan automatically without further AI calls
	err = a.executePlan(ctx, installPlan)
	if err != nil {
		a.state = StateFailed
		a.context.Error = err.Error()
	}

	if a.onComplete != nil {
		a.onComplete(a.context)
	}

	return a.context, err
}

// InstallPlan represents a complete installation plan from AI
type InstallPlan struct {
	Detected    bool             `json:"detected"`
	Version     string           `json:"version"`
	InstallPath string           `json:"install_path"`
	Method      string           `json:"method"`
	Commands    []InstallCommand `json:"commands"`
	Success     bool             `json:"success"`
	Message     string           `json:"message"`
	Error       string           `json:"error"`
}

// InstallCommand represents a single command in the installation plan
type InstallCommand struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Type            string `json:"type"`
	Command         string `json:"command"`
	ContinueOnError bool   `json:"continue_on_error"`
}

// buildSystemPrompt builds the system prompt for the AI
func (a *InstallAgent) buildSystemPrompt() string {
	// Build system info string
	systemInfo := fmt.Sprintf(`You are an intelligent software installation agent. Analyze the request and provide a complete installation plan.

System Information:
- OS: %s
- Architecture: %s`, a.context.OS, a.context.Arch)

	// Add Linux-specific info
	if a.context.OS == "linux" {
		systemInfo += fmt.Sprintf(`
- Linux Distribution: %s
- Package Manager: %s`, a.context.LinuxDistro, a.context.PackageManager)
	}

	return systemInfo + `

Your Task:
Analyze the software installation request and respond with a SINGLE JSON object containing the complete installation plan.

Response Format (JSON only):
{
  "detected": true/false,
  "version": "detected or target version",
  "install_path": "path where software is/will be installed",
  "method": "package_manager|binary|script",
  "commands": [
    {
      "name": "step_name",
      "description": "What this step does",
      "type": "detect|download|install|verify",
      "command": "actual shell command",
      "continue_on_error": false
    }
  ],
  "success": true/false,
  "message": "Installation result message",
  "error": "Error message if failed"
}

Installation Guidelines:
1. For Windows: Use winget with --accept-source-agreements --accept-package-agreements flags
2. For macOS: Use brew or download dmg/pkg
3. For Linux: Use apt/dnf/pacman/snap/flatpak

CRITICAL: Dependency Detection
BEFORE installing any software, you MUST check if required dependencies are available:
- For npm packages: Check if node and npm are installed first
- For pip packages: Check if python and pip are installed first
- For brew packages: Check if brew is installed first

If a dependency is missing, you MUST install it first:
- Node.js on Windows: winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements
- Node.js on macOS: brew install node
- Node.js on Debian/Ubuntu: curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash - && sudo apt-get install -y nodejs
- Node.js on RHEL/CentOS/Fedora: curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash - && sudo yum install -y nodejs
- Python on Windows: winget install Python.Python.3.12 --accept-source-agreements --accept-package-agreements
- Python on macOS: brew install python3
- Python on Debian/Ubuntu: sudo apt-get install -y python3 python3-pip
- Python on RHEL/CentOS/Fedora: sudo yum install -y python3 python3-pip

CRITICAL: Linux Distribution Detection
Before using package manager commands, detect the Linux distribution:
- Check /etc/os-release for distribution info
- Use 'yum' or 'dnf' for RHEL/CentOS/Fedora/Amazon Linux
- Use 'apt' or 'apt-get' for Debian/Ubuntu
- Common detection: cat /etc/os-release | grep -E "^(ID|ID_LIKE)="

CRITICAL: Operating System Compatibility Matrix
When installing software, consider OS version limitations:
- CentOS 7 (glibc 2.17): Node.js max version is 16.x (NOT 18+, 20+, 22+)
- CentOS 7 EOL: mirrorlist.centos.org is offline, use vault.centos.org
- RHEL/CentOS 8+: Supports newer Node.js versions
- Ubuntu 18.04+: Supports all modern software versions
- When glibc dependency errors occur, suggest older compatible versions

Software Version Compatibility for CentOS 7:
- Node.js: Use 16.x or install via NVM (nvm install 16)
- Python: System has 2.7, use 3.6 from EPEL or pyenv
- Go: Use older version or install manually
- Docker: Use docker-ce from Docker's repo

Important Rules:
- ALWAYS check for required dependencies BEFORE attempting to install
- Provide ALL commands needed in a single response
- Include verification commands at the end
- Handle the case where software is already installed
- Use appropriate commands for the current OS
- Keep commands simple and reliable
- For winget install commands, ALWAYS include --accept-source-agreements --accept-package-agreements
- Installation commands may take several minutes - this is expected
- Verification may fail immediately after install due to PATH not being refreshed - set continue_on_error: true for verify steps

Example Response for installing nodejs on Windows:
{
  "detected": false,
  "version": "22.14.0",
  "install_path": "C:\\Program Files\\nodejs",
  "method": "package_manager",
  "commands": [
    {"name": "detect", "description": "Check if Node.js is installed", "type": "detect", "command": "node --version", "continue_on_error": true},
    {"name": "install", "description": "Install Node.js via winget", "type": "install", "command": "winget install OpenJS.NodeJS.LTS --accept-source-agreements --accept-package-agreements", "continue_on_error": false},
    {"name": "verify", "description": "Verify installation", "type": "verify", "command": "node --version && npm --version", "continue_on_error": false}
  ],
  "success": true,
  "message": "Node.js will be installed via winget"
}

Example Response for installing nodejs on RHEL/CentOS/Fedora Linux:
{
  "detected": false,
  "version": "22.14.0",
  "install_path": "/usr/bin/node",
  "method": "package_manager",
  "commands": [
    {"name": "detect_distro", "description": "Detect Linux distribution", "type": "detect", "command": "cat /etc/os-release | grep -E '^(ID|ID_LIKE)='", "continue_on_error": true},
    {"name": "detect", "description": "Check if Node.js is installed", "type": "detect", "command": "node --version", "continue_on_error": true},
    {"name": "install", "description": "Install Node.js via yum (RHEL/CentOS)", "type": "install", "command": "curl -fsSL https://rpm.nodesource.com/setup_lts.x | sudo bash - && sudo yum install -y nodejs", "continue_on_error": false},
    {"name": "verify", "description": "Verify installation", "type": "verify", "command": "node --version && npm --version", "continue_on_error": true}
  ],
  "success": true,
  "message": "Node.js will be installed via yum using NodeSource repository"
}`
}

// buildUserPrompt builds the user request prompt
func (a *InstallAgent) buildUserPrompt(packageName string, options InstallOptions) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Install: %s\n", packageName))

	if options.Version != "" {
		sb.WriteString(fmt.Sprintf("Required version: %s\n", options.Version))
	}
	if options.CustomPath != "" {
		sb.WriteString(fmt.Sprintf("Install location: %s\n", options.CustomPath))
	}
	if options.DryRun {
		sb.WriteString("Mode: DRY RUN (show commands only, do not execute)\n")
	}
	if options.Force {
		sb.WriteString("Mode: FORCE REINSTALL\n")
	}
	if options.SkipDeps {
		sb.WriteString("Mode: SKIP DEPENDENCIES\n")
	}

	sb.WriteString("\nProvide the complete installation plan in JSON format.")

	return sb.String()
}

// parseInstallPlan parses the AI response into an installation plan
func (a *InstallAgent) parseInstallPlan(response string) (*InstallPlan, error) {
	// Extract JSON from response
	jsonStart := strings.Index(response, "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Find matching closing brace
	depth := 0
	jsonEnd := -1
	for i, c := range response[jsonStart:] {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				jsonEnd = jsonStart + i + 1
				break
			}
		}
	}

	if jsonEnd == -1 {
		return nil, fmt.Errorf("incomplete JSON in response")
	}

	jsonStr := response[jsonStart:jsonEnd]

	var plan InstallPlan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &plan, nil
}

// executePlan executes the installation plan automatically
func (a *InstallAgent) executePlan(ctx context.Context, plan *InstallPlan) error {
	a.context.TotalSteps = len(plan.Commands)
	a.context.Steps = make([]InstallStep, len(plan.Commands))

	// Convert commands to steps
	for i, cmd := range plan.Commands {
		a.context.Steps[i] = InstallStep{
			ID:          cmd.Name,
			Name:        cmd.Name,
			Description: cmd.Description,
			Type:        StepType(cmd.Type),
			Command:     cmd.Command,
			Retryable:   !cmd.ContinueOnError,
		}
	}

	// Check if already installed and not forcing
	if plan.Detected && !a.context.Options.Force {
		a.state = StateCompleted
		a.context.Success = true
		a.context.FinalVersion = plan.Version
		a.context.FinalPath = plan.InstallPath
		a.context.Error = fmt.Sprintf("Already installed: %s at %s", plan.Version, plan.InstallPath)
		return nil
	}

	// Execute each command
	for i, cmd := range plan.Commands {
		a.context.CurrentStep = i + 1
		a.state = StateExecuting

		if a.onProgress != nil {
			a.onProgress(i+1, len(plan.Commands), cmd.Description)
		}

		// Skip execution in dry-run mode
		if a.context.Options.DryRun {
			a.context.ExecutedSteps = append(a.context.ExecutedSteps, StepResult{
				StepID:    cmd.Name,
				Success:   true,
				Output:    "[DRY RUN] Would execute: " + cmd.Command,
				Timestamp: currentTime(),
			})
			continue
		}

		// Execute the command
		result := a.executeShellCommand(ctx, cmd.Command, cmd.Name)

		stepResult := StepResult{
			StepID:    cmd.Name,
			Success:   result.Success,
			Output:    result.Output,
			Error:     result.Error,
			Duration:  0,
			Timestamp: currentTime(),
		}
		a.context.ExecutedSteps = append(a.context.ExecutedSteps, stepResult)

		// Handle verify step failure specially - it might fail due to env not refreshed
		if !result.Success && cmd.Type == "verify" {
			// Check if install step succeeded - if so, verification failure might be false negative
			installSucceeded := false
			for _, prevStep := range a.context.ExecutedSteps {
				if prevStep.StepID == "install" && prevStep.Success {
					installSucceeded = true
					break
				}
			}
			if installSucceeded {
				// Install succeeded, verification might fail due to env not refreshed
				// Log warning but don't fail the whole installation
				stepResult.Output = "[WARNING] Verification failed but install succeeded. Program may require restart or PATH refresh.\n" + result.Output
				stepResult.Success = true // Mark as success since install worked
				continue
			}
		}

		if !result.Success && !cmd.ContinueOnError {
			// Error occurred - use LLM-driven error recovery
			shouldRetry, retryErr := a.handleErrorWithLLM(ctx, cmd.Name, cmd.Command, result.Output, result.Error, cmd)

			if retryErr != nil {
				return retryErr
			}

			if shouldRetry {
				// Retry the current command
				a.context.CurrentStep--
				continue
			}

			// If we get here, LLM decided to abort
			return fmt.Errorf("step '%s' failed: %s\nOutput: %s", cmd.Name, result.Error, result.Output)
		}
	}

	// Installation complete
	a.state = StateCompleted
	a.context.Success = true
	a.context.FinalVersion = plan.Version
	a.context.FinalPath = plan.InstallPath

	return nil
}

// handleErrorWithLLM uses LLM to analyze errors and decide recovery strategy
// Returns (shouldRetry, error) - whether to retry the original command and any fatal error
func (a *InstallAgent) handleErrorWithLLM(ctx context.Context, stepName, command, output, errMsg string, cmd InstallCommand) (bool, error) {
	// Analyze the error with fix history context
	analysis, analyzeErr := a.AnalyzeErrorWithHistory(ctx, stepName, command, output, errMsg)
	if analyzeErr != nil {
		return false, fmt.Errorf("step '%s' failed: %s\nOutput: %s\n(Error analysis failed: %v)",
			stepName, errMsg, output, analyzeErr)
	}

	// If there's a user callback, let them decide
	if a.onError != nil {
		decision := a.onError(stepName, command, output, errMsg, analysis)

		switch decision.Action {
		case ActionAbort:
			return false, fmt.Errorf("step '%s' failed (user aborted): %s\nOutput: %s", stepName, errMsg, output)

		case ActionRetry:
			return true, nil

		case ActionSkip:
			return false, nil

		case ActionRunCustom:
			// Execute custom commands
			for _, customCmd := range decision.CustomCmds {
				customResult := a.executeShellCommand(ctx, customCmd, "custom_fix")
				a.recordFixAttempt(stepName, "custom", []string{customCmd}, customResult.Success, customResult.Output, customResult.Error, 1.0)

				if !customResult.Success {
					// Custom command failed, ask LLM what to do next
					return a.handleErrorWithLLM(ctx, stepName, customCmd, customResult.Output, customResult.Error, cmd)
				}
			}
			return true, nil

		case ActionNaturalInput:
			// User provided natural language solution - let LLM interpret and execute
			return a.handleNaturalInput(ctx, stepName, command, output, errMsg, decision.NaturalInput, cmd)
		}
	}

	// LLM-driven decision
	if !analysis.ShouldContinue {
		// LLM decided to abort
		return false, fmt.Errorf("step '%s' failed: %s\nLLM Reason: %s\nOutput: %s",
			stepName, errMsg, analysis.Reason, output)
	}

	// Execute LLM-suggested commands
	if len(analysis.Commands) > 0 {
		for _, fixCmd := range analysis.Commands {
			fixResult := a.executeShellCommand(ctx, fixCmd, "llm_fix")
			a.recordFixAttempt(stepName, analysis.ErrorType, []string{fixCmd}, fixResult.Success, fixResult.Output, fixResult.Error, analysis.Confidence)

			if !fixResult.Success {
				// Fix command failed, recursively ask LLM for next action
				return a.handleErrorWithLLM(ctx, stepName, fixCmd, fixResult.Output, fixResult.Error, cmd)
			}
		}
	}

	// Fix commands succeeded, retry original command
	return true, nil
}

// handleNaturalInput processes user's natural language solution input with multi-turn support
// Returns (shouldRetry, error) - whether to retry the original command and any fatal error
func (a *InstallAgent) handleNaturalInput(ctx context.Context, stepName, command, output, errMsg, naturalInput string, cmd InstallCommand) (bool, error) {
	// Initialize or get the natural input session
	session := a.getOrCreateNaturalInputSession(stepName, command, output, errMsg)

	// Add user's input to session
	session.AddUserInput(naturalInput)

	// Process the input (supports multi-turn)
	return a.processNaturalInputSession(ctx, session, cmd)
}

// getOrCreateNaturalInputSession creates or retrieves an existing session
func (a *InstallAgent) getOrCreateNaturalInputSession(stepName, command, output, errMsg string) *NaturalInputSession {
	// Check if we have an active session for this step
	if a.context.NaturalInputSession != nil && a.context.NaturalInputSession.StepName == stepName {
		return a.context.NaturalInputSession
	}

	// Create new session
	session := &NaturalInputSession{
		StepName:        stepName,
		OriginalCommand: command,
		OriginalOutput:  output,
		OriginalError:   errMsg,
		Turns:           []NaturalInputTurn{},
		CreatedAt:       time.Now(),
	}
	a.context.NaturalInputSession = session
	return session
}

// processNaturalInputSession processes a natural input session with multi-turn support
func (a *InstallAgent) processNaturalInputSession(ctx context.Context, session *NaturalInputSession, cmd InstallCommand) (bool, error) {
	maxTurns := 5 // Maximum number of conversation turns

	for turnNum := len(session.Turns); turnNum < maxTurns; turnNum++ {
		// Get the latest user input
		currentTurn := &session.Turns[len(session.Turns)-1]

		// Build prompt with full conversation history
		prompt := a.buildMultiTurnNaturalInputPrompt(session)

		// Query LLM to interpret
		response, err := a.provider.Query(ctx, prompt)
		if err != nil {
			currentTurn.LLMError = fmt.Sprintf("LLM query failed: %v", err)
			return false, fmt.Errorf("failed to interpret natural language: %w", err)
		}

		// Parse the response
		llmResult, parseErr := a.parseNaturalInputResponseFull(response)
		if parseErr != nil {
			currentTurn.LLMError = parseErr.Error()
			return false, fmt.Errorf("failed to parse interpretation: %w", parseErr)
		}

		// Store LLM interpretation
		currentTurn.LLMUnderstanding = llmResult.Understanding
		currentTurn.LLMCommands = llmResult.Commands
		currentTurn.LLMNotes = llmResult.Notes

		// Check if LLM needs clarification
		if llmResult.NeedsClarification {
			// LLM needs more information - request user input via callback
			if a.onNaturalInputClarification != nil {
				clarificationResponse := a.onNaturalInputClarification(session.StepName, llmResult.ClarificationQuestion)
				if clarificationResponse == "" {
					// User cancelled
					return false, fmt.Errorf("user cancelled during clarification")
				}
				// Add clarification as new turn and continue
				session.AddUserInput(clarificationResponse)
				continue
			}
			// No callback available, proceed with best effort
		}

		// Execute the interpreted commands
		allSucceeded := true
		var lastFailedCmd string
		var lastFailedOutput, lastFailedError string

		for _, interpretedCmd := range llmResult.Commands {
			result := a.executeShellCommand(ctx, interpretedCmd, "natural_input_fix")

			execRecord := CommandExecutionRecord{
				Command:   interpretedCmd,
				Success:   result.Success,
				Output:    result.Output,
				Error:     result.Error,
				Timestamp: time.Now(),
			}
			currentTurn.ExecutedCommands = append(currentTurn.ExecutedCommands, execRecord)

			if !result.Success {
				allSucceeded = false
				lastFailedCmd = interpretedCmd
				lastFailedOutput = result.Output
				lastFailedError = result.Error
				break
			}
		}

		if allSucceeded {
			// All commands succeeded, retry original
			return true, nil
		}

		// Some command failed - ask if user wants to continue with more input
		if a.onNaturalInputExecutionFailed != nil {
			userChoice := a.onNaturalInputExecutionFailed(session.StepName, lastFailedCmd, lastFailedOutput, lastFailedError)

			switch userChoice.Action {
			case NaturalInputActionContinue:
				// User wants to provide more input
				session.AddUserInput(userChoice.AdditionalInput)
				continue
			case NaturalInputActionAbort:
				return false, fmt.Errorf("user aborted: %s", lastFailedError)
			case NaturalInputActionRetry:
				// Retry without new input
				continue
			}
		}

		// No callback or callback returned retry - let LLM handle the new error
		return a.handleErrorWithLLM(ctx, session.StepName, lastFailedCmd, lastFailedOutput, lastFailedError, cmd)
	}

	// Max turns reached
	return false, fmt.Errorf("max conversation turns (%d) reached without resolution", maxTurns)
}

// buildNaturalInputPrompt builds the prompt for interpreting user's natural language solution
func (a *InstallAgent) buildNaturalInputPrompt(stepName, command, output, errMsg, naturalInput string) string {
	return fmt.Sprintf(`You are an intelligent command interpreter. Convert the user's natural language solution into executable shell commands.

System Information:
- OS: %s
- Architecture: %s
- Linux Distribution: %s
- Package Manager: %s

Failed Step: %s
Original Command: %s

Error Output:
%s

Error Message:
%s

User's Natural Language Solution:
%s

Your task:
1. Understand what the user is suggesting
2. Convert their suggestion into concrete, executable shell commands
3. Consider the OS and package manager when generating commands

Respond with a JSON object:
{
  "understanding": "Brief explanation of what the user wants to do",
  "commands": ["command1", "command2", ...],
  "confidence": 0.0-1.0,
  "notes": "Any important notes or warnings"
}

CRITICAL RULES:
1. Commands must be complete and executable
2. For multi-step operations, combine with && or use separate commands
3. Consider environment differences (PATH, permissions, etc.)
4. If the user's suggestion is unclear, generate safe commands that partially implement their intent
5. For complex user suggestions, break them down into multiple simpler commands
6. Always include full paths for commands if the environment might not have them in PATH

Examples:
- User: "try using nvm to install node" → ["curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.0/install.sh | bash", "source ~/.nvm/nvm.sh && nvm install node"]
- User: "use vault.centos.org instead" → ["sed -i 's/mirrorlist/#mirrorlist/g' /etc/yum.repos.d/*.repo", "sed -i 's|#baseurl=http://mirror.centos.org|baseurl=http://vault.centos.org|g' /etc/yum.repos.d/*.repo"]
- User: "install older version 16.x" → Change version in package install command

Respond with ONLY the JSON object, no additional text.`,
		a.context.OS, a.context.Arch, a.context.LinuxDistro, a.context.PackageManager,
		stepName, command, output, errMsg, naturalInput)
}

// NaturalInputResponse represents the LLM's interpretation of natural language input
type NaturalInputResponse struct {
	Understanding       string   `json:"understanding"`
	Commands            []string `json:"commands"`
	Confidence          float64  `json:"confidence"`
	Notes               string   `json:"notes"`
	NeedsClarification  bool     `json:"needs_clarification,omitempty"`
	ClarificationQuestion string `json:"clarification_question,omitempty"`
}

// parseNaturalInputResponse parses the LLM response for natural language interpretation
func (a *InstallAgent) parseNaturalInputResponse(response string) ([]string, error) {
	// Extract JSON from response
	jsonStart := strings.Index(response, "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Find matching closing brace
	depth := 0
	jsonEnd := -1
	for i, c := range response[jsonStart:] {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				jsonEnd = jsonStart + i + 1
				break
			}
		}
	}

	if jsonEnd == -1 {
		return nil, fmt.Errorf("incomplete JSON in response")
	}

	jsonStr := response[jsonStart:jsonEnd]

	var result NaturalInputResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(result.Commands) == 0 {
		return nil, fmt.Errorf("no commands generated from natural language input")
	}

	return result.Commands, nil
}

// parseNaturalInputResponseFull parses the LLM response and returns the full response object
func (a *InstallAgent) parseNaturalInputResponseFull(response string) (*NaturalInputResponse, error) {
	// Extract JSON from response
	jsonStart := strings.Index(response, "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Find matching closing brace
	depth := 0
	jsonEnd := -1
	for i, c := range response[jsonStart:] {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				jsonEnd = jsonStart + i + 1
				break
			}
		}
	}

	if jsonEnd == -1 {
		return nil, fmt.Errorf("incomplete JSON in response")
	}

	jsonStr := response[jsonStart:jsonEnd]

	var result NaturalInputResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &result, nil
}

// buildMultiTurnNaturalInputPrompt builds a prompt with full conversation history
func (a *InstallAgent) buildMultiTurnNaturalInputPrompt(session *NaturalInputSession) string {
	// Get the last user input
	lastTurn := &session.Turns[len(session.Turns)-1]

	// Build conversation history
	conversationHistory := session.GetConversationHistory()

	return fmt.Sprintf(`You are an intelligent command interpreter helping to fix an installation error.
The user is providing solutions in natural language. Convert their suggestions into executable shell commands.

System Information:
- OS: %s
- Architecture: %s
- Linux Distribution: %s
- Package Manager: %s

Original Failed Step: %s
Original Command: %s

Original Error Output:
%s

Original Error Message:
%s

=== CONVERSATION HISTORY ===
%s

=== CURRENT USER INPUT ===
%s

Your task:
1. Review the conversation history to understand what has been tried
2. Understand the user's latest suggestion in context
3. Convert their suggestion into concrete, executable shell commands
4. If previous commands failed, try a DIFFERENT approach
5. If you need clarification, set needs_clarification to true and provide a question

Respond with a JSON object:
{
  "understanding": "Brief explanation of what the user wants to do",
  "commands": ["command1", "command2", ...],
  "confidence": 0.0-1.0,
  "notes": "Any important notes or warnings",
  "needs_clarification": false,
  "clarification_question": "Question if clarification needed"
}

CRITICAL RULES:
1. Learn from failed attempts in the conversation history
2. Do NOT repeat commands that already failed
3. Consider what worked and what didn't in previous turns
4. If the user's input is a correction, incorporate it with previous context
5. Commands must be complete and executable
6. For multi-step operations, combine with && or use separate commands

Examples of learning from history:
- If "yum install X" failed with repo error, try fixing the repo first
- If "nvm install" failed because nvm wasn't sourced, include source command
- If user says "no that's wrong, try Y", abandon previous approach and use Y

Respond with ONLY the JSON object, no additional text.`,
		a.context.OS, a.context.Arch, a.context.LinuxDistro, a.context.PackageManager,
		session.StepName, session.OriginalCommand,
		session.OriginalOutput, session.OriginalError,
		conversationHistory,
		lastTurn.UserInput)
}

// recordFixAttempt records a fix attempt in the history
func (a *InstallAgent) recordFixAttempt(stepName, errorType string, commands []string, success bool, output, errMsg string, confidence float64) {
	attempt := FixAttempt{
		StepName:   stepName,
		ErrorType:  errorType,
		Commands:   commands,
		Success:    success,
		Output:     output,
		Error:      errMsg,
		Timestamp:  time.Now(),
		Confidence: confidence,
	}
	a.context.FixHistory = append(a.context.FixHistory, attempt)
}

// AnalyzeErrorWithHistory analyzes an error with full fix history context
func (a *InstallAgent) AnalyzeErrorWithHistory(ctx context.Context, stepName, command, output, errMsg string) (*ErrorAnalysis, error) {
	// Build enhanced prompt with fix history
	prompt := a.buildErrorAnalysisPromptWithHistory(stepName, command, output, errMsg)

	// Query LLM
	response, err := a.provider.Query(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM query failed: %w", err)
	}

	// Parse the response
	analysis, err := a.parseErrorAnalysis(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return analysis, nil
}

// ShellResult represents the result of a shell command
type ShellResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// executeShellCommand executes a shell command and returns the result
func (a *InstallAgent) executeShellCommand(ctx context.Context, command, stepName string) *ShellResult {
	// ====== SAFETY CHECK ======
	if a.safetyValidator != nil {
		safetyResult := a.safetyValidator.ValidateCommand(command)

		switch safetyResult.Level {
		case SafetyLevelForbidden:
			// Command is absolutely forbidden
			return &ShellResult{
				Success: false,
				Error:   fmt.Sprintf("SAFETY BLOCKED: %s\n%s", safetyResult.Reason, strings.Join(safetyResult.Suggestions, "\n")),
				Output:  "",
			}

		case SafetyLevelDangerous:
			// Dangerous command - check with callback
			if a.onDangerousCommand != nil {
				if !a.onDangerousCommand(command, safetyResult.Reason) {
					return &ShellResult{
						Success: false,
						Error:   fmt.Sprintf("DANGEROUS COMMAND REJECTED: %s", command),
						Output:  "",
					}
				}
			} else {
				// No callback - default to reject dangerous commands
				return &ShellResult{
					Success: false,
					Error:   fmt.Sprintf("DANGEROUS COMMAND BLOCKED (no confirmation): %s\nReason: %s", command, safetyResult.Reason),
					Output:  "",
				}
			}

		case SafetyLevelWarning:
			// Warning - log but proceed
			if a.context != nil {
				a.context.Warnings = append(a.context.Warnings,
					fmt.Sprintf("Safety warning for '%s': %s", command, safetyResult.Reason))
			}
		}
	}
	// ====== END SAFETY CHECK ======

	if a.onToolCall != nil {
		a.onToolCall(stepName, map[string]interface{}{"command": command})
	}

	// Determine timeout based on step type
	timeout := 60 // default 60 seconds
	switch {
	case strings.Contains(stepName, "install") || strings.Contains(stepName, "download"):
		timeout = 300 // 5 minutes for install/download
	case strings.Contains(stepName, "detect") || strings.Contains(stepName, "verify") || strings.Contains(stepName, "check"):
		timeout = 30 // 30 seconds for quick checks
	}

	// Use the run_command tool with appropriate timeout
	result, err := a.tools.Execute(ctx, "run_command", map[string]interface{}{
		"command": command,
		"timeout": timeout,
	})

	if err != nil {
		// Check if it's a timeout error
		if ctx.Err() == context.DeadlineExceeded || strings.Contains(err.Error(), "timeout") {
			return &ShellResult{
				Success: false,
				Error:   fmt.Sprintf("command timed out after %d seconds", timeout),
				Output:  "",
			}
		}
		return &ShellResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	return &ShellResult{
		Success: result.Success,
		Output:  result.Output,
		Error:   result.Error,
	}
}

// AnalyzeError uses LLM to analyze an installation error and suggest fixes
func (a *InstallAgent) AnalyzeError(ctx context.Context, stepName, command, output, errMsg string) (*ErrorAnalysis, error) {
	// Build error analysis prompt
	prompt := a.buildErrorAnalysisPrompt(stepName, command, output, errMsg)

	// Query LLM for analysis
	response, err := a.provider.Query(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to query LLM for error analysis: %w", err)
	}

	// Parse the response
	analysis, err := a.parseErrorAnalysis(response)
	if err != nil {
		return nil, fmt.Errorf("failed to parse error analysis: %w", err)
	}

	return analysis, nil
}

// buildErrorAnalysisPrompt builds the prompt for error analysis
func (a *InstallAgent) buildErrorAnalysisPrompt(stepName, command, output, errMsg string) string {
	return fmt.Sprintf(`You are an expert system administrator analyzing an installation failure.

System Information:
- OS: %s
- Architecture: %s
- Linux Distribution: %s
- Package Manager: %s

Failed Step: %s
Command: %s

Error Message:
%s

Command Output:
%s

Analyze this error and provide a JSON response with the following structure:
{
  "error_type": "category of error (e.g., repo_unavailable, permission_denied, network_error, dependency_missing, disk_space, glibc_incompatible, etc.)",
  "root_cause": "Clear explanation of why this error occurred",
  "is_recoverable": true/false,
  "suggested_fixes": [
    {
      "id": "fix_1",
      "description": "Human-readable description of what this fix does",
      "commands": ["command1", "command2"],
      "risk": "low|medium|high",
      "auto_safe": true/false
    }
  ],
  "confidence": 0.0-1.0,
  "web_search_needed": true/false,
  "web_search_query": "suggested search query if web search is needed"
}

CRITICAL: OS Compatibility Knowledge
- CentOS 7 has glibc 2.17 - software requiring glibc >= 2.28 will NOT work
- Node.js 18+ requires glibc 2.28+ - use Node.js 16.x for CentOS 7
- When glibc errors appear, suggest older compatible versions or alternative install methods
- CentOS 7 EOL: mirrorlist.centos.org is offline, use vault.centos.org

IMPORTANT GUIDELINES:
1. Be specific about the root cause - explain WHY it happened
2. For glibc/libc version errors:
   - This means the software is incompatible with the OS
   - Suggest using an older version of the software
   - Or suggest alternative installation methods (nvm, pyenv, manual download)
3. For CentOS 7 EOL issues (mirrorlist.centos.org unreachable):
   - The fix is to replace mirrorlist with vault.centos.org
4. For dependency conflicts (like "Processing Dependency: glibc >= 2.28"):
   - Mark is_recoverable as false if the software truly cannot run on this OS
   - Suggest using an older compatible version
5. Mark auto_safe as true ONLY if the fix is completely safe to run automatically
6. If you're unsure about the solution, set web_search_needed to true with a good search query

Respond with ONLY the JSON object, no additional text.`, a.context.OS, a.context.Arch, a.context.LinuxDistro, a.context.PackageManager, stepName, command, errMsg, output)
}

// buildErrorAnalysisPromptWithHistory builds an enhanced prompt including fix history
func (a *InstallAgent) buildErrorAnalysisPromptWithHistory(stepName, command, output, errMsg string) string {
	// Format fix history
	fixHistoryStr := "None"
	if len(a.context.FixHistory) > 0 {
		var historyBuilder strings.Builder
		for i, attempt := range a.context.FixHistory {
			historyBuilder.WriteString(fmt.Sprintf("\n  Attempt %d:\n", i+1))
			historyBuilder.WriteString(fmt.Sprintf("    Error Type: %s\n", attempt.ErrorType))
			historyBuilder.WriteString(fmt.Sprintf("    Commands: %v\n", attempt.Commands))
			historyBuilder.WriteString(fmt.Sprintf("    Success: %v\n", attempt.Success))
			if attempt.Error != "" {
				historyBuilder.WriteString(fmt.Sprintf("    Error: %s\n", attempt.Error))
			}
			historyBuilder.WriteString(fmt.Sprintf("    Confidence: %.2f\n", attempt.Confidence))
		}
		fixHistoryStr = historyBuilder.String()
	}

	return fmt.Sprintf(`You are an intelligent error recovery agent. Analyze the installation failure and decide the best course of action.

System Information:
- OS: %s
- Architecture: %s
- Linux Distribution: %s
- Package Manager: %s

Failed Step: %s
Command: %s

Error Message:
%s

Command Output:
%s

Previous Fix Attempts:
%s

Analyze the error and respond with JSON:
{
  "error_type": "network | permission | dependency | version_conflict | disk_space | unknown",
  "root_cause": "Clear explanation of why this error occurred",
  "should_continue": true/false,
  "reason": "Why you think we should/shouldn't continue trying",
  "confidence": 0.0-1.0,
  "next_action": "try_fix | try_alternative | abort | skip",
  "commands": ["command1", "command2"],
  "alternative_approach": "Description of alternative if next_action is try_alternative",
  "suggested_fixes": [
    {
      "id": "fix_1",
      "description": "Human-readable description",
      "commands": ["cmd1"],
      "risk": "low|medium|high",
      "auto_safe": true/false
    }
  ]
}

CRITICAL DECISION GUIDELINES:
1. should_continue:
   - Set to FALSE if:
     * Multiple fix attempts (>3) have already failed
     * Confidence is low (< 0.5)
     * Error indicates fundamental incompatibility (e.g., glibc version mismatch on CentOS 7)
     * The error requires manual user intervention (e.g., system upgrade)
   - Set to TRUE if:
     * This is the first or second attempt
     * You have a different approach to try
     * The error seems recoverable with a different strategy

2. next_action options:
   - "try_fix": Execute the commands in the "commands" field, then retry the original command
   - "try_alternative": Use a completely different installation approach
   - "abort": Stop trying and report failure
   - "skip": Skip this step and continue with the next one

3. IMPORTANT - Shell Command Behavior:
   - "source" commands in subshells DON'T persist environment changes
   - Combine commands with && when they depend on each other
   - Example: "source ~/.nvm/nvm.sh && nvm install 16" (NOT separate commands)

4. Learn from previous attempts:
   - If a previous fix used "yum" and failed, try "dnf" or a different approach
   - If nvm installation failed with source issues, use a different method
   - Don't repeat the exact same commands that already failed

5. Alternative approaches to consider:
   - Different package managers (apt vs yum vs dnf vs pacman)
   - Manual binary download and installation
   - Using version managers (nvm, pyenv, sdkman)
   - Building from source
   - Using Docker containers

Respond with ONLY the JSON object, no additional text.`,
		a.context.OS, a.context.Arch, a.context.LinuxDistro, a.context.PackageManager,
		stepName, command, errMsg, output, fixHistoryStr)
}

// parseErrorAnalysis parses the LLM response into ErrorAnalysis struct
func (a *InstallAgent) parseErrorAnalysis(response string) (*ErrorAnalysis, error) {
	// Extract JSON from response
	jsonStart := strings.Index(response, "{")
	if jsonStart == -1 {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Find matching closing brace
	depth := 0
	jsonEnd := -1
	for i, c := range response[jsonStart:] {
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				jsonEnd = jsonStart + i + 1
				break
			}
		}
	}

	if jsonEnd == -1 {
		return nil, fmt.Errorf("incomplete JSON in response")
	}

	jsonStr := response[jsonStart:jsonEnd]

	var analysis ErrorAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &analysis); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &analysis, nil
}

// currentTime returns current time
func currentTime() time.Time {
	return time.Now()
}

// detectLinuxDistro detects the Linux distribution by reading /etc/os-release
func detectLinuxDistro() string {
	// Try to read /etc/os-release
	data, err := readFile("/etc/os-release")
	if err != nil {
		// Fallback: try other common files
		if data, err = readFile("/etc/centos-release"); err == nil {
			return "centos"
		}
		if data, err = readFile("/etc/redhat-release"); err == nil {
			return "rhel"
		}
		if data, err = readFile("/etc/fedora-release"); err == nil {
			return "fedora"
		}
		if data, err = readFile("/etc/debian_version"); err == nil {
			return "debian"
		}
		return "unknown"
	}

	content := strings.ToLower(string(data))

	// Parse ID field
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") {
			id := strings.Trim(strings.TrimPrefix(line, "id="), "\"'")
			return id
		}
		if strings.HasPrefix(line, "id_like=") {
			idLike := strings.Trim(strings.TrimPrefix(line, "id_like="), "\"'")
			// Return the first ID_LIKE value as fallback
			if idLike != "" {
				return strings.Split(idLike, " ")[0]
			}
		}
	}

	return "unknown"
}

// readFile is a helper to read file contents
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// getPackageManager returns the appropriate package manager for a Linux distribution
func getPackageManager(distro string) string {
	switch distro {
	case "ubuntu", "debian", "linuxmint", "pop":
		return "apt"
	case "centos", "rhel", "redhat", "rocky", "almalinux", "amazon":
		return "yum"
	case "fedora":
		return "dnf"
	case "arch", "manjaro":
		return "pacman"
	case "opensuse", "suse", "sles":
		return "zypper"
	case "alpine":
		return "apk"
	default:
		// Check for rhel/centos family in ID_LIKE
		if strings.Contains(distro, "rhel") || strings.Contains(distro, "centos") || strings.Contains(distro, "fedora") {
			return "yum"
		}
		if strings.Contains(distro, "debian") || strings.Contains(distro, "ubuntu") {
			return "apt"
		}
		return "unknown"
	}
}

// GetState returns the current agent state
func (a *InstallAgent) GetState() AgentState {
	return a.state
}

// GetContext returns the current install context
func (a *InstallAgent) GetContext() *InstallContext {
	return a.context
}

// GetProgress returns current progress
func (a *InstallAgent) GetProgress() (current, total int) {
	return a.context.CurrentStep, a.context.TotalSteps
}

// formatBlockers formats blocker errors for display
func formatBlockers(blockers []dependencies.Blocker) string {
	var sb strings.Builder
	for i, b := range blockers {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s: %s", i+1, b.Package, b.Description))
		if b.Required != "" && b.Current != "" {
			sb.WriteString(fmt.Sprintf(" (需要: %s, 当前: %s)", b.Required, b.Current))
		}
		if b.Workaround != "" {
			sb.WriteString(fmt.Sprintf("\n      建议方案: %s", b.Workaround))
		}
	}
	return sb.String()
}
