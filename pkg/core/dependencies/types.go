// Package dependencies provides software dependency management and environment validation
package dependencies

import "time"

// DependencyNode represents a node in the dependency tree
type DependencyNode struct {
	// Basic info
	ID              string `yaml:"id" json:"id"`
	Name            string `yaml:"name" json:"name"`
	Description     string `yaml:"description,omitempty" json:"description,omitempty"`
	VersionRequired string `yaml:"version_required,omitempty" json:"version_required,omitempty"`

	// Environment requirements
	Requirements EnvRequirements `yaml:"requirements,omitempty" json:"requirements,omitempty"`

	// Child dependencies
	Dependencies []*DependencyNode `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`

	// Platform-specific configuration
	Platforms map[string]PlatformConfig `yaml:"platforms,omitempty" json:"platforms,omitempty"`

	// Version-specific requirements (key: version pattern like "18+", "16.x")
	VersionRequirements map[string]EnvRequirements `yaml:"version_requirements,omitempty" json:"version_requirements,omitempty"`

	// What this package provides (for reverse dependency lookup)
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`

	// Validation result (populated during validation)
	ValidationResult *DepValidationResult `yaml:"validation_result,omitempty" json:"validation_result,omitempty"`

	// Install methods
	InstallMethods []InstallMethod `yaml:"install_methods,omitempty" json:"install_methods,omitempty"`
}

// EnvRequirements defines environment requirements for a package
type EnvRequirements struct {
	// Supported operating systems (empty = all supported)
	OS []string `yaml:"os,omitempty" json:"os,omitempty"`

	// Supported architectures (empty = all supported)
	Arch []string `yaml:"arch,omitempty" json:"arch,omitempty"`

	// Minimum required versions of system libraries
	MinVersions map[string]string `yaml:"min_versions,omitempty" json:"min_versions,omitempty"`

	// Maximum supported versions (for compatibility limits)
	MaxVersions map[string]string `yaml:"max_versions,omitempty" json:"max_versions,omitempty"`

	// Required environment variables
	EnvVars []string `yaml:"env_vars,omitempty" json:"env_vars,omitempty"`

	// Minimum disk space in MB
	MinDiskSpace int `yaml:"min_disk_space,omitempty" json:"min_disk_space,omitempty"`

	// Minimum memory in MB
	MinMemory int `yaml:"min_memory,omitempty" json:"min_memory,omitempty"`
}

// PlatformConfig defines platform-specific installation configuration
type PlatformConfig struct {
	// Distro-specific config (for Linux)
	Distros map[string]DistroConfig `yaml:"distros,omitempty" json:"distros,omitempty"`

	// Install methods for this platform
	InstallMethods []InstallMethod `yaml:"install_methods,omitempty" json:"install_methods,omitempty"`

	// Platform-specific requirements override
	Requirements *EnvRequirements `yaml:"requirements,omitempty" json:"requirements,omitempty"`
}

// DistroConfig defines distribution-specific configuration
type DistroConfig struct {
	// Package manager to use
	PackageManager string `yaml:"package_manager,omitempty" json:"package_manager,omitempty"`

	// Install methods specific to this distro
	InstallMethods []InstallMethod `yaml:"install_methods,omitempty" json:"install_methods,omitempty"`

	// Distro version-specific limitations
	VersionLimits []VersionLimit `yaml:"version_limits,omitempty" json:"version_limits,omitempty"`
}

// VersionLimit defines a version limitation for a specific OS/distro version
type VersionLimit struct {
	// OS/Distro version constraint (e.g., "<=7" for CentOS 7 and below)
	OSVersion string `yaml:"os_version,omitempty" json:"os_version,omitempty"`

	// Maximum software version supported on this OS version
	MaxSoftwareVersion string `yaml:"max_software_version,omitempty" json:"max_software_version,omitempty"`

	// Reason for the limitation
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`

	// Suggested workaround
	Workaround string `yaml:"workaround,omitempty" json:"workaround,omitempty"`
}

// InstallMethod defines how to install a package
type InstallMethod struct {
	// Type of installation (binary, package_manager, script, npm, pip, etc.)
	Type string `yaml:"type" json:"type"`

	// Priority (lower = preferred)
	Priority int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// Install command(s)
	Commands []string `yaml:"commands,omitempty" json:"commands,omitempty"`

	// Verification command
	VerifyCmd string `yaml:"verify_cmd,omitempty" json:"verify_cmd,omitempty"`

	// Whether sudo is required
	SudoRequired bool `yaml:"sudo_required,omitempty" json:"sudo_required,omitempty"`

	// Whether this method is recommended
	Recommended bool `yaml:"recommended,omitempty" json:"recommended,omitempty"`

	// Notes about this method
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
}

// DepValidationResult represents the validation result for a dependency
type DepValidationResult struct {
	// Validation status
	Status DepStatus `json:"status"`

	// Current installed version (if any)
	CurrentVersion string `json:"current_version,omitempty"`

	// Target version to install
	TargetVersion string `json:"target_version,omitempty"`

	// Reason if blocked
	BlockReason string `json:"block_reason,omitempty"`

	// Suggested install method
	InstallMethod *InstallMethod `json:"install_method,omitempty"`

	// Warnings (non-blocking issues)
	Warnings []string `json:"warnings,omitempty"`

	// Whether web search is needed to determine requirements
	NeedsWebSearch bool `json:"needs_web_search,omitempty"`

	// Suggested web search query
	WebSearchQuery string `json:"web_search_query,omitempty"`
}

// DepStatus represents the status of a dependency validation
type DepStatus string

const (
	// DepSatisfied - dependency is already installed and meets requirements
	DepSatisfied DepStatus = "satisfied"

	// DepInstallable - dependency can be automatically installed
	DepInstallable DepStatus = "installable"

	// DepBlocked - dependency is blocked by system limitations
	DepBlocked DepStatus = "blocked"

	// DepUnknown - cannot determine status, needs further investigation
	DepUnknown DepStatus = "unknown"

	// DepConflict - conflicts with existing installation
	DepConflict DepStatus = "conflict"
)

// DependencyType defines the type of dependency relationship
type DependencyType string

const (
	// DepTypeRequired - must be installed
	DepTypeRequired DependencyType = "required"

	// DepTypeOptional - nice to have but not required
	DepTypeOptional DependencyType = "optional"

	// DepTypeRecommended - recommended for best experience
	DepTypeRecommended DependencyType = "recommended"

	// DepTypeBuild - required for building from source
	DepTypeBuild DependencyType = "build"
)

// DependencyRef references another dependency in the dependency tree
type DependencyRef struct {
	// ID of the dependency
	ID string `yaml:"id" json:"id"`

	// Version constraint (e.g., ">=18.0.0", "16.x")
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	// Type of dependency
	Type DependencyType `yaml:"type,omitempty" json:"type,omitempty"`
}

// SystemInfo contains system information for validation
type SystemInfo struct {
	// Operating system (linux, darwin, windows)
	OS string `json:"os"`

	// OS version/distro (e.g., "centos", "ubuntu", "windows 10")
	Distro string `json:"distro,omitempty"`

	// OS version number (e.g., "7", "22.04", "10.0.19045")
	OSVersion string `json:"os_version,omitempty"`

	// Architecture (amd64, arm64)
	Arch string `json:"arch"`

	// Package manager (apt, yum, dnf, brew, winget, etc.)
	PackageManager string `json:"package_manager,omitempty"`

	// System library versions
	LibraryVersions map[string]string `json:"library_versions,omitempty"`

	// Available disk space in MB
	AvailableDiskSpace int `json:"available_disk_space,omitempty"`

	// Available memory in MB
	AvailableMemory int `json:"available_memory,omitempty"`

	// Timestamp of when this info was collected
	CollectedAt time.Time `json:"collected_at"`
}

// ValidationResult represents the overall validation result
type ValidationResult struct {
	// Whether the installation can proceed
	CanProceed bool `json:"can_proceed"`

	// Dependencies that need to be installed
	ToInstall []*DependencyNode `json:"to_install,omitempty"`

	// Blocking issues
	Blockers []Blocker `json:"blockers,omitempty"`

	// Non-blocking warnings
	Warnings []string `json:"warnings,omitempty"`

	// System info snapshot
	SystemInfo *SystemInfo `json:"system_info,omitempty"`

	// Installation order (topologically sorted)
	InstallOrder []string `json:"install_order,omitempty"`
}

// Blocker represents a blocking issue that prevents installation
type Blocker struct {
	// Package that has the blocker
	Package string `json:"package"`

	// Type of blocker
	Type string `json:"type"`

	// Description of the blocker
	Description string `json:"description"`

	// What is required
	Required string `json:"required,omitempty"`

	// What is currently available
	Current string `json:"current,omitempty"`

	// Suggested workaround
	Workaround string `json:"workaround,omitempty"`
}

// MemoryEntry represents a learned dependency rule stored in memory
type MemoryEntry struct {
	// Package ID
	ID string `yaml:"id" json:"id"`

	// When this was learned
	LearnedAt time.Time `yaml:"learned_at" json:"learned_at"`

	// Source of this knowledge (install_success, user_defined, etc.)
	Source string `yaml:"source" json:"source"`

	// The dependency node
	Dependency DependencyNode `yaml:"dependency" json:"dependency"`

	// Number of times successfully used
	SuccessCount int `yaml:"success_count" json:"success_count"`

	// Last successful use
	LastUsed time.Time `yaml:"last_used,omitempty" json:"last_used,omitempty"`
}
