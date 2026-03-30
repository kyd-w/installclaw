// Package agent provides intelligent installation agent capabilities
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kyd-w/installclaw/pkg/core/dependencies"
)

// AgentState represents the current state of the agent
type AgentState string

const (
	StateIdle       AgentState = "idle"
	StateThinking   AgentState = "thinking"
	StateValidating AgentState = "validating"  // NEW: validating environment
	StateBlocked    AgentState = "blocked"     // NEW: blocked by environment
	StateExecuting  AgentState = "executing"
	StateWaiting    AgentState = "waiting"
	StateCompleted  AgentState = "completed"
	StateFailed     AgentState = "failed"
)

// InstallContext contains all information needed for installation
type InstallContext struct {
	// Request
	PackageName string            `json:"packageName"`
	UserGoal     string            `json:"userGoal"`      // What user wants to achieve
	Options      InstallOptions    `json:"options"`

	// System Info
	OS           string            `json:"os"`
	Arch         string            `json:"arch"`
	LinuxDistro  string            `json:"linuxDistro"`  // For Linux: ubuntu, centos, rhel, fedora, debian, etc.
	PackageManager string          `json:"packageManager"` // winget, brew, apt, yum, dnf, etc.
	ExistingTools []string         `json:"existingTools"`  // Already installed tools

	// Execution State
	CurrentStep   int               `json:"currentStep"`
	TotalSteps    int               `json:"totalSteps"`
	Steps         []InstallStep     `json:"steps"`
	ExecutedSteps []StepResult      `json:"executedSteps"`

	// Error Recovery
	FixHistory   []FixAttempt      `json:"fixHistory,omitempty"` // Track all fix attempts

	// Multi-turn Natural Input Session
	NaturalInputSession *NaturalInputSession `json:"naturalInputSession,omitempty"`

	// NEW: Dependency validation
	DepsToInstall []string              `json:"depsToInstall,omitempty"` // Dependencies to install first
	Warnings      []string             `json:"warnings,omitempty"`
	SystemInfo    *dependencies.SystemInfo `json:"systemInfo,omitempty"`

	// Results
	Success      bool              `json:"success"`
	FinalPath    string            `json:"finalPath,omitempty"`
	FinalVersion string            `json:"finalVersion,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// InstallOptions contains user-specified options
type InstallOptions struct {
	DryRun          bool     `json:"dryRun"`
	Force           bool     `json:"force"`
	SkipDeps        bool     `json:"skipDeps"`
	SkipValidation  bool     `json:"skipValidation"` // NEW: Skip environment validation
	Version         string   `json:"version"`
	CustomPath      string   `json:"customPath"`
	AllowUntrusted  bool     `json:"allowUntrusted"`
	Timeout         int      `json:"timeout"`
}

// InstallStep represents a single installation step
type InstallStep struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Type        StepType      `json:"type"`
	Command     string        `json:"command,omitempty"`
	Commands    []string      `json:"commands,omitempty"`
	ExpectedOut string        `json:"expectedOut,omitempty"`
	VerifyCmd   string        `json:"verifyCmd,omitempty"`
	Retryable   bool          `json:"retryable"`
	Timeout     time.Duration `json:"timeout"`
}

// StepType defines the type of installation step
type StepType string

const (
	StepTypeDetect   StepType = "detect"   // Detect system/package info
	StepTypeDownload StepType = "download" // Download files
	StepTypeInstall  StepType = "install"  // Run install command
	StepTypeConfig   StepType = "config"   // Configure environment
	StepTypeVerify   StepType = "verify"   // Verify installation
	StepTypeCleanup  StepType = "cleanup"  // Cleanup temp files
)

// StepResult represents the result of executing a step
type StepResult struct {
	StepID      string        `json:"stepId"`
	Success     bool          `json:"success"`
	Output      string        `json:"output,omitempty"`
	Error       string        `json:"error,omitempty"`
	Duration    time.Duration `json:"duration"`
	Attempts    int           `json:"attempts"`
	Timestamp   time.Time     `json:"timestamp"`
}

// ErrorAnalysis represents the LLM's analysis of an installation error
type ErrorAnalysis struct {
	ErrorType       string   `json:"error_type"`        // e.g., "repo_unavailable", "permission_denied", "network_error"
	RootCause       string   `json:"root_cause"`        // Human-readable explanation
	IsRecoverable   bool     `json:"is_recoverable"`    // Whether the error can be fixed
	SuggestedFixes  []FixAction `json:"suggested_fixes"` // List of possible fixes
	Confidence      float64  `json:"confidence"`        // Confidence level 0-1

	// LLM-driven decision fields
	ShouldContinue   bool     `json:"should_continue"`   // Whether to continue trying
	Reason           string   `json:"reason"`            // Why to continue/abort
	NextAction       string   `json:"next_action"`       // "try_fix", "try_alternative", "abort", "skip"
	Commands         []string `json:"commands"`          // Commands to execute for this attempt
	AlternativeApproach string `json:"alternative_approach,omitempty"` // Description of alternative approach
}

// FixAction represents a single fix action that can be executed
type FixAction struct {
	ID          string   `json:"id"`           // Unique identifier for the fix
	Description string   `json:"description"`  // Human-readable description
	Commands    []string `json:"commands"`     // Commands to execute
	Risk        string   `json:"risk"`         // Risk level: "low", "medium", "high"
	AutoSafe    bool     `json:"auto_safe"`    // Whether it's safe to execute automatically
}

// FixAttempt represents a single fix attempt for tracking history
type FixAttempt struct {
	StepName    string    `json:"step_name"`     // Which step this fix was for
	ErrorType   string    `json:"error_type"`    // Type of error being fixed
	Commands    []string  `json:"commands"`      // Commands executed
	Success     bool      `json:"success"`       // Whether the fix commands succeeded
	Output      string    `json:"output"`        // Output from fix commands
	Error       string    `json:"error"`         // Error if fix failed
	Timestamp   time.Time `json:"timestamp"`
	Confidence  float64   `json:"confidence"`    // LLM confidence for this fix
}

// ToolCall represents a tool function call from the AI
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	ToolCallID string `json:"toolCallId"`
	Success    bool   `json:"success"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
}

// Message represents a message in the agent conversation
type Message struct {
	Role       string       `json:"role"` // system, user, assistant
	Content    string       `json:"content"`
	ToolCalls  []ToolCall   `json:"toolCalls,omitempty"`
	ToolResult *ToolResult  `json:"toolResult,omitempty"`
	Timestamp  time.Time    `json:"timestamp"`
}

// Conversation manages the agent's conversation history
type Conversation struct {
	Messages []Message `json:"messages"`
}

// AddMessage adds a message to the conversation
func (c *Conversation) AddMessage(role, content string) {
	c.Messages = append(c.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// AddToolCall adds an assistant message with tool calls
func (c *Conversation) AddToolCall(toolCalls []ToolCall) {
	c.Messages = append(c.Messages, Message{
		Role:      "assistant",
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
	})
}

// AddToolResult adds a tool result message
// Note: Uses "user" role instead of "tool" because this agent uses custom JSON format,
// not standard OpenAI tool calling API
func (c *Conversation) AddToolResult(result *ToolResult) {
	var content string
	if result.Success {
		content = fmt.Sprintf("[Tool Result] %s: %s", result.ToolCallID, result.Output)
	} else {
		content = fmt.Sprintf("[Tool Error] %s: %s", result.ToolCallID, result.Error)
	}

	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
}

// GetContext returns the conversation as context for AI
func (c *Conversation) GetContext() string {
	var context string
	for _, msg := range c.Messages {
		switch msg.Role {
		case "system":
			context += "[SYSTEM] " + msg.Content + "\n"
		case "user":
			context += "[USER] " + msg.Content + "\n"
		case "assistant":
			if msg.Content != "" {
				context += "[ASSISTANT] " + msg.Content + "\n"
			}
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					context += "[TOOL_CALL] " + tc.Name + "\n"
				}
			}
		case "tool":
			if msg.ToolResult != nil {
				if msg.ToolResult.Success {
					context += "[TOOL_RESULT] Success: " + msg.ToolResult.Output + "\n"
				} else {
					context += "[TOOL_RESULT] Error: " + msg.ToolResult.Error + "\n"
				}
			}
		}
	}
	return context
}

// AgentConfig contains configuration for the agent
type AgentConfig struct {
	MaxIterations    int           `json:"maxIterations"`
	StepTimeout      time.Duration `json:"stepTimeout"`
	EnableRetry      bool          `json:"enableRetry"`
	MaxRetries       int           `json:"maxRetries"`
	Verbose          bool          `json:"verbose"`
	MemoryDir        string        `json:"memoryDir"` // Directory for learned dependencies
}

// DefaultAgentConfig returns default agent configuration
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		MaxIterations: 10,
		StepTimeout:   5 * time.Minute,
		EnableRetry:   true,
		MaxRetries:    3,
		Verbose:       true,
		MemoryDir:     ".installer/memory/dependencies",
	}
}

// InstallAgent is the main intelligent installation agent
type InstallAgent struct {
	config     *AgentConfig
	provider   AIProvider
	tools      ToolRegistry
	context    *InstallContext
	conversation *Conversation
	state      AgentState

	// Dependency validation
	depValidator *dependencies.Validator
	depLearner  *dependencies.Learner
	depLoader   *dependencies.Loader

	// Safety validation
	safetyValidator *CommandSafetyValidator

	// Callbacks
	onProgress  func(step int, total int, message string)
	onToolCall  func(tool string, args map[string]interface{})
	onComplete  func(ctx *InstallContext)
	onError     func(stepName, command, output, errMsg string, analysis *ErrorAnalysis) ErrorHandlingDecision

	// Multi-turn natural input callbacks
	onNaturalInputClarification    func(stepName, question string) string                                    // Returns user's clarification input
	onNaturalInputExecutionFailed  func(stepName, cmd, output, errMsg string) NaturalInputFailedResponse     // Returns user's decision

	// Safety callbacks
	onDangerousCommand  func(command, reason string) bool  // Returns true to allow execution
}

// ErrorHandlingDecision represents the user's decision on how to handle an error
type ErrorHandlingDecision struct {
	Action        ErrorAction `json:"action"`         // What to do next
	FixID         string      `json:"fix_id,omitempty"` // Which fix to apply (if action is apply_fix)
	CustomCmds    []string    `json:"custom_cmds,omitempty"` // Custom commands to run (if action is run_custom)
	NaturalInput  string      `json:"natural_input,omitempty"` // Natural language description of solution
}

// ErrorAction represents the action to take after error analysis
type ErrorAction string

const (
	ActionAbort         ErrorAction = "abort"          // Abort the installation
	ActionRetry         ErrorAction = "retry"          // Retry the failed step
	ActionApplyFix      ErrorAction = "apply_fix"      // Apply a suggested fix
	ActionRunCustom     ErrorAction = "run_custom"     // Run custom commands
	ActionSkip          ErrorAction = "skip"           // Skip this step and continue
	ActionNaturalInput  ErrorAction = "natural_input"  // User provides natural language solution
)

// ====== Multi-turn Natural Input Session Types ======

// NaturalInputSession represents a multi-turn conversation session for natural language input
type NaturalInputSession struct {
	StepName        string              `json:"step_name"`
	OriginalCommand string              `json:"original_command"`
	OriginalOutput  string              `json:"original_output"`
	OriginalError   string              `json:"original_error"`
	Turns           []NaturalInputTurn  `json:"turns"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// NaturalInputTurn represents a single turn in the conversation
type NaturalInputTurn struct {
	TurnNumber       int                      `json:"turn_number"`
	UserInput        string                   `json:"user_input"`
	LLMUnderstanding string                   `json:"llm_understanding,omitempty"`
	LLMCommands      []string                 `json:"llm_commands,omitempty"`
	LLMNotes         string                   `json:"llm_notes,omitempty"`
	LLMError         string                   `json:"llm_error,omitempty"`
	ExecutedCommands []CommandExecutionRecord `json:"executed_commands,omitempty"`
	Timestamp        time.Time                `json:"timestamp"`
}

// CommandExecutionRecord records the execution of a command
type CommandExecutionRecord struct {
	Command   string    `json:"command"`
	Success   bool      `json:"success"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// NaturalInputUserAction represents user's action after execution failure
type NaturalInputUserAction string

const (
	NaturalInputActionContinue NaturalInputUserAction = "continue" // Continue with more input
	NaturalInputActionAbort    NaturalInputUserAction = "abort"    // Abort the session
	NaturalInputActionRetry    NaturalInputUserAction = "retry"    // Retry without new input
)

// NaturalInputFailedResponse represents user's response when execution fails
type NaturalInputFailedResponse struct {
	Action          NaturalInputUserAction `json:"action"`
	AdditionalInput string                 `json:"additional_input,omitempty"` // For continue action
}

// AddUserInput adds a new user input turn to the session
func (s *NaturalInputSession) AddUserInput(input string) {
	turn := NaturalInputTurn{
		TurnNumber: len(s.Turns) + 1,
		UserInput:  input,
		Timestamp:  time.Now(),
	}
	s.Turns = append(s.Turns, turn)
	s.UpdatedAt = time.Now()
}

// GetConversationHistory returns formatted conversation history for LLM context
func (s *NaturalInputSession) GetConversationHistory() string {
	var history strings.Builder
	for i, turn := range s.Turns {
		history.WriteString(fmt.Sprintf("\n--- Turn %d ---\n", i+1))
		history.WriteString(fmt.Sprintf("User: %s\n", turn.UserInput))
		if turn.LLMUnderstanding != "" {
			history.WriteString(fmt.Sprintf("LLM Understanding: %s\n", turn.LLMUnderstanding))
		}
		if len(turn.LLMCommands) > 0 {
			history.WriteString(fmt.Sprintf("LLM Commands: %v\n", turn.LLMCommands))
		}
		if len(turn.ExecutedCommands) > 0 {
			for _, exec := range turn.ExecutedCommands {
				if exec.Success {
					history.WriteString(fmt.Sprintf("Executed [%s]: SUCCESS\n%s\n", exec.Command, exec.Output))
				} else {
					history.WriteString(fmt.Sprintf("Executed [%s]: FAILED\nError: %s\nOutput: %s\n", exec.Command, exec.Error, exec.Output))
				}
			}
		}
	}
	return history.String()
}

// GetLastFailedCommand returns the last failed command and its error, if any
func (s *NaturalInputSession) GetLastFailedCommand() (cmd, output, errMsg string, found bool) {
	for i := len(s.Turns) - 1; i >= 0; i-- {
		for j := len(s.Turns[i].ExecutedCommands) - 1; j >= 0; j-- {
			exec := s.Turns[i].ExecutedCommands[j]
			if !exec.Success {
				return exec.Command, exec.Output, exec.Error, true
			}
		}
	}
	return "", "", "", false
}

// AIProvider interface for AI capabilities
type AIProvider interface {
	// Query sends a prompt and returns the response
	Query(ctx context.Context, prompt string) (string, error)

	// QueryWithHistory sends a prompt with conversation history
	QueryWithHistory(ctx context.Context, messages []Message) (string, error)

	// IsAvailable checks if the provider is ready
	IsAvailable() bool

	// Name returns the provider name
	Name() string
}

// ToolRegistry interface for tool execution (defined in tools.go)
