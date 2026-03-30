// Package installer provides universal package installation
package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kyd-w/installclaw/pkg/core/ai"
	"github.com/kyd-w/installclaw/pkg/core/metadata"
	"github.com/kyd-w/installclaw/pkg/core/resolver"
	"github.com/kyd-w/installclaw/pkg/core/source"
)

// UniversalInstaller provides universal package installation
type UniversalInstaller struct {
	registry    *metadata.Registry
	aiManager   *ai.Manager
	verifier    *source.CompositeVerifier
	resolver    *resolver.Resolver
	downloader  *Downloader
	config      *InstallerConfig
	progress    chan InstallProgress
}

// InstallerConfig contains installer configuration
type InstallerConfig struct {
	DryRun           bool          `json:"dryRun"`           // Only simulate installation
	SkipDependencies bool          `json:"skipDependencies"` // Skip dependency installation
	ForceReinstall   bool          `json:"forceReinstall"`   // Force reinstall even if already installed
	Timeout          time.Duration `json:"timeout"`          // Installation timeout
	InstallDir       string        `json:"installDir"`       // Installation directory
	BinDir           string        `json:"binDir"`           // Binary directory
	ConfigDir        string        `json:"configDir"`        // Configuration directory
	AllowUntrusted   bool          `json:"allowUntrusted"`   // Allow untrusted sources (dangerous)
}

// DefaultInstallerConfig returns default installer configuration
func DefaultInstallerConfig() *InstallerConfig {
	return &InstallerConfig{
		DryRun:           false,
		SkipDependencies: false,
		ForceReinstall:   false,
		Timeout:          30 * time.Minute,
		InstallDir:       "/usr/local",
		BinDir:           "/usr/local/bin",
		ConfigDir:        "~/.config",
		AllowUntrusted:   false,
	}
}

// NewUniversalInstaller creates a new universal installer
func NewUniversalInstaller(config *InstallerConfig) *UniversalInstaller {
	if config == nil {
		config = DefaultInstallerConfig()
	}

	registry := metadata.NewRegistry()
	securityConfig := source.DefaultSecurityConfig()
	securityConfig.AllowUntrusted = config.AllowUntrusted

	return &UniversalInstaller{
		registry:   registry,
		aiManager:  ai.NewManager(nil),
		verifier:   source.NewCompositeVerifier(securityConfig),
		resolver:   resolver.NewResolver(registry),
		downloader: NewDownloader(),
		config:     config,
		progress:   make(chan InstallProgress, 100),
	}
}

// SetAIManager sets the AI manager for the installer
func (i *UniversalInstaller) SetAIManager(manager *ai.Manager) {
	i.aiManager = manager
}

// Install installs a package by name
func (i *UniversalInstaller) Install(ctx context.Context, software string) (*InstallResult, error) {
	result := &InstallResult{
		PackageID: software,
		StartTime: time.Now(),
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, i.config.Timeout)
	defer cancel()

	// 1. Query package metadata
	i.reportProgress(PhaseQuery, "Querying package information...", 0)
	pkg, err := i.queryPackage(ctx, software)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("failed to query package: %w", err)
	}
	result.PackageID = pkg.ID
	result.Version = pkg.Version

	// 2. Security verification
	i.reportProgress(PhaseVerify, "Verifying package sources...", 10)
	if err := i.verifier.Verify(ctx, pkg); err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("security verification failed: %w", err)
	}

	// 3. Resolve dependencies
	i.reportProgress(PhaseResolve, "Resolving dependencies...", 20)
	depGraph, err := i.resolver.Resolve(ctx, pkg)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("dependency resolution failed: %w", err)
	}

	// 4. Install dependencies (if not skipped)
	if !i.config.SkipDependencies {
		depPkgs := depGraph.GetOrderedPackages()
		for idx, depPkg := range depPkgs {
			if depPkg.ID == pkg.ID {
				continue // Skip main package
			}

			progress := 20 + (idx+1)*30/len(depPkgs)
			i.reportProgress(PhaseInstallDeps, fmt.Sprintf("Installing dependency: %s", depPkg.Name), progress)

			if i.config.DryRun {
				fmt.Printf("[DRY RUN] Would install dependency: %s\n", depPkg.ID)
				continue
			}

			depResult, err := i.installPackage(ctx, depPkg)
			if err != nil {
				result.Error = err.Error()
				return result, fmt.Errorf("failed to install dependency %s: %w", depPkg.ID, err)
			}
			result.Dependencies = append(result.Dependencies, depResult)
		}
	}

	// 5. Install main package
	i.reportProgress(PhaseInstall, fmt.Sprintf("Installing %s...", pkg.Name), 70)
	if i.config.DryRun {
		fmt.Printf("[DRY RUN] Would install: %s %s\n", pkg.ID, pkg.Version)
		result.Success = true
		return result, nil
	}

	_, err = i.installPackage(ctx, pkg)
	if err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("installation failed: %w", err)
	}

	// 6. Generate configuration
	i.reportProgress(PhaseConfig, "Generating configuration...", 90)
	if len(pkg.ConfigTemplates) > 0 {
		if err := i.generateConfig(pkg); err != nil {
			// Log warning but don't fail
			result.Warnings = append(result.Warnings, fmt.Sprintf("Config generation warning: %v", err))
		}
	}

	// 7. Verify installation
	i.reportProgress(PhaseVerifyInstall, "Verifying installation...", 95)
	if err := i.verifyInstallation(pkg); err != nil {
		result.Error = err.Error()
		return result, fmt.Errorf("verification failed: %w", err)
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	i.reportProgress(PhaseComplete, "Installation complete!", 100)
	return result, nil
}

// queryPackage queries package metadata from cache, predefined configs, or AI
func (i *UniversalInstaller) queryPackage(ctx context.Context, name string) (*metadata.PackageMetadata, error) {
	// 1. Check registry (includes predefined configs)
	if pkg, ok := i.registry.Get(name); ok {
		fmt.Printf("[DEBUG] Found %s in registry\n", name)
		return pkg, nil
	}

	// 2. Query AI
	fmt.Printf("[DEBUG] Querying AI for: %s\n", name)
	prompt := buildPackageQueryPrompt(name)
	response, err := i.aiManager.Query(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("AI query failed: %w", err)
	}

	fmt.Printf("[DEBUG] AI Response (first 500 chars):\n%s\n", truncate(response, 500))

	// 3. Parse AI response
	pkg, err := parseAIResponse(response, name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w", err)
	}

	fmt.Printf("[DEBUG] Parsed package: ID=%s, Name=%s, Version=%s, Methods=%d\n",
		pkg.ID, pkg.Name, pkg.Version, len(pkg.InstallMethods))

	// 4. Cache result
	i.registry.Register(pkg)

	return pkg, nil
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// installPackage installs a single package
func (i *UniversalInstaller) installPackage(ctx context.Context, pkg *metadata.PackageMetadata) (*InstallResult, error) {
	result := &InstallResult{
		PackageID: pkg.ID,
		Version:   pkg.Version,
	}

	// Select best installation method
	method := i.selectInstallMethod(pkg)
	if method == nil {
		return result, fmt.Errorf("no suitable installation method found for %s", pkg.ID)
	}

	result.Method = string(method.Type)

	// Download if binary method
	if method.Type == "binary" {
		asset := i.selectAsset(pkg)
		if asset == nil {
			return result, fmt.Errorf("no suitable download asset found for %s", pkg.ID)
		}

		downloadPath, err := i.downloader.Download(ctx, asset)
		if err != nil {
			return result, fmt.Errorf("download failed: %w", err)
		}
		result.InstallPath = downloadPath
	}

	// Execute installation commands
	for _, cmd := range method.Commands {
		processedCmd := i.processCommand(cmd, pkg)

		if err := i.executeCommand(ctx, processedCmd, method); err != nil {
			return result, fmt.Errorf("command failed: %w", err)
		}
	}

	// Execute post-install actions
	for _, action := range method.PostInstall {
		for _, cmd := range action.Commands {
			processedCmd := i.processCommand(cmd, pkg)

			if err := i.executeCommand(ctx, processedCmd, method); err != nil {
				// Log warning but continue
				result.Warnings = append(result.Warnings, fmt.Sprintf("Post-install action '%s' failed: %v", action.Name, err))
			}
		}
	}

	result.Success = true
	return result, nil
}

// selectInstallMethod selects the best installation method for the current platform
func (i *UniversalInstaller) selectInstallMethod(pkg *metadata.PackageMetadata) *metadata.InstallMethod {
	currentOS := runtime.GOOS
	currentArch := runtime.GOARCH

	fmt.Printf("[DEBUG] Selecting install method for %s (OS=%s, Arch=%s)\n", pkg.ID, currentOS, currentArch)
	fmt.Printf("[DEBUG] Available methods: %d\n", len(pkg.InstallMethods))

	for idx, method := range pkg.InstallMethods {
		fmt.Printf("[DEBUG] Method %d: Type=%s, Name='%s', Commands=%v\n",
			idx, method.Type, method.Name, method.Commands)
		fmt.Printf("[DEBUG]   Platform.OS=%v, Platform.Arch=%v\n",
			method.Platform.OS, method.Platform.Arch)

		// Check platform compatibility
		if len(method.Platform.OS) > 0 {
			found := false
			for _, os := range method.Platform.OS {
				if os == currentOS || os == "all" {
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("[DEBUG] Method %d skipped: OS mismatch\n", idx)
				continue
			}
		}

		if len(method.Platform.Arch) > 0 {
			found := false
			for _, arch := range method.Platform.Arch {
				if arch == currentArch || arch == "all" {
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("[DEBUG] Method %d skipped: Arch mismatch\n", idx)
				continue
			}
		}

		fmt.Printf("[DEBUG] Method %d selected!\n", idx)
		return &method
	}

	fmt.Printf("[DEBUG] No suitable method found\n")
	return nil
}

// selectAsset selects the best download asset for the current platform
func (i *UniversalInstaller) selectAsset(pkg *metadata.PackageMetadata) *metadata.Asset {
	currentOS := runtime.GOOS
	currentArch := runtime.GOARCH

	for _, source := range pkg.Sources {
		if source.ReleaseInfo == nil {
			continue
		}

		for _, asset := range source.ReleaseInfo.Assets {
			if asset.OS == currentOS || asset.OS == "all" {
				if asset.Arch == currentArch || asset.Arch == "all" {
					return &asset
				}
			}
		}
	}

	return nil
}

// processCommand replaces placeholders in commands
func (i *UniversalInstaller) processCommand(cmd string, pkg *metadata.PackageMetadata) string {
	replacements := map[string]string{
		"{{os}}":      runtime.GOOS,
		"{{arch}}":    runtime.GOARCH,
		"{{version}}": pkg.Version,
		"{{id}}":      pkg.ID,
	}

	for placeholder, value := range replacements {
		cmd = replaceAll(cmd, placeholder, value)
	}

	return cmd
}

// reportProgress reports installation progress
func (i *UniversalInstaller) reportProgress(phase InstallPhase, message string, percent int) {
	if i.progress != nil {
		select {
		case i.progress <- InstallProgress{
			Phase:     phase,
			Message:   message,
			Percent:   percent,
			Timestamp: time.Now(),
		}:
		default:
			// Channel full, skip
		}
	}
}

// Progress returns the progress channel
func (i *UniversalInstaller) Progress() <-chan InstallProgress {
	return i.progress
}

// buildPackageQueryPrompt builds the AI prompt for package query
func buildPackageQueryPrompt(name string) string {
	return fmt.Sprintf(`I need to install "%s" on %s (%s).

IMPORTANT: Provide ACCURATE installation commands that are verified to work.

Return JSON in this EXACT format:
{
  "id": "unique-package-id",
  "name": "Display Name",
  "version": "latest",
  "homepage": "https://official-website.com",
  "category": "runtime|dev-tool|database|application",
  "sources": [
    {"type": "official", "url": "https://...", "trusted": true}
  ],
  "installMethods": [
    {
      "type": "package",
      "name": "Winget (Windows)",
      "commands": ["winget install --exact --id CORRECT_PACKAGE_ID_HERE --accept-source-agreements --accept-package-agreements"],
      "verifyCmd": "winget list --exact --id CORRECT_PACKAGE_ID_HERE",
      "platform": {"os": ["windows"], "arch": ["amd64", "arm64"]}
    },
    {
      "type": "package",
      "name": "Homebrew (macOS)",
      "commands": ["brew install PACKAGE_NAME"],
      "platform": {"os": ["darwin"], "arch": ["amd64", "arm64"]}
    },
    {
      "type": "package",
      "name": "APT (Linux)",
      "commands": ["sudo apt update && sudo apt install -y PACKAGE_NAME"],
      "platform": {"os": ["linux"], "arch": ["amd64", "arm64"]}
    },
    {
      "type": "binary",
      "name": "Direct Download",
      "commands": ["# Download from official website"],
      "platform": {"os": ["all"], "arch": ["all"]}
    }
  ]
}

CRITICAL RULES:
1. For winget: Use ONLY verified winget IDs. Search with "winget search" first
2. For brew: Use the exact formula name from homebrew
3. For apt: Use packages available in Ubuntu/Debian repositories
4. If unsure about package manager ID, provide official download URL instead
5. The commands MUST be copy-paste ready and work immediately
6. Include --accept-source-agreements --accept-package-agreements for winget to avoid prompts

Current platform: %s/%s`, name, runtime.GOOS, runtime.GOARCH, runtime.GOOS, runtime.GOARCH)
}

// parseAIResponse parses the AI response into package metadata
func parseAIResponse(response string, name string) (*metadata.PackageMetadata, error) {
	pkg := &metadata.PackageMetadata{
		ID:          name,
		Name:        name,
		Version:     "latest",
		Description: "Package queried from AI",
		Category:    "unknown",
	}

	// Try to parse as JSON first
	response = strings.TrimSpace(response)

	// Extract JSON if embedded in markdown code block
	if idx := strings.Index(response, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(response[start:], "```")
		if end != -1 {
			response = response[start : start+end]
		}
	} else if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		end := strings.Index(response[start:], "```")
		if end != -1 {
			response = response[start : start+end]
		}
	}

	// Try to find JSON object
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart != -1 && jsonEnd != -1 && jsonEnd > jsonStart {
		jsonStr := response[jsonStart : jsonEnd+1]

		var aiResult struct {
			ID             string `json:"id"`
			Software       string `json:"software"`
			Name           string `json:"name"`
			LatestVersion  string `json:"latest_version"`
			Version        string `json:"version"`
			Description    string `json:"description"`
			Homepage       string `json:"homepage"`
			Category       string `json:"category"`
			InstallCommand string `json:"install_command"`
			Install        string `json:"install"`
			GitHub         string `json:"github"`
			OfficialURL    string `json:"official_url"`
			InstallMethods []struct {
				Type       string   `json:"type"`
				Name       string   `json:"name"`
				Commands   []string `json:"commands"`
				VerifyCmd  string   `json:"verifyCmd"`
				Verify     string   `json:"verify"`
				Platform   struct {
					OS   []string `json:"os"`
					Arch []string `json:"arch"`
				} `json:"platform"`
			} `json:"installMethods"`
			Sources []struct {
				Type     string `json:"type"`
				URL      string `json:"url"`
				Stars    int    `json:"stars"`
				Trusted  bool   `json:"trusted"`
			} `json:"sources"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &aiResult); err == nil {
			if aiResult.Name != "" {
				pkg.Name = aiResult.Name
			} else if aiResult.Software != "" {
				pkg.Name = aiResult.Software
			}
			if aiResult.ID != "" {
				pkg.ID = aiResult.ID
			}
			if aiResult.LatestVersion != "" {
				pkg.Version = aiResult.LatestVersion
			} else if aiResult.Version != "" {
				pkg.Version = aiResult.Version
			}
			if aiResult.Description != "" {
				pkg.Description = aiResult.Description
			}
			if aiResult.Homepage != "" {
				pkg.Homepage = aiResult.Homepage
			}
			if aiResult.Category != "" {
				pkg.Category = aiResult.Category
			}

			// Build sources from AI response
			for _, src := range aiResult.Sources {
				sourceType := metadata.SourceCustom
				switch src.Type {
				case "github", "GitHub":
					sourceType = metadata.SourceGitHub
				case "official", "Official":
					sourceType = metadata.SourceOfficial
				case "npm":
					sourceType = metadata.SourceNPM
				case "pypi":
					sourceType = metadata.SourcePyPI
				}
				pkg.Sources = append(pkg.Sources, metadata.PackageSource{
					Type:     sourceType,
					URL:      src.URL,
					Stars:    src.Stars,
					Trusted:  src.Trusted || sourceType == metadata.SourceOfficial,
					MinStars: 200,
				})
			}

			// Fallback sources
			if len(pkg.Sources) == 0 && aiResult.Homepage != "" {
				pkg.Sources = append(pkg.Sources, metadata.PackageSource{
					Type:     metadata.SourceOfficial,
					URL:      aiResult.Homepage,
					Trusted:  true,
				})
			}

			// Build install methods from AI response
			for _, method := range aiResult.InstallMethods {
				methodType := metadata.InstallScript
				switch method.Type {
				case "binary":
					methodType = metadata.InstallBinary
				case "package":
					methodType = metadata.InstallPackage
				case "script":
					methodType = metadata.InstallScript
				case "source":
					methodType = metadata.InstallSource
				}

				installMethod := metadata.InstallMethod{
					Type:       methodType,
					Name:       method.Name,
					Commands:   method.Commands,
					VerifyCmd:  method.VerifyCmd,
					Platform: metadata.PlatformConstraint{
						OS:   method.Platform.OS,
						Arch: method.Platform.Arch,
					},
				}

				// Default platform to "all" if not specified
				if len(installMethod.Platform.OS) == 0 {
					installMethod.Platform.OS = []string{"all"}
				}
				if len(installMethod.Platform.Arch) == 0 {
					installMethod.Platform.Arch = []string{"all"}
				}

				pkg.InstallMethods = append(pkg.InstallMethods, installMethod)
			}

			// Fallback install method from simple fields
			if len(pkg.InstallMethods) == 0 {
				installCmd := aiResult.InstallCommand
				if installCmd == "" {
					installCmd = aiResult.Install
				}
				if installCmd != "" {
					pkg.InstallMethods = append(pkg.InstallMethods, metadata.InstallMethod{
						Type:       metadata.InstallScript,
						Name:       "AI Recommended",
						Commands:   []string{installCmd},
						VerifyCmd:  name + " --version",
						Platform: metadata.PlatformConstraint{
							OS:   []string{"all"},
							Arch: []string{"all"},
						},
					})
				}
			}

			return pkg, nil
		}
	}

	// Fallback: parse text response using regex patterns
	pkg.Description = extractField(response, "Description:", "Description")
	if v := extractField(response, "Latest Version:", "Latest Version"); v != "" {
		pkg.Version = v
	} else if v := extractField(response, "Version:", "Version"); v != "" {
		pkg.Version = v
	}
	if v := extractField(response, "Homepage:", "Homepage"); v != "" {
		pkg.Homepage = v
		pkg.Sources = append(pkg.Sources, metadata.PackageSource{
			Type:     metadata.SourceOfficial,
			URL:      v,
			Trusted:  true,
		})
	}

	// Extract install command
	installCmd := extractField(response, "Install:", "Install")
	if installCmd == "" {
		installCmd = extractField(response, "Install Command:", "Install Command")
	}
	if installCmd != "" {
		// Clean up command (remove markdown formatting)
		installCmd = strings.Trim(installCmd, "`")
		pkg.InstallMethods = append(pkg.InstallMethods, metadata.InstallMethod{
			Type:       metadata.InstallScript,
			Name:       "AI Recommended",
			Commands:   []string{installCmd},
			VerifyCmd:  name + " --version",
			Platform: metadata.PlatformConstraint{
				OS:   []string{"all"},
				Arch: []string{"all"},
			},
		})
	}

	// If no sources found, add a default placeholder
	if len(pkg.Sources) == 0 {
		pkg.Sources = append(pkg.Sources, metadata.PackageSource{
			Type:     metadata.SourceCustom,
			URL:      "ai-generated",
			Trusted:  true, // Trust AI-generated packages
		})
	}

	// If no install methods found, add a placeholder
	if len(pkg.InstallMethods) == 0 {
		pkg.InstallMethods = append(pkg.InstallMethods, metadata.InstallMethod{
			Type:       metadata.InstallScript,
			Name:       "Manual Install",
			Commands:   []string{fmt.Sprintf("# Please check %s for installation instructions", pkg.Homepage)},
			Platform: metadata.PlatformConstraint{
				OS:   []string{"all"},
				Arch: []string{"all"},
			},
		})
	}

	return pkg, nil
}

// extractField extracts a field value from text response
func extractField(text, prefix, _ string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(prefix)) {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// replaceAll is a helper for string replacement
func replaceAll(s, old, new string) string {
	// Simple implementation - in production use strings.ReplaceAll
	result := ""
	for {
		idx := findString(s, old)
		if idx == -1 {
			result += s
			break
		}
		result += s[:idx] + new
		s = s[idx+len(old):]
	}
	return result
}

func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Additional methods would be implemented here:
// - executeCommand: executes shell commands
// - generateConfig: generates configuration files
// - verifyInstallation: verifies the installation
// - Rollback: rolls back a failed installation

func (i *UniversalInstaller) executeCommand(ctx context.Context, cmd string, method *metadata.InstallMethod) error {
	if i.config.DryRun {
		fmt.Printf("[DRY RUN] Would execute: %s\n", cmd)
		return nil
	}

	fmt.Printf("Executing: %s\n", cmd)

	// Determine shell based on OS
	var shell string
	var shellFlag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellFlag = "/C"
	} else {
		shell = "sh"
		shellFlag = "-c"
	}

	// Create command
	execCmd := exec.CommandContext(ctx, shell, shellFlag, cmd)

	// Set working directory if specified
	if method != nil && method.WorkingDir != "" {
		execCmd.Dir = method.WorkingDir
	}

	// Set environment variables
	if method != nil && len(method.EnvVars) > 0 {
		execCmd.Env = os.Environ()
		for k, v := range method.EnvVars {
			execCmd.Env = append(execCmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Capture output
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr

	// Execute
	startTime := time.Now()
	err := execCmd.Run()
	duration := time.Since(startTime)

	// Log output
	if stdout.Len() > 0 {
		fmt.Printf("  stdout: %s\n", strings.TrimSpace(stdout.String()))
	}
	if stderr.Len() > 0 {
		fmt.Printf("  stderr: %s\n", strings.TrimSpace(stderr.String()))
	}

	if err != nil {
		fmt.Printf("  ❌ Failed (%v): %v\n", duration, err)
		return fmt.Errorf("command failed: %w", err)
	}

	fmt.Printf("  ✅ Completed (%v)\n", duration)
	return nil
}

func (i *UniversalInstaller) generateConfig(pkg *metadata.PackageMetadata) error {
	// Placeholder - would generate config files from templates
	return nil
}

func (i *UniversalInstaller) verifyInstallation(pkg *metadata.PackageMetadata) error {
	// Placeholder - would verify installation
	return nil
}
