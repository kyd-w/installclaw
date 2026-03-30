// Package system provides system-level detection utilities
package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// InstalledSoftware represents detected installed software
type InstalledSoftware struct {
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Path          string   `json:"path,omitempty"`
	InstallMethod string   `json:"installMethod,omitempty"`
	IsInstalled   bool     `json:"isInstalled"`
	LatestVersion string   `json:"latestVersion,omitempty"`
	NeedsUpdate   bool     `json:"needsUpdate"`
}

// SoftwareDetector detects installed software on the system
type SoftwareDetector struct {
	timeout time.Duration
}

// NewSoftwareDetector creates a new software detector
func NewSoftwareDetector() *SoftwareDetector {
	return &SoftwareDetector{
		timeout: 10 * time.Second,
	}
}

// KnownSoftware contains detection rules for common software
var KnownSoftware = map[string]SoftwareDetection{
	// Runtimes
	"nodejs": {
		Commands: []string{"node --version", "node -v"},
		VersionRegex: `v?(\d+\.\d+\.\d+)`,
		PathCommand: "which node; where node",
	},
	"python": {
		Commands: []string{"python3 --version", "python --version"},
		VersionRegex: `Python (\d+\.\d+\.\d+)`,
		PathCommand: "which python3; where python",
	},
	"go": {
		Commands: []string{"go version"},
		VersionRegex: `go(\d+\.\d+(?:\.\d+)?)`,
		PathCommand: "which go; where go",
	},
	"rust": {
		Commands: []string{"rustc --version"},
		VersionRegex: `rustc (\d+\.\d+\.\d+)`,
		PathCommand: "which rustc; where rustc",
	},
	"java": {
		Commands: []string{"java -version"},
		VersionRegex: `version "?(\d+(?:\.\d+)?)`,
		PathCommand: "which java; where java",
	},
	"ruby": {
		Commands: []string{"ruby --version"},
		VersionRegex: `ruby (\d+\.\d+\.\d+)`,
		PathCommand: "which ruby; where ruby",
	},
	"php": {
		Commands: []string{"php --version"},
		VersionRegex: `PHP (\d+\.\d+\.\d+)`,
		PathCommand: "which php; where php",
	},

	// Package Managers
	"npm": {
		Commands: []string{"npm --version"},
		VersionRegex: `(\d+\.\d+\.\d+)`,
	},
	"yarn": {
		Commands: []string{"yarn --version"},
		VersionRegex: `(\d+\.\d+\.\d+)`,
	},
	"pnpm": {
		Commands: []string{"pnpm --version"},
		VersionRegex: `(\d+\.\d+\.\d+)`,
	},
	"pip": {
		Commands: []string{"pip --version", "pip3 --version"},
		VersionRegex: `pip (\d+\.\d+(?:\.\d+)?)`,
	},
	"pipx": {
		Commands: []string{"pipx --version"},
		VersionRegex: `(\d+\.\d+\.\d+)`,
	},
	"cargo": {
		Commands: []string{"cargo --version"},
		VersionRegex: `cargo (\d+\.\d+\.\d+)`,
	},

	// Databases
	"redis": {
		Commands: []string{"redis-server --version"},
		VersionRegex: `v=(\d+\.\d+\.\d+)`,
	},
	"mongodb": {
		Commands: []string{"mongod --version"},
		VersionRegex: `db version v?(\d+\.\d+\.\d+)`,
	},
	"postgresql": {
		Commands: []string{"psql --version"},
		VersionRegex: `PostgreSQL (\d+(?:\.\d+)?)`,
	},
	"mysql": {
		Commands: []string{"mysql --version"},
		VersionRegex: `Ver (\d+\.\d+\.\d+)`,
	},
	"sqlite": {
		Commands: []string{"sqlite3 --version"},
		VersionRegex: `(\d+\.\d+\.\d+)`,
	},

	// DevOps & Infrastructure
	"docker": {
		Commands: []string{"docker --version"},
		VersionRegex: `Docker version (\d+\.\d+\.\d+)`,
	},
	"kubectl": {
		Commands: []string{"kubectl version --client"},
		VersionRegex: `GitVersion:"?v?(\d+\.\d+\.\d+)`,
	},
	"terraform": {
		Commands: []string{"terraform version"},
		VersionRegex: `Terraform v?(\d+\.\d+\.\d+)`,
	},
	"ansible": {
		Commands: []string{"ansible --version"},
		VersionRegex: `ansible \[core (\d+\.\d+\.\d+)`,
	},
	"helm": {
		Commands: []string{"helm version"},
		VersionRegex: `Version:"v?(\d+\.\d+\.\d+)"`,
	},

	// Web Servers
	"nginx": {
		Commands: []string{"nginx -v"},
		VersionRegex: `nginx version: nginx/(\d+\.\d+\.\d+)`,
	},
	"apache": {
		Commands: []string{"apache2 -v", "httpd -v"},
		VersionRegex: `Apache/(\d+\.\d+\.\d+)`,
	},
	"caddy": {
		Commands: []string{"caddy version"},
		VersionRegex: `v?(\d+\.\d+\.\d+)`,
	},

	// Tools
	"git": {
		Commands: []string{"git --version"},
		VersionRegex: `git version (\d+\.\d+\.\d+)`,
	},
	"curl": {
		Commands: []string{"curl --version"},
		VersionRegex: `curl (\d+\.\d+\.\d+)`,
	},
	"wget": {
		Commands: []string{"wget --version"},
		VersionRegex: `Wget (\d+\.\d+)`,
	},
	"make": {
		Commands: []string{"make --version"},
		VersionRegex: `GNU Make (\d+\.\d+)`,
	},
	"cmake": {
		Commands: []string{"cmake --version"},
		VersionRegex: `cmake version (\d+\.\d+\.\d+)`,
	},

	// Editors
	"vim": {
		Commands: []string{"vim --version"},
		VersionRegex: `VIM - Vi IMproved (\d+\.\d+)`,
	},
	"nano": {
		Commands: []string{"nano --version"},
		VersionRegex: `GNU nano version (\d+\.\d+)`,
	},

	// AI Tools
	"ollama": {
		Commands: []string{"ollama --version"},
		VersionRegex: `ollama version (\d+\.\d+\.\d+)`,
	},
}

// SoftwareDetection contains detection rules
type SoftwareDetection struct {
	Commands     []string `json:"commands"`
	VersionRegex string   `json:"versionRegex"`
	PathCommand  string   `json:"pathCommand,omitempty"`
}

// Detect checks if software is installed and returns version info
func (d *SoftwareDetector) Detect(name string) *InstalledSoftware {
	result := &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}

	// Normalize name
	normalizedName := strings.ToLower(strings.ReplaceAll(name, "-", ""))

	// Find detection rules
	detection, ok := KnownSoftware[normalizedName]
	if !ok {
		// Try fuzzy matching
		for key, det := range KnownSoftware {
			if strings.Contains(key, normalizedName) || strings.Contains(normalizedName, key) {
				detection = det
				ok = true
				break
			}
		}
	}

	if ok {
		// Try each detection command
		for _, cmd := range detection.Commands {
			version, path, err := d.runDetection(cmd, detection.VersionRegex)
			if err == nil && version != "" {
				result.IsInstalled = true
				result.Version = version
				result.Path = path
				result.InstallMethod = d.detectInstallMethod(name, path)
				return result
			}
		}
	}

	// Try platform-specific detection (GUI apps, package managers, etc.)
	platformResult := d.DetectPlatformApp(name)
	if platformResult.IsInstalled {
		return platformResult
	}

	// Generic detection - try common command patterns
	return d.detectGeneric(name)
}

// detectGeneric tries generic detection for unknown software
func (d *SoftwareDetector) detectGeneric(name string) *InstalledSoftware {
	result := &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}

	// Try common command patterns
	commands := []string{
		fmt.Sprintf("%s --version", name),
		fmt.Sprintf("%s -v", name),
		fmt.Sprintf("%s version", name),
		fmt.Sprintf("%s --version", strings.ToLower(name)),
		fmt.Sprintf("%s -v", strings.ToLower(name)),
	}

	for _, cmd := range commands {
		version, path, err := d.runDetection(cmd, `(\d+(?:\.\d+)+(?:[-_][a-zA-Z0-9]+)?)`)
		if err == nil && version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			return result
		}
	}

	return result
}

// runDetection executes a detection command and extracts version
func (d *SoftwareDetector) runDetection(cmdStr, versionRegex string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", "", err
	}

	output := stdout.String() + stderr.String()

	// Extract version
	re := regexp.MustCompile(versionRegex)
	matches := re.FindStringSubmatch(output)
	if len(matches) > 1 {
		version := matches[1]
		path := d.findPath(parts[0])
		return version, path, nil
	}

	return "", "", fmt.Errorf("version not found in output")
}

// findPath finds the path of an executable
func (d *SoftwareDetector) findPath(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try 'which' (Unix) or 'where' (Windows)
	for _, cmdName := range []string{"which", "where"} {
		cmd := exec.CommandContext(ctx, cmdName, name)
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return lines[0]
			}
		}
	}

	return ""
}

// detectInstallMethod tries to determine how software was installed
func (d *SoftwareDetector) detectInstallMethod(name, path string) string {
	if path == "" {
		return "unknown"
	}

	path = strings.ToLower(path)

	// Check common install locations
	switch {
	case strings.Contains(path, "/usr/local/Cellar/") || strings.Contains(path, "homebrew"):
		return "homebrew"
	case strings.Contains(path, "/usr/local/") || strings.Contains(path, "c:\\program files\\"):
		return "binary"
	case strings.Contains(path, "/usr/bin/") || strings.Contains(path, "/bin/"):
		return "package-manager"
	case strings.Contains(path, "npm") || strings.Contains(path, "node_modules"):
		return "npm"
	case strings.Contains(path, ".local/bin") || strings.Contains(path, ".cargo/bin"):
		return "local"
	case strings.Contains(path, "appdata") || strings.Contains(path, ".local/share"):
		return "user"
	default:
		return "unknown"
	}
}

// DetectMultiple detects multiple software packages
func (d *SoftwareDetector) DetectMultiple(names []string) map[string]*InstalledSoftware {
	results := make(map[string]*InstalledSoftware)
	for _, name := range names {
		results[name] = d.Detect(name)
	}
	return results
}

// QuickCheck quickly checks if a command exists
func QuickCheck(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, "--version")
	return cmd.Run() == nil
}
