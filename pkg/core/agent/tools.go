package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ToolRegistry manages available tools
type ToolRegistry interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error)
	List() []string
	GetSchema(name string) string
}

// DefaultToolRegistry is the default implementation
type DefaultToolRegistry struct {
	tools map[string]Tool
}

// Tool defines a tool interface
type Tool interface {
	Name() string
	Description() string
	Schema() string
	Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error)
}

// NewDefaultToolRegistry creates a new default tool registry
func NewDefaultToolRegistry() *DefaultToolRegistry {
	return NewDefaultToolRegistryWithConfig(nil)
}

// ToolRegistryConfig contains configuration for the tool registry
type ToolRegistryConfig struct {
	WebSearch *WebSearchConfig
}

// NewDefaultToolRegistryWithConfig creates a new default tool registry with configuration
func NewDefaultToolRegistryWithConfig(cfg *ToolRegistryConfig) *DefaultToolRegistry {
	r := &DefaultToolRegistry{
		tools: make(map[string]Tool),
	}

	// Register built-in tools
	r.Register(&DetectSoftwareTool{})
	r.Register(&RunCommandTool{})
	r.Register(&ReadFileTool{})
	r.Register(&WriteFileTool{})
	r.Register(&CheckPathTool{})

	// Register web search tool with optional config
	var webSearchCfg *WebSearchConfig
	if cfg != nil && cfg.WebSearch != nil {
		webSearchCfg = cfg.WebSearch
	}
	r.Register(NewWebSearchTool(webSearchCfg))

	return r
}

// Register adds a tool to the registry
func (r *DefaultToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

// Execute runs a tool by name
func (r *DefaultToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (*ToolResult, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool.Execute(ctx, args)
}

// List returns available tool names
func (r *DefaultToolRegistry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// GetSchema returns the JSON schema for a tool
func (r *DefaultToolRegistry) GetSchema(name string) string {
	tool, ok := r.tools[name]
	if !ok {
		return ""
	}
	return tool.Schema()
}

// ============= Built-in Tools =============

// DetectSoftwareTool checks if software is installed
type DetectSoftwareTool struct{}

func (t *DetectSoftwareTool) Name() string {
	return "detect_software"
}

func (t *DetectSoftwareTool) Description() string {
	return "Check if software is already installed on the system"
}

func (t *DetectSoftwareTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Software name to detect"},
			"command": {"type": "string", "description": "Command to check (e.g., 'node --version')"}
		},
		"required": ["name"]
	}`
}

func (t *DetectSoftwareTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, _ := args["name"].(string)
	cmd, _ := args["command"].(string)

	if cmd == "" {
		// Try common commands
		commonCmds := []string{
			name + " --version",
			name + " -v",
			name,
		}
		for _, c := range commonCmds {
			if result := tryCommand(ctx, c); result != nil {
				return result, nil
			}
		}
		// All commands failed, software not found
		return &ToolResult{
			ToolCallID: t.Name(),
			Success:    false,
			Output:     fmt.Sprintf("%s is not installed", name),
		}, nil
	}

	return tryCommand(ctx, cmd), nil
}

// tryCommand tries to execute a command
func tryCommand(ctx context.Context, cmd string) *ToolResult {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}

	execCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		execCmd = exec.CommandContext(execCtx, "cmd", "/c", strings.Join(parts, " "))
	} else {
		execCmd = exec.CommandContext(execCtx, "sh", "-c", strings.Join(parts, " "))
	}

	output, err := execCmd.CombinedOutput()
	if err != nil {
		// Command failed, return nil to indicate failure
		return nil
	}

	return &ToolResult{
		ToolCallID: "detect_software",
		Success:    true,
		Output:     strings.TrimSpace(string(output)),
	}
}

// RunCommandTool executes shell commands
type RunCommandTool struct{}

func (t *RunCommandTool) Name() string {
	return "run_command"
}

func (t *RunCommandTool) Description() string {
	return "Execute a shell command"
}

func (t *RunCommandTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"command": {"type": "string", "description": "Command to execute"},
			"timeout": {"type": "integer", "description": "Hard timeout in seconds (default: 60)"},
			"sudo": {"type": "boolean", "description": "Run with elevated privileges"},
			"capture_output": {"type": "boolean", "description": "Capture stdout/stderr (default: true)"},
			"smart_timeout": {"type": "boolean", "description": "Use smart timeout (only timeout if process is idle, default: true for install commands)"},
			"idle_timeout": {"type": "integer", "description": "Idle timeout in seconds - time without activity before killing process (default: 30)"}
		},
		"required": ["command"]
	}`
}

func (t *RunCommandTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmd, _ := args["command"].(string)
	timeout, _ := args["timeout"].(float64)
	sudo, _ := args["sudo"].(bool)
	useSmartTimeout, _ := args["smart_timeout"].(bool)
	idleTimeout, _ := args["idle_timeout"].(float64)

	if cmd == "" {
		return nil, fmt.Errorf("command is required")
	}

	// Default timeout
	if timeout == 0 {
		timeout = 60
	}

	// Detect if this is a long-running package manager command
	isPackageManagerCmd := strings.Contains(cmd, "install") ||
		strings.Contains(cmd, "update") ||
		strings.Contains(cmd, "upgrade") ||
		strings.Contains(cmd, "yum") ||
		strings.Contains(cmd, "apt") ||
		strings.Contains(cmd, "dnf") ||
		strings.Contains(cmd, "makecache") ||
		strings.Contains(cmd, "clean")

	// Use smart timeout by default for install/download commands
	if useSmartTimeout || isPackageManagerCmd {
		// Smart timeout: only timeout if process is idle
		// For package managers, use longer idle timeout (120s instead of 30s)
		if idleTimeout == 0 {
			if isPackageManagerCmd {
				idleTimeout = 120 // 2 minutes idle timeout for package managers
			} else {
				idleTimeout = 30 // 30 seconds for other commands
			}
		}

		// Also extend hard timeout for package managers
		hardTimeout := timeout
		if isPackageManagerCmd && timeout < 300 {
			hardTimeout = 300 // 5 minutes minimum for package managers
		}

		execResult := ExecuteWithSmartTimeout(ctx, cmd, time.Duration(hardTimeout)*time.Second, time.Duration(idleTimeout)*time.Second)

		result := &ToolResult{
			ToolCallID: t.Name(),
			Success:    execResult.Success,
			Output:     execResult.Output,
		}

		if !execResult.Success {
			result.Error = execResult.Error
			if execResult.TimedOut {
				result.Error = fmt.Sprintf("command %s: %s", execResult.KillReason, execResult.Error)
			}
		}

		return result, nil
	}

	// Standard timeout (original behavior)
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		if sudo {
			// Windows: run as admin (requires elevation)
			execCmd = exec.CommandContext(cmdCtx, "powershell", "-Command", cmd)
		} else {
			execCmd = exec.CommandContext(cmdCtx, "cmd", "/c", cmd)
		}
	} else {
		if sudo {
			execCmd = exec.CommandContext(cmdCtx, "sudo", "sh", "-c", cmd)
		} else {
			execCmd = exec.CommandContext(cmdCtx, "sh", "-c", cmd)
		}
	}

	var output []byte
	var err error

	// Always capture output for better error messages
	output, err = execCmd.CombinedOutput()

	result := &ToolResult{
		ToolCallID: t.Name(),
		Success:    err == nil,
		Output:     strings.TrimSpace(string(output)),
	}

	if err != nil {
		result.Error = err.Error()
		// Check for timeout
		if cmdCtx.Err() == context.DeadlineExceeded {
			result.Error = fmt.Sprintf("command timed out after %.0f seconds", timeout)
		}
	}

	return result, nil
}

// ReadFileTool reads file contents
type ReadFileTool struct{}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read contents of a file"
}

func (t *ReadFileTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path to read"},
			"max_size": {"type": "integer", "description": "Maximum bytes to read (default: 1MB)"}
		},
		"required": ["path"]
	}`
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	path, _ := args["path"].(string)
	maxSize, _ := args["max_size"].(float64)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Expand path
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	// Default max size: 1MB
	if maxSize == 0 {
		maxSize = 1024 * 1024
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return &ToolResult{
			ToolCallID: t.Name(),
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	// Truncate if too large
	if len(content) > int(maxSize) {
		content = content[:int(maxSize)]
	}

	return &ToolResult{
		ToolCallID: t.Name(),
		Success:   true,
		Output:     string(content),
	}, nil
}

// WriteFileTool writes content to a file
type WriteFileTool struct{}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Write content to a file"
}

func (t *WriteFileTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "File path to write"},
			"content": {"type": "string", "description": "Content to write"},
			"append": {"type": "boolean", "description": "Append to file instead of overwrite"},
			"mode": {"type": "string", "description": "File mode (e.g., '0644')"}
		},
		"required": ["path", "content"]
	}`
}

func (t *WriteFileTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	append, _ := args["append"].(bool)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Expand path
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[1:])
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return &ToolResult{
			ToolCallID: t.Name(),
			Success:   false,
			Error:     fmt.Sprintf("failed to create directory: %v", err),
		}, nil
	}

	// Write or append
	var err error
	if append {
		err = os.WriteFile(path, []byte(content), 0644)
	} else {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, err = f.WriteString(content)
			f.Close()
		}
	}

	if err != nil {
		return &ToolResult{
			ToolCallID: t.Name(),
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	return &ToolResult{
		ToolCallID: t.Name(),
		Success:   true,
		Output:     fmt.Sprintf("Successfully wrote %d bytes to %s", len(content), path),
	}, nil
}

// CheckPathTool checks if a path or command exists
type CheckPathTool struct{}

func (t *CheckPathTool) Name() string {
	return "check_path"
}

func (t *CheckPathTool) Description() string {
	return "Check if a path or command exists in the system"
}

func (t *CheckPathTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path or command name to check"},
			"type": {"type": "string", "enum": ["file", "dir", "command"], "description": "Type to check"}
		},
		"required": ["path"]
	}`
}

func (t *CheckPathTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	path, _ := args["path"].(string)
	checkType, _ := args["type"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Expand path
	path = os.ExpandEnv(path)

	var exists bool
	var foundPath string

	switch checkType {
	case "command":
		// Look for command in PATH
		foundPath, _ = exec.LookPath(path)
		exists = foundPath != ""

	case "dir":
		info, err := os.Stat(path)
		exists = err == nil && info.IsDir()
		foundPath = path

	case "file", "":
		info, err := os.Stat(path)
		exists = err == nil && !info.IsDir()
		foundPath = path

	default:
		return nil, fmt.Errorf("invalid type: %s", checkType)
	}

	result := &ToolResult{
		ToolCallID: t.Name(),
		Success:   exists,
	}

	if exists {
		result.Output = fmt.Sprintf("Found: %s", foundPath)
	} else {
		result.Output = fmt.Sprintf("Not found: %s", path)
	}

	return result, nil
}

// MarshalJSON returns JSON schema as bytes
func (r *ToolResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"toolCallId": r.ToolCallID,
		"success":    r.Success,
		"output":     r.Output,
		"error":      r.Error,
	})
}

// WebSearchTool searches the web for information
type WebSearchTool struct {
	apiKey  string // Tavily API key (optional)
	primary string // Primary search engine: "duckduckgo" or "tavily"
}

// WebSearchConfig configures the web search tool
type WebSearchConfig struct {
	APIKey  string
	Primary string // "duckduckgo" (default) or "tavily"
}

// NewWebSearchTool creates a new web search tool with optional configuration
func NewWebSearchTool(cfg *WebSearchConfig) *WebSearchTool {
	t := &WebSearchTool{
		primary: "duckduckgo",
	}
	if cfg != nil {
		t.apiKey = cfg.APIKey
		if cfg.Primary != "" {
			t.primary = cfg.Primary
		}
	}
	return t
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web for information, solutions, and documentation"
}

func (t *WebSearchTool) Schema() string {
	return `{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query"},
			"search_type": {"type": "string", "enum": ["general", "error_solution", "documentation", "package_info"], "description": "Type of search to optimize results"},
			"engine": {"type": "string", "enum": ["auto", "tavily", "duckduckgo"], "description": "Search engine to use (default: auto)"}
		},
		"required": ["query"]
	}`
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	query, _ := args["query"].(string)
	searchType, _ := args["search_type"].(string)
	engine, _ := args["engine"].(string)

	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Enhance query based on search type
	switch searchType {
	case "error_solution":
		query = query + " solution fix how to resolve"
	case "documentation":
		query = query + " documentation official guide"
	case "package_info":
		query = query + " install requirements dependencies"
	}

	// Determine which engine to use
	useEngine := engine
	if useEngine == "" || useEngine == "auto" {
		useEngine = t.primary
		// If Tavily is preferred but no API key, fallback to DuckDuckGo
		if useEngine == "tavily" && t.apiKey == "" {
			useEngine = "duckduckgo"
		}
	}

	// Try Tavily first if configured
	if useEngine == "tavily" && t.apiKey != "" {
		result, err := t.searchWithTavily(ctx, query)
		if err == nil && result.Success {
			return result, nil
		}
		// Tavily failed, try fallback
		if result != nil && result.Output != "" {
			return result, nil
		}
	}

	// Use DuckDuckGo (default or fallback)
	return t.searchWithDuckDuckGo(ctx, query)
}

// searchWithTavily performs search using Tavily API
func (t *WebSearchTool) searchWithTavily(ctx context.Context, query string) (*ToolResult, error) {
	result := &ToolResult{
		ToolCallID: t.Name(),
	}

	// Tavily API request body
	requestBody := map[string]interface{}{
		"query":           query,
		"search_depth":    "basic",
		"include_answer":  true,
		"include_raw_content": false,
		"max_results":     5,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		result.Error = fmt.Sprintf("failed to marshal request: %v", err)
		return result, nil
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search", bytes.NewReader(jsonBody))
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		return result, nil
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("tavily request failed: %v", err)
		return result, nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Sprintf("failed to read response: %v", err)
		return result, nil
	}

	if resp.StatusCode != 200 {
		result.Error = fmt.Sprintf("tavily returned status %d: %s", resp.StatusCode, string(body))
		return result, nil
	}

	// Parse Tavily response
	var tavilyResp struct {
		Answer  string `json:"answer"`
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Score   float64 `json:"score"`
		} `json:"results"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}

	if err := json.Unmarshal(body, &tavilyResp); err != nil {
		result.Error = fmt.Sprintf("failed to parse response: %v", err)
		return result, nil
	}

	// Build readable response
	var response strings.Builder

	if tavilyResp.Answer != "" {
		response.WriteString("## Answer\n\n")
		response.WriteString(tavilyResp.Answer)
		response.WriteString("\n\n")
	}

	if len(tavilyResp.Results) > 0 {
		response.WriteString("## Search Results\n\n")
		for i, r := range tavilyResp.Results {
			if i >= 5 {
				break
			}
			response.WriteString(fmt.Sprintf("### %d. %s\n", i+1, r.Title))
			response.WriteString(fmt.Sprintf("**URL**: %s\n", r.URL))
			response.WriteString(fmt.Sprintf("**Relevance**: %.0f%%\n\n", r.Score*100))
			if r.Content != "" {
				response.WriteString(r.Content)
				response.WriteString("\n\n")
			}
		}
	}

	result.Output = response.String()
	result.Success = true

	if result.Output == "" {
		result.Output = t.fallbackSearch(query)
	}

	return result, nil
}

// searchWithDuckDuckGo performs search using DuckDuckGo Instant Answer API
func (t *WebSearchTool) searchWithDuckDuckGo(ctx context.Context, query string) (*ToolResult, error) {
	result := &ToolResult{
		ToolCallID: t.Name(),
	}

	// Use curl to search via DuckDuckGo instant answer API (no API key needed)
	searchURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		strings.ReplaceAll(query, " ", "+"))

	cmd := exec.CommandContext(ctx, "curl", "-s", "-L", "--max-time", "30", searchURL)
	output, err := cmd.Output()

	if err != nil {
		result.Error = fmt.Sprintf("web search failed: %v", err)
		result.Output = t.fallbackSearch(query)
		return result, nil
	}

	// Parse DuckDuckGo response
	var ddgResponse struct {
		AbstractText   string `json:"AbstractText"`
		AbstractSource string `json:"AbstractSource"`
		AbstractURL    string `json:"AbstractURL"`
		Heading        string `json:"Heading"`
		RelatedTopics  []struct {
			Text string `json:"Text"`
			URL  string `json:"FirstURL"`
		} `json:"RelatedTopics"`
		Results []struct {
			Text string `json:"Text"`
			URL  string `json:"FirstURL"`
		} `json:"Results"`
	}

	if err := json.Unmarshal(output, &ddgResponse); err != nil {
		result.Output = string(output)
		result.Success = true
		return result, nil
	}

	// Build readable response
	var response strings.Builder
	if ddgResponse.Heading != "" {
		response.WriteString(fmt.Sprintf("## %s\n\n", ddgResponse.Heading))
	}
	if ddgResponse.AbstractText != "" {
		response.WriteString(fmt.Sprintf("%s\n\n", ddgResponse.AbstractText))
		if ddgResponse.AbstractURL != "" {
			response.WriteString(fmt.Sprintf("Source: %s\n\n", ddgResponse.AbstractURL))
		}
	}

	if len(ddgResponse.RelatedTopics) > 0 {
		response.WriteString("### Related Information:\n")
		for i, topic := range ddgResponse.RelatedTopics {
			if i >= 5 {
				break
			}
			if topic.Text != "" {
				response.WriteString(fmt.Sprintf("- %s\n", topic.Text))
				if topic.URL != "" {
					response.WriteString(fmt.Sprintf("  URL: %s\n", topic.URL))
				}
			}
		}
	}

	result.Output = response.String()
	result.Success = true

	if result.Output == "" {
		result.Output = t.fallbackSearch(query)
	}

	return result, nil
}

// fallbackSearch provides a fallback when web search fails
func (t *WebSearchTool) fallbackSearch(query string) string {
	return fmt.Sprintf(`Web search unavailable. Please try searching manually:

1. Google: https://www.google.com/search?q=%s
2. Stack Overflow: https://stackoverflow.com/search?q=%s
3. GitHub Issues: https://github.com/issues?q=%s

Common solutions to try:
- Check if the software is compatible with your OS version
- Look for alternative installation methods
- Check the official documentation
- Search for the specific error message`, strings.ReplaceAll(query, " ", "+"))
}
