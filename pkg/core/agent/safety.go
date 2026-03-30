// Package agent provides intelligent installation agent capabilities
package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// CommandSafetyLevel represents the safety level of a command
type CommandSafetyLevel int

const (
	SafetyLevelSafe      CommandSafetyLevel = iota // Safe to execute automatically
	SafetyLevelWarning                             // Execute with warning
	SafetyLevelDangerous                           // Requires explicit confirmation
	SafetyLevelForbidden                           // Never execute
)

// SafetyCheckResult represents the result of a safety check
type SafetyCheckResult struct {
	Level       CommandSafetyLevel `json:"level"`
	Reason      string             `json:"reason"`
	Category    string             `json:"category"`
	Suggestions []string           `json:"suggestions,omitempty"`
}

// CommandSafetyValidator validates commands for safety
type CommandSafetyValidator struct {
	forbiddenPatterns    []*regexp.Regexp
	dangerousPatterns    []*regexp.Regexp
	warningPatterns      []*regexp.Regexp
	protectedPaths       []string
	protectedFiles       []string
	dangerousCommands    map[string]bool
	requireRootCommands  map[string]bool
}

// NewCommandSafetyValidator creates a new safety validator
func NewCommandSafetyValidator() *CommandSafetyValidator {
	v := &CommandSafetyValidator{
		dangerousCommands:   make(map[string]bool),
		requireRootCommands: make(map[string]bool),
	}

	v.initForbiddenPatterns()
	v.initDangerousPatterns()
	v.initWarningPatterns()
	v.initProtectedPaths()
	v.initDangerousCommands()

	return v
}

// initForbiddenPatterns initializes patterns that should NEVER be executed
func (v *CommandSafetyValidator) initForbiddenPatterns() {
	// Patterns that are absolutely forbidden
	forbiddenRegexes := []string{
		// Disk/system destruction
		`rm\s+(-[rf]+\s+)*(/\s*)?$`,
		`rm\s+(-[rf]+\s+)*(/\*)`,
		`rm\s+(-[rf]+\s+)*/bin`,
		`rm\s+(-[rf]+\s+)*/boot`,
		`rm\s+(-[rf]+\s+)*/dev`,
		`rm\s+(-[rf]+\s+)*/etc`,
		`rm\s+(-[rf]+\s+)*/lib`,
		`rm\s+(-[rf]+\s+)*/proc`,
		`rm\s+(-[rf]+\s+)*/root`,
		`rm\s+(-[rf]+\s+)*/sbin`,
		`rm\s+(-[rf]+\s+)*/sys`,
		`rm\s+(-[rf]+\s+)*/usr`,
		`rm\s+(-[rf]+\s+)*/var`,
		`rm\s+(-[rf]+\s+)*~`,
		`rm\s+(-[rf]+\s+)*\*`,

		// Disk overwrite
		`dd\s+.*of=/dev/[sh]d`,
		`dd\s+.*of=/dev/nvme`,
		`dd\s+.*of=/dev/mmcblk`,

		// Fork bomb
		`:(){ :\|:& };:`,

		// Download and execute without verification
		`curl\s+.*\|\s*(sudo\s+)?bash`,
		`wget\s+.*\|\s*(sudo\s+)?bash`,
		`curl\s+.*\|\s*(sudo\s+)?sh`,
		`wget\s+.*\|\s*(sudo\s+)?sh`,

		// Modify critical system files
		`>\s*/etc/passwd`,
		`>\s*/etc/shadow`,
		`>\s*/etc/sudoers`,
		`chmod\s+777\s+/etc/passwd`,
		`chmod\s+777\s+/etc/shadow`,
		`chmod\s+777\s+/etc/sudoers`,

		// Network attacks
		`iptables\s+-F`,
		`iptables\s+-P\s+INPUT\s+DROP`,
		`iptables\s+-P\s+OUTPUT\s+DROP`,

		// Kernel modules
		`rmmod\s+\*`,

		// Clear all files
		`find\s+/.*-delete`,
		`find\s+/.*-exec\s+rm`,

		// Shutdown/reboot without warning
		`shutdown\s+(-h\s+)?now`,
		`reboot\s*(--force)?`,

		// Format disk
		`mkfs\s+/dev/[sh]d`,
		`mkfs\.ext[234]\s+/dev/`,
		`mkfs\.xfs\s+/dev/`,
	}

	for _, pattern := range forbiddenRegexes {
		if re, err := regexp.Compile(pattern); err == nil {
			v.forbiddenPatterns = append(v.forbiddenPatterns, re)
		}
	}
}

// initDangerousPatterns initializes patterns that require explicit confirmation
func (v *CommandSafetyValidator) initDangerousPatterns() {
	dangerousRegexes := []string{
		// Package removal
		`(apt|apt-get)\s+remove\s+`,
		`(apt|apt-get)\s+purge\s+`,
		`yum\s+remove\s+`,
		`dnf\s+remove\s+`,
		`pacman\s+-R\s+`,
		`pip\s+uninstall\s+`,
		`npm\s+uninstall\s+(-g\s+)?`,
		`winget\s+uninstall\s+`,

		// System modification
		`systemctl\s+(disable|mask)\s+`,
		`chkconfig\s+.*off`,

		// File operations on system dirs
		`mv\s+.*/etc/`,
		`cp\s+.*/etc/.*\s+/etc/`,
		`chmod\s+[0-7]+\s+/etc/`,
		`chown\s+.*/etc/`,

		// Kernel operations
		`modprobe\s+`,
		`insmod\s+`,
		`rmmod\s+`,

		// User management
		`userdel\s+`,
		`useradd\s+`,
		`passwd\s+`,

		// Network configuration
		`ifconfig\s+.+\s+down`,
		`ip\s+link\s+set\s+.+\s+down`,
		`route\s+.*del\s+default`,

		// Service management
		`service\s+.+\s+stop`,
		`systemctl\s+stop\s+`,
		`systemctl\s+restart\s+`,

		// Curl/wget to protected locations
		`curl\s+.*>\s*/usr/bin/`,
		`curl\s+.*>\s*/usr/local/bin/`,
		`wget\s+.*-O\s+/usr/bin/`,
		`wget\s+.*-O\s+/usr/local/bin/`,

		// Environment modifications
		`export\s+PATH\s*=`,
		`source\s+/etc/profile`,
		`\.\s+/etc/profile`,
	}

	for _, pattern := range dangerousRegexes {
		if re, err := regexp.Compile(pattern); err == nil {
			v.dangerousPatterns = append(v.dangerousPatterns, re)
		}
	}
}

// initWarningPatterns initializes patterns that should warn the user
func (v *CommandSafetyValidator) initWarningPatterns() {
	warningRegexes := []string{
		// File removal (non-system)
		`rm\s+-rf\s+`,

		// File overwrite
		`>\s+`,

		// Recursive operations
		`chmod\s+-R\s+`,
		`chown\s+-R\s+`,

		// Sudo operations
		`sudo\s+`,

		// Curl/wget without checksum
		`curl\s+(-o|-O)\s+`,
		`wget\s+`,

		// Running scripts
		`bash\s+.*\.sh`,
		`sh\s+.*\.sh`,
		`\.\/.*\.sh`,

		// Piping to bash (semi-dangerous)
		`\|\s*bash`,
		`\|\s*sh`,
	}

	for _, pattern := range warningRegexes {
		if re, err := regexp.Compile(pattern); err == nil {
			v.warningPatterns = append(v.warningPatterns, re)
		}
	}
}

// initProtectedPaths initializes paths that should be protected
func (v *CommandSafetyValidator) initProtectedPaths() {
	v.protectedPaths = []string{
		// System directories
		"/bin",
		"/sbin",
		"/usr/bin",
		"/usr/sbin",
		"/usr/lib",
		"/usr/lib64",
		"/lib",
		"/lib64",
		"/boot",
		"/dev",
		"/proc",
		"/sys",
		"/etc",
		"/root",

		// User data (warning only)
		"/home",
		"/Users",

		// Package manager databases
		"/var/lib/dpkg",
		"/var/lib/rpm",
		"/var/lib/pacman",
	}

	v.protectedFiles = []string{
		// Critical system files
		"/etc/passwd",
		"/etc/shadow",
		"/etc/sudoers",
		"/etc/fstab",
		"/etc/hosts",
		"/etc/hostname",
		"/etc/resolv.conf",
		"/etc/profile",
		"/etc/environment",

		// Boot files
		"/boot/vmlinuz",
		"/boot/grub/grub.cfg",
		"/boot/grub2/grub.cfg",

		// Systemd
		"/etc/systemd/system",

		// SSH
		"/etc/ssh/sshd_config",
		"/root/.ssh/authorized_keys",
	}
}

// initDangerousCommands initializes the dangerous commands map
func (v *CommandSafetyValidator) initDangerousCommands() {
	// Commands that should trigger extra caution
	dangerous := []string{
		"rm", "dd", "mkfs", "fdisk", "parted",
		"shutdown", "reboot", "halt", "poweroff",
		"iptables", "ip6tables", "ufw",
		"userdel", "useradd", "usermod", "groupdel", "groupadd",
		"chown", "chmod",
		"systemctl", "service", "chkconfig",
	}

	for _, cmd := range dangerous {
		v.dangerousCommands[cmd] = true
	}

	// Commands that typically require root
	rootRequired := []string{
		"apt", "apt-get", "yum", "dnf", "pacman", "zypper", "rpm",
		"systemctl", "service",
		"useradd", "userdel", "usermod",
		"iptables", "ufw",
		"mount", "umount",
		"fdisk", "parted", "mkfs",
	}

	for _, cmd := range rootRequired {
		v.requireRootCommands[cmd] = true
	}
}

// ValidateCommand checks if a command is safe to execute
func (v *CommandSafetyValidator) ValidateCommand(command string) SafetyCheckResult {
	command = strings.TrimSpace(command)

	// Check forbidden patterns first
	for _, pattern := range v.forbiddenPatterns {
		if pattern.MatchString(command) {
			return SafetyCheckResult{
				Level:    SafetyLevelForbidden,
				Reason:   "Command matches forbidden pattern that could cause system damage",
				Category: "forbidden_pattern",
				Suggestions: []string{
					"This command has been blocked for safety",
					"If you need to perform this action, please do it manually",
				},
			}
		}
	}

	// Check for protected paths
	if result := v.checkProtectedPaths(command); result.Level != SafetyLevelSafe {
		return result
	}

	// Check dangerous patterns
	for _, pattern := range v.dangerousPatterns {
		if pattern.MatchString(command) {
			return SafetyCheckResult{
				Level:    SafetyLevelDangerous,
				Reason:   "Command modifies system configuration and requires explicit confirmation",
				Category: "dangerous_pattern",
				Suggestions: []string{
					"Review the command carefully before proceeding",
					"Consider backing up affected files first",
				},
			}
		}
	}

	// Check warning patterns
	for _, pattern := range v.warningPatterns {
		if pattern.MatchString(command) {
			return SafetyCheckResult{
				Level:    SafetyLevelWarning,
				Reason:   "Command may have significant side effects",
				Category: "warning_pattern",
				Suggestions: []string{
					"Proceed with caution",
				},
			}
		}
	}

	// Check for dangerous commands
	words := strings.Fields(command)
	if len(words) > 0 {
		baseCmd := filepath.Base(words[0])
		if v.dangerousCommands[baseCmd] {
			return SafetyCheckResult{
				Level:    SafetyLevelWarning,
				Reason:   fmt.Sprintf("Command '%s' is flagged as potentially dangerous", baseCmd),
				Category: "dangerous_command",
			}
		}
	}

	return SafetyCheckResult{
		Level:    SafetyLevelSafe,
		Reason:   "Command appears safe to execute",
		Category: "safe",
	}
}

// checkProtectedPaths checks if command affects protected paths
func (v *CommandSafetyValidator) checkProtectedPaths(command string) SafetyCheckResult {
	// Check for operations on protected paths
	for _, path := range v.protectedPaths {
		// Check for removal of protected paths
		if strings.Contains(command, "rm ") && strings.Contains(command, path) {
			return SafetyCheckResult{
				Level:    SafetyLevelForbidden,
				Reason:   fmt.Sprintf("Command attempts to remove protected path: %s", path),
				Category: "protected_path",
				Suggestions: []string{
					"Removing system directories is not allowed",
					"If you need to clean up, use package manager instead",
				},
			}
		}

		// Check for modification of protected paths
		if (strings.Contains(command, ">") || strings.Contains(command, ">>")) &&
			strings.Contains(command, path) {
			return SafetyCheckResult{
				Level:    SafetyLevelDangerous,
				Reason:   fmt.Sprintf("Command attempts to write to protected path: %s", path),
				Category: "protected_path",
				Suggestions: []string{
					"Writing to system directories requires explicit confirmation",
				},
			}
		}
	}

	// Check for operations on protected files
	for _, file := range v.protectedFiles {
		if strings.Contains(command, file) {
			// Reading is okay, writing is not
			if strings.Contains(command, ">") || strings.Contains(command, ">>") ||
				strings.Contains(command, "rm ") || strings.Contains(command, "mv ") {
				return SafetyCheckResult{
					Level:    SafetyLevelForbidden,
					Reason:   fmt.Sprintf("Command attempts to modify protected file: %s", file),
					Category: "protected_file",
					Suggestions: []string{
						"Modifying critical system files is not allowed",
						"If you need to change system configuration, do it manually",
					},
				}
			}
		}
	}

	return SafetyCheckResult{Level: SafetyLevelSafe}
}

// ValidateCommands validates multiple commands and returns the most severe result
func (v *CommandSafetyValidator) ValidateCommands(commands []string) SafetyCheckResult {
	worstLevel := SafetyLevelSafe
	worstResult := SafetyCheckResult{Level: SafetyLevelSafe}

	for _, cmd := range commands {
		result := v.ValidateCommand(cmd)
		if result.Level > worstLevel {
			worstLevel = result.Level
			worstResult = result
		}
	}

	return worstResult
}

// SanitizeCommand attempts to make a command safer
func (v *CommandSafetyValidator) SanitizeCommand(command string) (string, []string) {
	var warnings []string

	// Remove any trailing semicolons that might hide additional commands
	command = strings.TrimSuffix(strings.TrimSpace(command), ";")

	// Check for multiple commands separated by ; or &&
	if strings.Contains(command, ";") && !strings.Contains(command, "\"") {
		warnings = append(warnings, "Multiple commands detected - please verify each command")
	}

	// Check for command substitution that might be dangerous
	if strings.Contains(command, "$(") || strings.Contains(command, "`") {
		warnings = append(warnings, "Command contains substitution - please verify the result")
	}

	return command, warnings
}

// IsInteractiveCommand checks if a command requires user interaction
func (v *CommandSafetyValidator) IsInteractiveCommand(command string) bool {
	interactivePatterns := []string{
		`apt\s+install`,
		`apt-get\s+install`,
		`yum\s+install`,
		`dnf\s+install`,
		`pacman\s+-S`,
		`pip\s+install`,
		`npm\s+install`,
		`winget\s+install`,
		`brew\s+install`,
	}

	for _, pattern := range interactivePatterns {
		if matched, _ := regexp.MatchString(pattern, command); matched {
			return true
		}
	}

	return false
}

// GetSafeAlternative suggests safer alternatives for dangerous commands
func (v *CommandSafetyValidator) GetSafeAlternative(command string) string {
	// Suggest alternatives for common dangerous patterns
	alternatives := map[string]string{
		"rm -rf /":           "This command is blocked. Use specific paths instead.",
		"chmod 777":          "Consider using more restrictive permissions like 755 or 644",
		"curl | bash":        "Download the script first, review it, then execute",
		"wget | bash":        "Download the script first, review it, then execute",
	}

	for pattern, alternative := range alternatives {
		if strings.Contains(command, pattern) {
			return alternative
		}
	}

	return ""
}

// SafeExecute wraps command execution with safety checks
func (a *InstallAgent) SafeExecute(ctx context.Context, command string, onConfirm func() bool) (*ShellResult, error) {
	safetyResult := a.safetyValidator.ValidateCommand(command)

	switch safetyResult.Level {
	case SafetyLevelForbidden:
		return nil, fmt.Errorf("command blocked for safety: %s\n%s",
			safetyResult.Reason, strings.Join(safetyResult.Suggestions, "\n"))

	case SafetyLevelDangerous:
		// Requires explicit confirmation
		if onConfirm == nil || !onConfirm() {
			return nil, fmt.Errorf("dangerous command rejected: %s", command)
		}

	case SafetyLevelWarning:
		// Log warning but proceed
		if a.context != nil {
			a.context.Warnings = append(a.context.Warnings,
				fmt.Sprintf("Safety warning for command '%s': %s", command, safetyResult.Reason))
		}
	}

	// Execute the command
	result := a.executeShellCommand(ctx, command, "safe_execute")
	return result, nil
}

// ValidateCommandSafety is a convenience method to validate command safety
func (a *InstallAgent) ValidateCommandSafety(command string) SafetyCheckResult {
	if a.safetyValidator == nil {
		a.safetyValidator = NewCommandSafetyValidator()
	}
	return a.safetyValidator.ValidateCommand(command)
}

// ValidateCommandsSafety validates multiple commands
func (a *InstallAgent) ValidateCommandsSafety(commands []string) SafetyCheckResult {
	if a.safetyValidator == nil {
		a.safetyValidator = NewCommandSafetyValidator()
	}
	return a.safetyValidator.ValidateCommands(commands)
}
