package installer

import (
	"time"
)

// InstallPhase represents the current phase of installation
type InstallPhase string

const (
	PhaseQuery        InstallPhase = "query"
	PhaseVerify       InstallPhase = "verify"
	PhaseResolve      InstallPhase = "resolve"
	PhaseDownload     InstallPhase = "download"
	PhaseInstallDeps  InstallPhase = "install_deps"
	PhaseInstall      InstallPhase = "install"
	PhaseConfig       InstallPhase = "config"
	PhaseVerifyInstall InstallPhase = "verify_install"
	PhaseComplete     InstallPhase = "complete"
)

// InstallProgress represents installation progress
type InstallProgress struct {
	Phase     InstallPhase `json:"phase"`
	Message   string       `json:"message"`
	Percent   int          `json:"percent"`
	Timestamp time.Time    `json:"timestamp"`
	Error     string       `json:"error,omitempty"`
}

// InstallResult represents the result of an installation
type InstallResult struct {
	PackageID    string        `json:"packageId"`
	Version      string        `json:"version"`
	Success      bool          `json:"success"`
	Method       string        `json:"method"`
	InstallPath  string        `json:"installPath,omitempty"`
	BinaryPath   string        `json:"binaryPath,omitempty"`
	ConfigPath   string        `json:"configPath,omitempty"`
	Duration     time.Duration `json:"duration"`
	Error        string        `json:"error,omitempty"`
	Warnings     []string      `json:"warnings,omitempty"`
	Dependencies []*InstallResult `json:"dependencies,omitempty"`
	StartTime    time.Time     `json:"startTime"`
	EndTime      time.Time     `json:"endTime"`
}

// InstallType defines the installation type
type InstallType string

const (
	InstallTypeBinary  InstallType = "binary"
	InstallTypePackage InstallType = "package"
	InstallTypeScript  InstallType = "script"
	InstallTypeSource  InstallType = "source"
)
