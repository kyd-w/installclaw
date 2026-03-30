// Package metadata defines core data structures for the universal installer
package metadata

import (
	"time"
)

// PackageMetadata describes a software package's complete metadata
type PackageMetadata struct {
	// Basic Information
	ID          string   `json:"id" yaml:"id"`                   // Unique identifier, e.g., "nodejs", "redis"
	Name        string   `json:"name" yaml:"name"`               // Display name
	Version     string   `json:"version" yaml:"version"`         // Version string
	Description string   `json:"description" yaml:"description"` // Description
	Homepage    string   `json:"homepage" yaml:"homepage"`       // Official website URL
	License     string   `json:"license" yaml:"license"`         // License type
	Category    string   `json:"category" yaml:"category"`       // "runtime", "dev-tool", "ai-tool", "database", etc.
	Tags        []string `json:"tags" yaml:"tags"`               // Search tags
	Keywords    []string `json:"keywords" yaml:"keywords"`       // Search keywords

	// Source Information
	Sources []PackageSource `json:"sources" yaml:"sources"` // Download sources

	// Installation Information
	InstallMethods []InstallMethod `json:"installMethods" yaml:"installMethods"` // Installation methods
	Dependencies   []Dependency    `json:"dependencies" yaml:"dependencies"`     // Prerequisites
	Conflicts      []Conflict      `json:"conflicts" yaml:"conflicts"`           // Conflicting packages

	// Configuration
	ConfigTemplates []ConfigTemplate `json:"configTemplates" yaml:"configTemplates"` // Config file templates

	// Verification
	Checksum  string `json:"checksum,omitempty" yaml:"checksum,omitempty"`   // SHA256 checksum
	Signature string `json:"signature,omitempty" yaml:"signature,omitempty"` // GPG signature

	// Metadata
	MinVersion   string    `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`   // Minimum supported version
	MaxVersion   string    `json:"maxVersion,omitempty" yaml:"maxVersion,omitempty"`   // Maximum supported version
	LastUpdated  time.Time `json:"lastUpdated" yaml:"lastUpdated"`                     // Last update time
	Author       string    `json:"author,omitempty" yaml:"author,omitempty"`           // Author
	Maintainers  []string  `json:"maintainers,omitempty" yaml:"maintainers,omitempty"` // Maintainers
	Deprecated   bool      `json:"deprecated" yaml:"deprecated"`                       // Is deprecated
	ReplacedBy   string    `json:"replacedBy,omitempty" yaml:"replacedBy,omitempty"`   // Replacement package ID
}

// SourceType defines the type of package source
type SourceType string

const (
	SourceGitHub   SourceType = "github"   // GitHub repository
	SourceOfficial SourceType = "official" // Official website
	SourceNPM      SourceType = "npm"      // NPM registry
	SourcePyPI     SourceType = "pypi"     // Python PyPI
	SourceGo       SourceType = "go"       // Go modules
	SourceCargo    SourceType = "cargo"    // Rust Cargo
	SourceHomebrew SourceType = "homebrew" // Homebrew
	SourceApt      SourceType = "apt"      // Debian/Ubuntu APT
	SourceYum      SourceType = "yum"      // RHEL/CentOS YUM
	SourceCustom   SourceType = "custom"   // Custom source
)

// PackageSource describes a download source for a package
type PackageSource struct {
	Type         SourceType  `json:"type" yaml:"type"`                           // Source type
	URL          string      `json:"url" yaml:"url"`                             // Download URL or repo URL
	Priority     int         `json:"priority" yaml:"priority"`                   // Priority (lower = higher priority)
	Stars        int         `json:"stars,omitempty" yaml:"stars,omitempty"`     // GitHub stars (for GitHub sources)
	MinStars     int         `json:"minStars,omitempty" yaml:"minStars,omitempty"` // Minimum stars requirement (default 200)
	Trusted      bool        `json:"trusted" yaml:"trusted"`                     // Is this source trusted
	AuthRequired bool        `json:"authRequired,omitempty" yaml:"authRequired,omitempty"` // Requires authentication
	ReleaseInfo  *ReleaseInfo `json:"releaseInfo,omitempty" yaml:"releaseInfo,omitempty"` // Release information
	ChecksumURL  string      `json:"checksumUrl,omitempty" yaml:"checksumUrl,omitempty"` // Checksum file URL
}

// ReleaseInfo describes a software release
type ReleaseInfo struct {
	Version     string    `json:"version" yaml:"version"`           // Version string
	PublishedAt time.Time `json:"publishedAt" yaml:"publishedAt"`   // Publish time
	Assets      []Asset   `json:"assets" yaml:"assets"`             // Downloadable assets
	Changelog   string    `json:"changelog,omitempty" yaml:"changelog,omitempty"` // Changelog
	Prerelease  bool      `json:"prerelease" yaml:"prerelease"`     // Is prerelease
	Latest      bool      `json:"latest" yaml:"latest"`             // Is latest release
}

// Asset describes a downloadable file
type Asset struct {
	ID          string `json:"id" yaml:"id"`                   // Asset ID
	Name        string `json:"name" yaml:"name"`               // File name
	OS          string `json:"os" yaml:"os"`                   // Target OS: "linux", "darwin", "windows", "all"
	Arch        string `json:"arch" yaml:"arch"`               // Target arch: "amd64", "arm64", "x86", "all"
	URL         string `json:"url" yaml:"url"`                 // Download URL
	Checksum    string `json:"checksum,omitempty" yaml:"checksum,omitempty"`       // SHA256 checksum
	Signature   string `json:"signature,omitempty" yaml:"signature,omitempty"`     // GPG signature
	Size        int64  `json:"size,omitempty" yaml:"size,omitempty"`               // File size in bytes
	ContentType string `json:"contentType,omitempty" yaml:"contentType,omitempty"` // MIME type
}

// InstallType defines the installation method type
type InstallType string

const (
	InstallBinary  InstallType = "binary"  // Binary download
	InstallPackage InstallType = "package" // Package manager (npm, pip, etc.)
	InstallScript  InstallType = "script"  // Installation script
	InstallSource  InstallType = "source"  // Build from source
)

// InstallMethod describes an installation method
type InstallMethod struct {
	Type         InstallType          `json:"type" yaml:"type"`                           // Installation type
	Name         string               `json:"name" yaml:"name"`                           // Method name
	Description  string               `json:"description,omitempty" yaml:"description,omitempty"` // Description
	Priority     int                  `json:"priority" yaml:"priority"`                   // Execution priority (lower = higher)
	Platform     PlatformConstraint   `json:"platform,omitempty" yaml:"platform,omitempty"` // Platform constraints
	Commands     []string             `json:"commands" yaml:"commands"`                   // Commands to execute
	WorkingDir   string               `json:"workingDir,omitempty" yaml:"workingDir,omitempty"` // Working directory
	Timeout      int                  `json:"timeout,omitempty" yaml:"timeout,omitempty"` // Timeout in seconds
	EnvVars      map[string]string    `json:"envVars,omitempty" yaml:"envVars,omitempty"` // Environment variables
	PostInstall  []PostInstallAction  `json:"postInstall,omitempty" yaml:"postInstall,omitempty"` // Post-install actions
	VerifyCmd    string               `json:"verifyCmd,omitempty" yaml:"verifyCmd,omitempty"` // Verification command
	RollbackCmd  string               `json:"rollbackCmd,omitempty" yaml:"rollbackCmd,omitempty"` // Rollback command
	SudoRequired bool                 `json:"sudoRequired,omitempty" yaml:"sudoRequired,omitempty"` // Requires sudo
}

// PlatformConstraint defines platform constraints for an install method
type PlatformConstraint struct {
	OS   []string `json:"os,omitempty" yaml:"os,omitempty"`     // Allowed OS list (empty = all)
	Arch []string `json:"arch,omitempty" yaml:"arch,omitempty"` // Allowed arch list (empty = all)
}

// PostInstallAction describes a post-installation action
type PostInstallAction struct {
	Name        string            `json:"name" yaml:"name"`                           // Action name
	Description string            `json:"description,omitempty" yaml:"description,omitempty"` // Description
	Commands    []string          `json:"commands" yaml:"commands"`                   // Commands to execute
	EnvVars     map[string]string `json:"envVars,omitempty" yaml:"envVars,omitempty"` // Environment variables
}

// DependencyType defines the dependency relationship type
type DependencyType string

const (
	DependencyRequired  DependencyType = "required"  // Required dependency
	DependencyOptional  DependencyType = "optional"  // Optional dependency
	DependencyRecommended DependencyType = "recommended" // Recommended dependency
)

// Dependency describes a package dependency
type Dependency struct {
	PackageID   string         `json:"packageId" yaml:"packageId"`             // Dependency package ID
	Type        DependencyType `json:"type" yaml:"type"`                       // Dependency type
	Version     string         `json:"version,omitempty" yaml:"version,omitempty"` // Version constraint (semver)
	Condition   string         `json:"condition,omitempty" yaml:"condition,omitempty"` // Conditional expression
	Feature     string         `json:"feature,omitempty" yaml:"feature,omitempty"` // Related feature
}

// Conflict describes a package conflict
type Conflict struct {
	PackageID string `json:"packageId" yaml:"packageId"` // Conflicting package ID
	Version   string `json:"version,omitempty" yaml:"version,omitempty"` // Version range
	Reason    string `json:"reason,omitempty" yaml:"reason,omitempty"` // Conflict reason
}

// ConfigTemplate describes a configuration file template
type ConfigTemplate struct {
	ID              string                 `json:"id" yaml:"id"`                               // Template ID
	Name            string                 `json:"name" yaml:"name"`                           // Template name
	Description     string                 `json:"description,omitempty" yaml:"description,omitempty"` // Description
	TargetPath      string                 `json:"targetPath" yaml:"targetPath"`               // Target file path (supports ~ expansion)
	Content         string                 `json:"content,omitempty" yaml:"content,omitempty"` // Template content
	TemplateFile    string                 `json:"templateFile,omitempty" yaml:"templateFile,omitempty"` // External template file path
	Permissions     string                 `json:"permissions,omitempty" yaml:"permissions,omitempty"` // File permissions (e.g., "0644")
	Variables       map[string]interface{} `json:"variables,omitempty" yaml:"variables,omitempty"` // Template variables
	Condition       string                 `json:"condition,omitempty" yaml:"condition,omitempty"` // Conditional generation
	Overwrite       bool                   `json:"overwrite" yaml:"overwrite"`                 // Overwrite existing file
	BackupExisting  bool                   `json:"backupExisting" yaml:"backupExisting"`       // Backup existing file
}

// InstallResult describes the result of an installation
type InstallResult struct {
	Success      bool              `json:"success" yaml:"success"`           // Installation success
	PackageID    string            `json:"packageId" yaml:"packageId"`       // Package ID
	Version      string            `json:"version" yaml:"version"`           // Installed version
	InstallPath  string            `json:"installPath" yaml:"installPath"`   // Installation path
	BinaryPath   string            `json:"binaryPath" yaml:"binaryPath"`     // Binary path (if applicable)
	ConfigPath   string            `json:"configPath" yaml:"configPath"`     // Config file path
	Method       InstallType       `json:"method" yaml:"method"`             // Installation method used
	Duration     time.Duration     `json:"duration" yaml:"duration"`         // Installation duration
	Error        string            `json:"error,omitempty" yaml:"error,omitempty"` // Error message
	Warnings     []string          `json:"warnings,omitempty" yaml:"warnings,omitempty"` // Warnings
	Dependencies []DependencyResult `json:"dependencies,omitempty" yaml:"dependencies,omitempty"` // Dependency results
}

// DependencyResult describes the result of a dependency installation
type DependencyResult struct {
	PackageID string `json:"packageId" yaml:"packageId"` // Package ID
	Success   bool   `json:"success" yaml:"success"`     // Installation success
	Error     string `json:"error,omitempty" yaml:"error,omitempty"` // Error message
}
