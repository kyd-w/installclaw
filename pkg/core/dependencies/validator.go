package dependencies

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Validator validates dependencies against the current system environment
type Validator struct {
	loader    *Loader
	sysInfo   *SystemInfo
	aiBuilder AIDependencyBuilder // For generating dependencies via AI
}

// AIDependencyBuilder interface for AI-based dependency generation
type AIDependencyBuilder interface {
	BuildDependencyTree(ctx context.Context, packageName string) (*DependencyNode, error)
}

// NewValidator creates a new dependency validator
func NewValidator(loader *Loader) *Validator {
	return &Validator{
		loader:  loader,
		sysInfo: CollectSystemInfo(),
	}
}

// SetAIBuilder sets the AI dependency builder
func (v *Validator) SetAIBuilder(builder AIDependencyBuilder) {
	v.aiBuilder = builder
}

// Validate validates a package and all its dependencies
func (v *Validator) Validate(ctx context.Context, packageName string, version string) (*ValidationResult, error) {
	// 1. Build dependency tree
	root, err := v.buildTree(ctx, packageName, version)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency tree: %w", err)
	}

	// 2. Validate the tree recursively
	v.validateNode(ctx, root, version)

	// 3. Collect results
	result := &ValidationResult{
		SystemInfo: v.sysInfo,
	}

	// Walk the tree to collect blockers, warnings, and installables
	v.collectResults(root, &result.Blockers, &result.Warnings, &result.ToInstall)

	// 4. Determine if installation can proceed
	result.CanProceed = len(result.Blockers) == 0

	// 5. Generate installation order (topological sort)
	if result.CanProceed && len(result.ToInstall) > 0 {
		result.InstallOrder = v.topologicalSort(result.ToInstall)
	}

	return result, nil
}

// buildTree builds the dependency tree for a package
func (v *Validator) buildTree(ctx context.Context, packageName string, version string) (*DependencyNode, error) {
	// 1. Try to load from knowledge sources
	node := v.loader.Load(packageName)
	if node == nil {
		// 2. If not found and AI builder is available, generate via AI
		if v.aiBuilder != nil {
			aiNode, err := v.aiBuilder.BuildDependencyTree(ctx, packageName)
			if err != nil {
				return nil, fmt.Errorf("AI failed to generate dependency tree: %w", err)
			}
			node = aiNode
		} else {
			// 3. Create a minimal node for unknown packages
			node = &DependencyNode{
				ID:   packageName,
				Name: packageName,
			}
		}
	}

	// 3. Recursively build child dependencies
	for i, dep := range node.Dependencies {
		childNode, err := v.buildTree(ctx, dep.ID, dep.VersionRequired)
		if err != nil {
			return nil, fmt.Errorf("failed to build dependency %s: %w", dep.ID, err)
		}
		node.Dependencies[i] = childNode
	}

	return node, nil
}

// validateNode validates a single node and all its children recursively
func (v *Validator) validateNode(ctx context.Context, node *DependencyNode, targetVersion string) *DepValidationResult {
	// 1. First validate all child dependencies
	for _, dep := range node.Dependencies {
		childResult := v.validateNode(ctx, dep, dep.VersionRequired)
		// If any child is blocked, the parent is also blocked
		if childResult.Status == DepBlocked {
			node.ValidationResult = &DepValidationResult{
				Status:      DepBlocked,
				BlockReason: fmt.Sprintf("依赖 %s 被阻塞: %s", dep.ID, childResult.BlockReason),
			}
			return node.ValidationResult
		}
	}

	// 2. Check environment requirements for this node
	result := v.checkEnvRequirements(node, targetVersion)

	// 3. If not satisfied, check if installable
	if result.Status != DepSatisfied {
		result = v.checkInstallable(node, targetVersion, result)
	}

	node.ValidationResult = result
	return result
}

// checkEnvRequirements checks if the environment meets the requirements
func (v *Validator) checkEnvRequirements(node *DependencyNode, targetVersion string) *DepValidationResult {
	req := v.getApplicableRequirements(node, targetVersion)

	// Check OS compatibility
	if len(req.OS) > 0 {
		osCompatible := false
		for _, os := range req.OS {
			if os == v.sysInfo.OS {
				osCompatible = true
				break
			}
		}
		if !osCompatible {
			return &DepValidationResult{
				Status:      DepBlocked,
				BlockReason: fmt.Sprintf("不支持的操作系统: 需要 %v, 当前 %s", req.OS, v.sysInfo.OS),
			}
		}
	}

	// Check architecture compatibility
	if len(req.Arch) > 0 {
		archCompatible := false
		for _, arch := range req.Arch {
			if arch == v.sysInfo.Arch {
				archCompatible = true
				break
			}
		}
		if !archCompatible {
			return &DepValidationResult{
				Status:      DepBlocked,
				BlockReason: fmt.Sprintf("不支持的架构: 需要 %v, 当前 %s", req.Arch, v.sysInfo.Arch),
			}
		}
	}

	// Check minimum library versions (system-level dependencies)
	for lib, minVer := range req.MinVersions {
		currentVer := v.sysInfo.LibraryVersions[lib]
		if currentVer == "" {
			return &DepValidationResult{
				Status:      DepUnknown,
				BlockReason: fmt.Sprintf("无法检测 %s 版本", lib),
			}
		}
		if compareVersions(currentVer, minVer) < 0 {
			// System library version insufficient - this is a blocker
			return &DepValidationResult{
				Status:         DepBlocked,
				BlockReason:    fmt.Sprintf("%s 版本不足: 需要 >= %s, 当前 %s", lib, minVer, currentVer),
				CurrentVersion: currentVer,
				TargetVersion:  minVer,
			}
		}
	}

	// Check if already installed
	installed := v.checkInstalled(node.ID)
	if installed != "" {
		return &DepValidationResult{
			Status:         DepSatisfied,
			CurrentVersion: installed,
		}
	}

	// Environment is compatible but not installed
	return &DepValidationResult{
		Status: DepInstallable,
	}
}

// getApplicableRequirements gets the requirements applicable for the target version
func (v *Validator) getApplicableRequirements(node *DependencyNode, targetVersion string) EnvRequirements {
	req := node.Requirements

	// Check version-specific requirements
	if targetVersion != "" && node.VersionRequirements != nil {
		for verPattern, verReq := range node.VersionRequirements {
			if versionMatchesPattern(targetVersion, verPattern) {
				// Merge requirements
				req = mergeRequirements(req, verReq)
				break
			}
		}
	}

	// Check platform-specific requirements
	if platformConfig, ok := node.Platforms[v.sysInfo.OS]; ok {
		if platformConfig.Requirements != nil {
			req = mergeRequirements(req, *platformConfig.Requirements)
		}

		// Check distro-specific version limits
		if v.sysInfo.OS == "linux" && v.sysInfo.Distro != "" {
			if distroConfig, ok := platformConfig.Distros[v.sysInfo.Distro]; ok {
				for _, limit := range distroConfig.VersionLimits {
					if osVersionMatches(v.sysInfo.OSVersion, limit.OSVersion) {
						// Check if target version exceeds the limit
						if targetVersion != "" && limit.MaxSoftwareVersion != "" {
							if compareVersions(targetVersion, limit.MaxSoftwareVersion) > 0 {
								// Add warning about version limit
								// This will be handled in validation
							}
						}
					}
				}
			}
		}
	}

	return req
}

// checkInstallable determines if a dependency can be automatically installed
func (v *Validator) checkInstallable(node *DependencyNode, targetVersion string, currentResult *DepValidationResult) *DepValidationResult {
	// Check if this is a system component that cannot be auto-installed
	if v.isSystemComponent(node.ID) {
		return &DepValidationResult{
			Status:      DepBlocked,
			BlockReason: fmt.Sprintf("%s 是系统组件，无法自动安装或升级", node.ID),
		}
	}

	// Find an appropriate install method
	method := v.findInstallMethod(node, targetVersion)
	if method != nil {
		return &DepValidationResult{
			Status:         DepInstallable,
			InstallMethod:  method,
			TargetVersion:  targetVersion,
			CurrentVersion: currentResult.CurrentVersion,
		}
	}

	// No install method found, mark as unknown
	return &DepValidationResult{
		Status:         DepUnknown,
		TargetVersion:  targetVersion,
		CurrentVersion: currentResult.CurrentVersion,
	}
}

// isSystemComponent checks if a dependency is a system-level component
func (v *Validator) isSystemComponent(name string) bool {
	systemComponents := []string{
		"glibc", "libc", "kernel", "systemd", "openssl",
		"libssl", "zlib", "gcc", "g++",
	}
	nameLower := strings.ToLower(name)
	for _, sys := range systemComponents {
		if strings.Contains(nameLower, sys) {
			return true
		}
	}
	return false
}

// findInstallMethod finds the best install method for the current platform
func (v *Validator) findInstallMethod(node *DependencyNode, targetVersion string) *InstallMethod {
	platformConfig, hasPlatform := node.Platforms[v.sysInfo.OS]

	// Collect all available methods
	var methods []InstallMethod

	if hasPlatform {
		// Add platform-specific methods
		methods = append(methods, platformConfig.InstallMethods...)

		// For Linux, check distro-specific methods
		if v.sysInfo.OS == "linux" && v.sysInfo.Distro != "" {
			if distroConfig, ok := platformConfig.Distros[v.sysInfo.Distro]; ok {
				// Prepend distro-specific methods (higher priority)
				methods = append(distroConfig.InstallMethods, methods...)
			}
		}
	}

	// Find the best (highest priority/recommended) method
	if len(methods) == 0 {
		return nil
	}

	best := methods[0]
	for _, m := range methods {
		if m.Recommended {
			return &m
		}
		if m.Priority < best.Priority {
			best = m
		}
	}

	return &best
}

// checkInstalled checks if a package is already installed
func (v *Validator) checkInstalled(packageID string) string {
	// Common version check commands
	commands := []string{
		packageID + " --version",
		packageID + " -v",
		packageID + " version",
	}

	for _, cmd := range commands {
		output, err := exec.Command("sh", "-c", cmd).Output()
		if err == nil && len(output) > 0 {
			// Extract version from output
			return extractVersion(string(output))
		}
	}

	// Also check via which/where
	_, err := exec.LookPath(packageID)
	if err == nil {
		return "installed"
	}

	return ""
}

// collectResults walks the tree and collects blockers, warnings, and installables
func (v *Validator) collectResults(node *DependencyNode, blockers *[]Blocker, warnings *[]string, toInstall *[]*DependencyNode) {
	if node.ValidationResult == nil {
		return
	}

	switch node.ValidationResult.Status {
	case DepBlocked:
		*blockers = append(*blockers, Blocker{
			Package:     node.ID,
			Type:        "dependency",
			Description: node.ValidationResult.BlockReason,
			Required:    node.ValidationResult.TargetVersion,
			Current:     node.ValidationResult.CurrentVersion,
		})

	case DepInstallable:
		*toInstall = append(*toInstall, node)

	case DepUnknown:
		*warnings = append(*warnings, fmt.Sprintf("无法确定 %s 的安装状态", node.ID))

	case DepConflict:
		*blockers = append(*blockers, Blocker{
			Package:     node.ID,
			Type:        "conflict",
			Description: node.ValidationResult.BlockReason,
		})
	}

	// Recurse into children
	for _, dep := range node.Dependencies {
		v.collectResults(dep, blockers, warnings, toInstall)
	}
}

// topologicalSort returns dependencies in installation order
func (v *Validator) topologicalSort(nodes []*DependencyNode) []string {
	// Build dependency graph
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	nodeMap := make(map[string]*DependencyNode)

	for _, node := range nodes {
		nodeMap[node.ID] = node
		if _, exists := inDegree[node.ID]; !exists {
			inDegree[node.ID] = 0
		}

		for _, dep := range node.Dependencies {
			graph[dep.ID] = append(graph[dep.ID], node.ID)
			inDegree[node.ID]++
		}
	}

	// Kahn's algorithm for topological sort
	queue := []string{}
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	result := []string{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		for _, neighbor := range graph[current] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	return result
}

// GetSystemInfo returns the collected system information
func (v *Validator) GetSystemInfo() *SystemInfo {
	return v.sysInfo
}

// CollectSystemInfo collects current system information
func CollectSystemInfo() *SystemInfo {
	info := &SystemInfo{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		LibraryVersions: make(map[string]string),
	}

	// Detect Linux distribution
	if info.OS == "linux" {
		info.Distro = detectLinuxDistro()
		info.PackageManager = detectPackageManager(info.Distro)
		info.OSVersion = detectOSVersion()
		info.LibraryVersions = detectLibraryVersions()
	}

	// Detect Windows version
	if info.OS == "windows" {
		info.OSVersion = detectWindowsVersion()
	}

	// Detect macOS version
	if info.OS == "darwin" {
		info.OSVersion = detectMacOSVersion()
		info.PackageManager = "brew"
	}

	return info
}

// detectLinuxDistro detects the Linux distribution
func detectLinuxDistro() string {
	// Read /etc/os-release
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}

	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), "\"'")
			return id
		}
	}

	return "unknown"
}

// detectPackageManager detects the package manager for a Linux distro
func detectPackageManager(distro string) string {
	switch distro {
	case "ubuntu", "debian", "linuxmint", "pop":
		return "apt"
	case "centos", "rhel", "rocky", "almalinux", "amazon":
		return "yum"
	case "fedora":
		return "dnf"
	case "arch", "manjaro":
		return "pacman"
	case "opensuse", "suse":
		return "zypper"
	case "alpine":
		return "apk"
	default:
		return "unknown"
	}
}

// detectOSVersion detects the OS version
func detectOSVersion() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}

	content := string(data)
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "VERSION_ID=") {
			version := strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"'")
			return version
		}
	}

	return ""
}

// detectLibraryVersions detects system library versions
func detectLibraryVersions() map[string]string {
	versions := make(map[string]string)

	// Detect glibc version
	if output, err := exec.Command("ldd", "--version").Output(); err == nil {
		// Output format: "ldd (Ubuntu GLIBC 2.31-0ubuntu9) 2.31"
		parts := strings.Fields(string(output))
		for i, part := range parts {
			if part == "GLIBC" && i+1 < len(parts) {
				versions["glibc"] = strings.TrimSuffix(strings.TrimSuffix(parts[i+1], ")"), "-")
			}
		}
		// Alternative: last field is usually the version
		if versions["glibc"] == "" && len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			if strings.Contains(lastPart, ".") {
				versions["glibc"] = lastPart
			}
		}
	}

	// Detect OpenSSL version
	if output, err := exec.Command("openssl", "version").Output(); err == nil {
		// Output format: "OpenSSL 1.1.1f  31 Mar 2020"
		parts := strings.Fields(string(output))
		if len(parts) >= 2 {
			versions["openssl"] = parts[1]
		}
	}

	return versions
}

// detectWindowsVersion detects Windows version
func detectWindowsVersion() string {
	// Use ver command
	output, err := exec.Command("cmd", "/c", "ver").Output()
	if err != nil {
		return ""
	}
	// Parse output like "Microsoft Windows [Version 10.0.19045.3803]"
	return string(output)
}

// detectMacOSVersion detects macOS version
func detectMacOSVersion() string {
	output, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// Helper functions

// compareVersions compares two version strings
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	maxLen := len(v1Parts)
	if len(v2Parts) > maxLen {
		maxLen = len(v2Parts)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(v1Parts) {
			fmt.Sscanf(v1Parts[i], "%d", &n1)
		}
		if i < len(v2Parts) {
			fmt.Sscanf(v2Parts[i], "%d", &n2)
		}

		if n1 < n2 {
			return -1
		}
		if n1 > n2 {
			return 1
		}
	}

	return 0
}

// versionMatchesPattern checks if a version matches a pattern like "18+" or "16.x"
func versionMatchesPattern(version, pattern string) bool {
	if strings.HasSuffix(pattern, "+") {
		// Version must be >= the prefix
		minVer := strings.TrimSuffix(pattern, "+")
		return compareVersions(version, minVer) >= 0
	}
	if strings.HasSuffix(pattern, ".x") {
		// Major version must match
		prefix := strings.TrimSuffix(pattern, ".x")
		return strings.HasPrefix(version, prefix+".")
	}
	return version == pattern
}

// osVersionMatches checks if OS version matches a constraint
func osVersionMatches(currentVersion, constraint string) bool {
	if strings.HasPrefix(constraint, "<=") {
		ver := strings.TrimPrefix(constraint, "<=")
		return compareVersions(currentVersion, ver) <= 0
	}
	if strings.HasPrefix(constraint, ">=") {
		ver := strings.TrimPrefix(constraint, ">=")
		return compareVersions(currentVersion, ver) >= 0
	}
	if strings.HasPrefix(constraint, "<") {
		ver := strings.TrimPrefix(constraint, "<")
		return compareVersions(currentVersion, ver) < 0
	}
	if strings.HasPrefix(constraint, ">") {
		ver := strings.TrimPrefix(constraint, ">")
		return compareVersions(currentVersion, ver) > 0
	}
	return currentVersion == constraint
}

// mergeRequirements merges two EnvRequirements, with latter taking precedence
func mergeRequirements(base, override EnvRequirements) EnvRequirements {
	result := base

	if len(override.OS) > 0 {
		result.OS = override.OS
	}
	if len(override.Arch) > 0 {
		result.Arch = override.Arch
	}
	if result.MinVersions == nil {
		result.MinVersions = make(map[string]string)
	}
	for k, v := range override.MinVersions {
		result.MinVersions[k] = v
	}
	if result.MaxVersions == nil {
		result.MaxVersions = make(map[string]string)
	}
	for k, v := range override.MaxVersions {
		result.MaxVersions[k] = v
	}

	return result
}

// extractVersion extracts a version number from command output
func extractVersion(output string) string {
	// Common patterns: "v1.2.3", "1.2.3", "version 1.2.3"
	fields := strings.Fields(output)
	for _, field := range fields {
		// Check if field looks like a version
		if strings.Contains(field, ".") && len(field) >= 3 {
			// Remove common prefixes
			ver := strings.TrimPrefix(field, "v")
			ver = strings.TrimPrefix(ver, "V")
			if isVersionNumber(ver) {
				return ver
			}
		}
	}
	return output
}

// isVersionNumber checks if a string looks like a version number
func isVersionNumber(s string) bool {
	hasDigit := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c != '.' && c != '-' && c != '_' {
			return false
		}
	}
	return hasDigit
}
