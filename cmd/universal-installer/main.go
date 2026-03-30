package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kyd-w/installclaw/pkg/core/agent"
	"github.com/kyd-w/installclaw/pkg/core/config"
	"github.com/kyd-w/installclaw/pkg/core/logger"
	"github.com/kyd-w/installclaw/pkg/core/metadata"
	"github.com/kyd-w/installclaw/pkg/core/system"
	"github.com/kyd-w/installclaw/pkg/providers"
)

var (
	cfgFile string
	verbose bool
)

func init() {
	flag.StringVar(&cfgFile, "config", "", "Path to configuration file")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output")
}

// getConfigPath returns the absolute path to a config directory
func getConfigPath(relativePath string) string {
	// 1. Try relative to current working directory
	if _, err := os.Stat(relativePath); err == nil {
		return relativePath
	}

	// 2. Try relative to executable
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		candidate := filepath.Join(execDir, relativePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 3. Return default (will fail gracefully)
	return relativePath
}

// findConfigFile finds the configuration file
func findConfigFile() string {
	// If specified via flag, use it
	if cfgFile != "" {
		return cfgFile
	}

	// Try common locations
	locations := []string{
		"./installer.yaml",
		"./installer.yml",
		getConfigPath("installer.yaml"),
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

func main() {
	// Parse flags
	flag.Parse()

	// Load configuration
	cfgPath := findConfigFile()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Printf("Warning: failed to load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	// Override log level if verbose
	if verbose {
		cfg.Logging.Level = "debug"
	}

	// Initialize logger
	if err := logger.Init(cfg.Logging.ToLoggerConfig()); err != nil {
		fmt.Printf("Warning: failed to initialize logger: %v\n", err)
	}
	defer logger.Close()

	logger.Debug("Configuration loaded from: %s", cfgPath)
	logger.Debug("Log level: %s", cfg.Logging.Level)

	// Handle interrupt signals
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("Interrupted, cleaning up...")
		cancel()
	}()

	// Parse arguments (skip flags)
	args := flag.Args()
	if len(args) < 1 {
		printUsage()
		os.Exit(1)
	}

	command := args[0]

	switch command {
	case "install":
		if len(args) < 2 {
			logger.Error("Error: missing package name")
			printUsage()
			os.Exit(1)
		}
		installCmd(ctx, args[1:])

	case "search":
		if len(args) < 2 {
			logger.Error("Error: missing search query")
			printUsage()
			os.Exit(1)
		}
		searchCmd(ctx, args[1])

	case "info":
		if len(args) < 2 {
			logger.Error("Error: missing package name")
			printUsage()
			os.Exit(1)
		}
		infoCmd(ctx, args[1])

	case "version":
		printVersion()

	case "help", "-h", "--help":
		printUsage()

	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Universal Smart Installer - Install any software with AI assistance

Usage:
  installer [options] <command> [arguments]

Commands:
  install <package>  Install a package
  search <query>     Search for packages (also shows local install status)
  info <package>     Show package information
  version            Show version information
  help               Show this help message

Options:
  --config <file>    Path to configuration file
  --verbose          Enable verbose output

Install Options:
  --dry-run          Simulate installation without making changes
  --no-deps          Skip dependency installation
  --force            Force reinstall if already installed
  --untrusted        Allow untrusted sources (dangerous)

Examples:
  installer install nodejs
  installer install python --dry-run
  installer search redis
  installer info golang
  installer --verbose install nodejs`)
}

func printVersion() {
	fmt.Println("Universal Smart Installer v1.0.0")
	fmt.Println("Built with Go - Cross-platform, zero dependencies")
}

// installCmd handles the install command using the intelligent agent
func installCmd(ctx context.Context, args []string) {
	var (
		packageName    string
		dryRun         bool
		skipDeps       bool
		force          bool
		allowUntrusted bool
	)

	for _, arg := range args {
		switch arg {
		case "--dry-run":
			dryRun = true
		case "--no-deps":
			skipDeps = true
		case "--force":
			force = true
		case "--untrusted":
			allowUntrusted = true
		default:
			if packageName == "" {
				packageName = arg
			}
		}
	}

	if packageName == "" {
		logger.Error("Error: no package specified")
		os.Exit(1)
	}

	logger.Info("Installing package: %s", packageName)
	logger.Debug("Options: dryRun=%v, skipDeps=%v, force=%v, allowUntrusted=%v",
		dryRun, skipDeps, force, allowUntrusted)

	// Load configuration
	cfgPath := findConfigFile()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Warn("Failed to load config: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	// Detect local installation status first
	detector := system.NewSoftwareDetector()
	localPkg := detector.Detect(packageName)

	if localPkg.IsInstalled {
		fmt.Printf("\n📍 Local Installation Detected:\n")
		fmt.Printf("   Version: %s\n", localPkg.Version)
		if localPkg.Path != "" {
			fmt.Printf("   Path:    %s\n", localPkg.Path)
		}
		fmt.Printf("   Method:  %s\n", localPkg.InstallMethod)

		if !force {
			fmt.Printf("\n   %s is already installed. Use --force to reinstall.\n", packageName)
			fmt.Printf("   Continue with install/update? [y/N]: ")

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))

			if response != "y" && response != "yes" {
				fmt.Println("Installation cancelled.")
				return
			}
		}
	} else {
		fmt.Printf("📍 %s is not installed locally\n", packageName)
	}

	// Create AI provider from configuration
	aiProvider, err := createAIProvider(cfg)
	if err != nil {
		fmt.Printf("⚠ AI provider not available: %v\n", err)
		fmt.Printf("   Falling back to predefined package configuration...\n")
		installWithPredefinedConfig(ctx, packageName, cfg, dryRun, force)
		return
	}

	// Create agent with provider
	agentCfg := agent.DefaultAgentConfig()
	agentCfg.Verbose = verbose

	installAgent := agent.NewInstallAgent(aiProvider, agentCfg)

	// Set up callbacks
	installAgent.SetProgressCallback(func(step, total int, message string) {
		fmt.Printf("\r[%d/%d] %s", step, total, message)
	})

	installAgent.SetToolCallCallback(func(tool string, args map[string]interface{}) {
		logger.Debug("Tool call: %s %v", tool, args)
	})

	// Set up error callback for intelligent error handling
	installAgent.SetErrorCallback(func(stepName, command, output, errMsg string, analysis *agent.ErrorAnalysis) agent.ErrorHandlingDecision {
		fmt.Printf("\n\n")
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("❌ Installation step failed: %s\n", stepName)
		fmt.Println(strings.Repeat("=", 60))
		fmt.Printf("\n📋 Command that failed:\n   %s\n", command)
		fmt.Printf("\n📤 Output:\n%s\n", truncateString(output, 500))

		// Display AI analysis
		fmt.Println("\n🔍 AI Error Analysis:")
		fmt.Println(strings.Repeat("-", 40))
		fmt.Printf("Error Type:     %s\n", analysis.ErrorType)
		fmt.Printf("Root Cause:     %s\n", analysis.RootCause)
		fmt.Printf("Recoverable:    %v\n", analysis.IsRecoverable)
		fmt.Printf("Confidence:     %.0f%%\n", analysis.Confidence*100)

		if len(analysis.SuggestedFixes) > 0 {
			fmt.Println("\n💡 Suggested Fixes:")
			for i, fix := range analysis.SuggestedFixes {
				fmt.Printf("\n   [%d] %s\n", i+1, fix.Description)
				fmt.Printf("       Risk: %s | Auto-safe: %v\n", fix.Risk, fix.AutoSafe)
				if len(fix.Commands) > 0 {
					fmt.Printf("       Commands:\n")
					for _, cmd := range fix.Commands {
						fmt.Printf("         • %s\n", cmd)
					}
				}
			}
		}

		// Prompt user for decision
		fmt.Println("\n" + strings.Repeat("-", 40))
		fmt.Println("How would you like to proceed?")
		fmt.Println("  [1] Apply fix #1" + getFixLabel(analysis, 0))
		if len(analysis.SuggestedFixes) > 1 {
			fmt.Println("  [2] Apply fix #2" + getFixLabel(analysis, 1))
		}
		if len(analysis.SuggestedFixes) > 2 {
			fmt.Println("  [3] Apply fix #3" + getFixLabel(analysis, 2))
		}
		fmt.Println("  [n] Describe your solution in natural language")
		fmt.Println("  [c] Enter custom commands")
		fmt.Println("  [r] Retry the failed step")
		fmt.Println("  [s] Skip this step and continue")
		fmt.Println("  [a] Abort installation")
		fmt.Println("  [q] Quit")
		fmt.Print("\nYour choice: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		switch response {
		case "1":
			if len(analysis.SuggestedFixes) > 0 {
				return agent.ErrorHandlingDecision{
					Action: agent.ActionApplyFix,
					FixID:  analysis.SuggestedFixes[0].ID,
				}
			}
		case "2":
			if len(analysis.SuggestedFixes) > 1 {
				return agent.ErrorHandlingDecision{
					Action: agent.ActionApplyFix,
					FixID:  analysis.SuggestedFixes[1].ID,
				}
			}
		case "3":
			if len(analysis.SuggestedFixes) > 2 {
				return agent.ErrorHandlingDecision{
					Action: agent.ActionApplyFix,
					FixID:  analysis.SuggestedFixes[2].ID,
				}
			}
		case "n":
			// Natural language input - multi-turn conversation
			fmt.Println("\n💬 Describe your solution (AI will convert it to commands):")
			fmt.Println("   Example: 'use nvm to install node' or 'try vault.centos.org instead'")
			fmt.Print("\nYour solution: ")
			naturalInput, _ := reader.ReadString('\n')
			naturalInput = strings.TrimSpace(naturalInput)
			if naturalInput != "" {
				return agent.ErrorHandlingDecision{
					Action:       agent.ActionNaturalInput,
					NaturalInput: naturalInput,
				}
			}
		case "c":
			fmt.Print("Enter commands (one per line, empty line to finish):\n")
			var commands []string
			for {
				fmt.Print("  > ")
				line, _ := reader.ReadString('\n')
				line = strings.TrimSpace(line)
				if line == "" {
					break
				}
				commands = append(commands, line)
			}
			if len(commands) > 0 {
				return agent.ErrorHandlingDecision{
					Action:     agent.ActionRunCustom,
					CustomCmds: commands,
				}
			}
		case "r":
			return agent.ErrorHandlingDecision{Action: agent.ActionRetry}
		case "s":
			return agent.ErrorHandlingDecision{Action: agent.ActionSkip}
		case "a", "q":
			return agent.ErrorHandlingDecision{Action: agent.ActionAbort}
		}

		// Default: abort
		return agent.ErrorHandlingDecision{Action: agent.ActionAbort}
	})

	// Set up multi-turn natural input callbacks
	installAgent.SetNaturalInputCallbacks(
		// Clarification callback - when LLM needs more info
		func(stepName, question string) string {
			fmt.Printf("\n🤔 AI needs clarification:\n   %s\n", question)
			fmt.Print("\nYour response (or 'cancel' to abort): ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(response)
			if strings.ToLower(response) == "cancel" {
				return "" // Empty string signals cancellation
			}
			return response
		},
		// Execution failed callback - when a command fails during multi-turn
		func(stepName, cmd, output, errMsg string) agent.NaturalInputFailedResponse {
			fmt.Printf("\n\n")
			fmt.Println(strings.Repeat("-", 40))
			fmt.Printf("⚠️  Command failed during natural input execution:\n")
			fmt.Printf("   Command: %s\n", cmd)
			fmt.Printf("   Error: %s\n", truncateString(errMsg, 200))
			fmt.Printf("   Output: %s\n", truncateString(output, 200))
			fmt.Println("\nHow would you like to proceed?")
			fmt.Println("  [c] Continue with more natural language input")
			fmt.Println("  [r] Retry (let AI try again)")
			fmt.Println("  [a] Abort the installation")
			fmt.Print("\nYour choice: ")

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "c":
				fmt.Print("\n💬 Provide more guidance: ")
				additionalInput, _ := reader.ReadString('\n')
				return agent.NaturalInputFailedResponse{
					Action:          agent.NaturalInputActionContinue,
					AdditionalInput: strings.TrimSpace(additionalInput),
				}
			case "r":
				return agent.NaturalInputFailedResponse{
					Action: agent.NaturalInputActionRetry,
				}
			default:
				return agent.NaturalInputFailedResponse{
					Action: agent.NaturalInputActionAbort,
				}
			}
		},
	)

	// Set up dangerous command confirmation callback
	installAgent.SetDangerousCommandCallback(func(command, reason string) bool {
		fmt.Printf("\n\n")
		fmt.Println(strings.Repeat("!", 60))
		fmt.Println("⚠️  DANGEROUS COMMAND DETECTED")
		fmt.Println(strings.Repeat("!", 60))
		fmt.Printf("\nCommand: %s\n", command)
		fmt.Printf("Reason:  %s\n", reason)
		fmt.Print("\nDo you want to execute this command? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		return response == "y" || response == "yes"
	})

	// Build install options
	options := agent.InstallOptions{
		DryRun:         dryRun,
		Force:          force,
		SkipDeps:       skipDeps,
		AllowUntrusted: allowUntrusted || cfg.Security.AllowUntrusted,
	}

	// Execute installation with agent
	fmt.Printf("\n🤖 Starting intelligent installation for: %s\n", packageName)
	fmt.Println(strings.Repeat("─", 60))

	result, err := installAgent.Install(ctx, packageName, options)
	if err != nil {
		logger.Error("Installation failed: %v", err)
		fmt.Printf("\n❌ Installation failed: %v\n", err)
		os.Exit(1)
	}

	// Print result
	printAgentResult(result)
}

// createAIProvider creates an AI provider from configuration
func createAIProvider(cfg *config.Config) (*agent.ProviderAdapter, error) {
	// Priority: config file > environment variables

	// Try OpenAI config (from config file)
	if cfg.AI.OpenAI.APIKey != "" {
		httpProvider := providers.NewHTTPProviderWithConfig(&providers.HTTPProviderConfig{
			APIKey:  cfg.AI.OpenAI.APIKey,
			APIBase: cfg.AI.OpenAI.BaseURL,
			Model:   cfg.AI.OpenAI.Model,
		})
		logger.Debug("Using OpenAI provider from config: base_url=%s, model=%s", cfg.AI.OpenAI.BaseURL, cfg.AI.OpenAI.Model)
		return agent.NewProviderAdapter(httpProvider, cfg.AI.OpenAI.Model), nil
	}

	// Try Claude config (from config file)
	if cfg.AI.Claude.APIKey != "" {
		httpProvider := providers.NewHTTPProviderWithConfig(&providers.HTTPProviderConfig{
			APIKey:  cfg.AI.Claude.APIKey,
			APIBase: cfg.AI.Claude.BaseURL,
			Model:   cfg.AI.Claude.Model,
		})
		logger.Debug("Using Claude provider from config: base_url=%s, model=%s", cfg.AI.Claude.BaseURL, cfg.AI.Claude.Model)
		return agent.NewProviderAdapter(httpProvider, cfg.AI.Claude.Model), nil
	}

	// Fallback: Try to create provider from environment variables
	provider, err := providers.NewProviderFromEnv()
	if err == nil {
		logger.Debug("Using provider from environment variables")
		return agent.NewProviderAdapter(provider, ""), nil
	}

	return nil, fmt.Errorf("no AI provider configured (set API key in installer.yaml or set OPENAI_API_KEY/ANTHROPIC_API_KEY)")
}

// installWithPredefinedConfig installs using predefined package configuration
func installWithPredefinedConfig(ctx context.Context, packageName string, cfg *config.Config, dryRun, force bool) {
	// Load package metadata
	registry := metadata.NewRegistry(getConfigPath("configs/packages"))
	if err := registry.LoadPredefined(ctx); err != nil {
		logger.Warn("Failed to load packages: %v", err)
	}

	pkg, ok := registry.Get(packageName)
	if !ok {
		fmt.Printf("❌ Package '%s' not found in predefined configuration\n", packageName)
		fmt.Printf("   Configure an AI provider to enable intelligent installation.\n")
		os.Exit(1)
	}

	// Use predefined installation methods
	fmt.Printf("\n📦 Found predefined package: %s\n", pkg.Name)
	fmt.Printf("   Version: %s\n", pkg.Version)
	fmt.Printf("   Description: %s\n", pkg.Description)

	if len(pkg.InstallMethods) == 0 {
		fmt.Printf("❌ No installation methods defined for this package\n")
		os.Exit(1)
	}

	// Execute installation (simplified)
	fmt.Printf("\n📋 Installation methods available:\n")
	for i, method := range pkg.InstallMethods {
		fmt.Printf("   %d. %s (%s)\n", i+1, method.Name, method.Type)
	}

	fmt.Printf("\n✅ Package information loaded. Use AI provider for intelligent installation.\n")
}

// printAgentResult prints the agent installation result
func printAgentResult(result *agent.InstallContext) {
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))

	if result.Success {
		logger.Info("Installation completed successfully")
		fmt.Println("✅ Installation completed successfully!")
		fmt.Printf("   Package: %s\n", result.PackageName)
		if result.FinalVersion != "" {
			fmt.Printf("   Version: %s\n", result.FinalVersion)
		}
		if result.FinalPath != "" {
			fmt.Printf("   Path:    %s\n", result.FinalPath)
		}
	} else {
		logger.Error("Installation failed")
		fmt.Println("❌ Installation failed")
		if result.Error != "" {
			fmt.Printf("   Error: %s\n", result.Error)
		}
	}
}

// searchCmd handles the search command
func searchCmd(ctx context.Context, query string) {
	logger.Debug("Searching for: %s", query)

	fmt.Printf("\n🔍 Searching for: %s\n", query)
	fmt.Println(strings.Repeat("─", 60))

	// 1. Check if already installed locally
	detector := system.NewSoftwareDetector()
	installed := detector.Detect(query)

	fmt.Println("\n📍 Local Status:")
	if installed.IsInstalled {
		fmt.Printf("   ✅ INSTALLED\n")
		fmt.Printf("   Version:    %s\n", installed.Version)
		if installed.Path != "" {
			fmt.Printf("   Path:       %s\n", installed.Path)
		}
		fmt.Printf("   Method:     %s\n", installed.InstallMethod)
	} else {
		fmt.Printf("   ❌ Not installed\n")
	}

	// 2. Search local predefined packages
	registry := metadata.NewRegistry(getConfigPath("configs/packages"))
	if err := registry.LoadPredefined(ctx); err != nil {
		logger.Debug("No package config found: %v", err)
	}

	results := registry.Search(query)
	fmt.Println("\n📦 Package Database:")
	if len(results) > 0 {
		for _, pkg := range results {
			if pkg.Version != "" {
				fmt.Printf("   %-15s v%s\n", pkg.ID, pkg.Version)
			} else {
				fmt.Printf("   %-15s\n", pkg.ID)
			}
			fmt.Printf("   %s\n", pkg.Description)

			// Version comparison (only if both versions available)
			if installed.IsInstalled && installed.Version != "" && pkg.Version != "" {
				compareVersions(installed.Version, pkg.Version)
			}
		}
	} else {
		fmt.Printf("   No predefined package found (optional: add configs/packages/*.yaml)\n")
	}

	// 3. Query AI for latest version and installation info
	fmt.Println("\n🤖 AI Search (Latest Info):")

	cfgPath := findConfigFile()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = config.DefaultConfig()
	}

	aiProvider, err := createAIProvider(cfg)
	if err != nil {
		fmt.Println("   ⚠ AI not configured. Set API key in installer.yaml")
		fmt.Println("\n💡 To install: installer install " + query)
		return
	}

	// Build prompt with context
	var prompt string
	if installed.IsInstalled {
		prompt = fmt.Sprintf(`I have %s version %s installed.

Please tell me:
1. What is the LATEST stable version of %s?
2. Should I update? Compare my version with the latest.
3. What's new in the latest version (brief)?

Format:
Latest Version: [version]
Update Needed: [Yes/No]
What's New: [brief description]
Install Command: [command to install/update]`,
			query, installed.Version, query)
	} else {
		prompt = fmt.Sprintf(`Search for software: "%s"

Please provide:
1. Software description
2. Latest stable version
3. Install command for the current platform
4. Official homepage

Format:
Software: [name]
Latest Version: [version]
Description: [brief description]
Install: [command]
Homepage: [URL]`,
			query)
	}

	response, err := aiProvider.Query(ctx, prompt)
	if err != nil {
		logger.Error("AI query failed: %v", err)
		fmt.Printf("   ❌ AI search failed: %v\n", err)
		return
	}

	logger.Info("AI search completed")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(response)
	fmt.Println(strings.Repeat("─", 60))

	// Show install hint
	if installed.IsInstalled {
		fmt.Println("\n💡 To update: installer install " + query + " --force")
	} else {
		fmt.Println("\n💡 To install: installer install " + query)
	}
}

// compareVersions compares installed vs latest version
func compareVersions(installed, latest string) {
	// Simple version comparison
	if installed == latest {
		fmt.Printf("   ✅ Up to date (v%s)\n", installed)
	} else {
		fmt.Printf("   ⬆️  Update available: v%s → v%s\n", installed, latest)
	}
}

// infoCmd handles the info command
func infoCmd(ctx context.Context, packageName string) {
	logger.Debug("Getting info for: %s", packageName)

	// First check local installation status
	detector := system.NewSoftwareDetector()
	localPkg := detector.Detect(packageName)

	fmt.Printf("Package: %s\n\n", packageName)

	// Show local status
	fmt.Println("📍 Local Status:")
	if localPkg.IsInstalled {
		fmt.Printf("   ✅ INSTALLED\n")
		fmt.Printf("   Version:    %s\n", localPkg.Version)
		if localPkg.Path != "" {
			fmt.Printf("   Path:       %s\n", localPkg.Path)
		}
		fmt.Printf("   Method:     %s\n", localPkg.InstallMethod)
	} else {
		fmt.Printf("   ❌ Not installed\n")
	}

	// Try to get package config
	registry := metadata.NewRegistry(getConfigPath("configs/packages"))
	if err := registry.LoadPredefined(ctx); err != nil {
		logger.Debug("No package config found: %v", err)
	}

	pkg, ok := registry.Get(packageName)
	if ok {
		fmt.Printf("\n📦 Package Info:\n")
		fmt.Printf("   Name:     %s\n", pkg.Name)
		if pkg.Version != "" {
			fmt.Printf("   Version:  %s\n", pkg.Version)
		}
		fmt.Printf("   Category: %s\n", pkg.Category)
		fmt.Printf("   Homepage: %s\n", pkg.Homepage)
		fmt.Printf("\n   Description:\n     %s\n", pkg.Description)

		if len(pkg.InstallMethods) > 0 {
			fmt.Println("\n📋 Install Methods:")
			for i, method := range pkg.InstallMethods {
				fmt.Printf("   %d. %s\n", i+1, method.Name)
			}
		}
	} else {
		fmt.Printf("\n📦 Package config not found locally.\n")
		fmt.Printf("   Use 'installer search %s' to get latest info via AI.\n", packageName)
	}
}

// printProgress prints installation progress
func printProgress(progress interface{}) {
	// Simple progress output
	fmt.Printf(".")
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

// getFixLabel returns a label for a fix option
func getFixLabel(analysis *agent.ErrorAnalysis, index int) string {
	if index >= len(analysis.SuggestedFixes) {
		return ""
	}
	fix := analysis.SuggestedFixes[index]
	label := fmt.Sprintf(" - %s (risk: %s)", fix.Description, fix.Risk)
	if fix.AutoSafe {
		label += " [AUTO-SAFE]"
	}
	return label
}
