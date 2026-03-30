//go:build linux

package system

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// LinuxAppAliases maps common names to Linux package names
var LinuxAppAliases = map[string][]string{
	// Communication
	"wechat":    {"wechat", "weixin", "electronic-wechat"},
	"weixin":    {"wechat", "weixin", "electronic-wechat"},
	"微信":       {"wechat", "electronic-wechat"},
	"qq":        {"qq", "linuxqq", "deepin-qq"},
	"dingtalk":  {"dingtalk", "deepin-dingtalk"},
	"钉钉":       {"dingtalk", "deepin-dingtalk"},
	"feishu":    {"feishu", "bytedance-feishu"},
	"飞书":       {"feishu", "bytedance-feishu"},
	"telegram":  {"telegram-desktop", "telegram"},
	"discord":   {"discord", "discord-canary"},
	"slack":     {"slack", "slack-desktop"},
	"skype":     {"skype", "skypeforlinux"},

	// Browsers
	"chrome":  {"google-chrome", "google-chrome-stable", "chrome"},
	"firefox": {"firefox", "firefox-esr"},
	"edge":    {"microsoft-edge", "microsoft-edge-stable"},
	"chromium": {"chromium", "chromium-browser"},
	"brave":   {"brave-browser", "brave"},

	// Development
	"vscode":   {"code", "visual-studio-code", "code-insiders"},
	"webstorm": {"webstorm", "jetbrains-webstorm"},
	"idea":     {"intellij-idea", "intellij-idea-ce", "jetbrains-idea"},
	"pycharm":  {"pycharm", "jetbrains-pycharm", "pycharm-ce"},
	"goland":   {"goland", "jetbrains-goland"},
	"sublime":  {"sublime-text", "sublime"},
	"cursor":   {"cursor", "cursor-appimage"},

	// Media
	"spotify":  {"spotify", "spotify-client"},
	"vlc":      {"vlc"},
	"obs":      {"obs-studio", "obs"},
	"mpv":      {"mpv"},

	// Productivity
	"notion":   {"notion", "notion-snap"},
	"figma":    {"figma", "figma-linux"},
	"zoom":     {"zoom", "zoom-client"},

	// Utilities
	"docker":       {"docker", "docker-ce", "docker.io"},
	"virtualbox":   {"virtualbox", "virtualbox-qt"},
	"vmware":       {"vmware-workstation", "vmware-player"},

	// Cloud Storage
	"dropbox":      {"dropbox"},
	"onedrive":     {"onedrive", "onedrive-d"},
	"baidunetdisk": {"baidunetdisk", "baidu-netdisk"},
	"百度网盘":        {"baidunetdisk", "baidu-netdisk"},
}

// DetectPlatformApp detects Linux applications via package managers
func (d *SoftwareDetector) DetectPlatformApp(name string) *InstalledSoftware {
	result := &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}

	normalizedName := strings.ToLower(strings.TrimSpace(name))

	// Get aliases
	aliases := LinuxAppAliases[normalizedName]
	if aliases == nil {
		for key, values := range LinuxAppAliases {
			if strings.Contains(key, normalizedName) || strings.Contains(normalizedName, key) {
				aliases = values
				break
			}
		}
	}
	if aliases == nil {
		aliases = []string{name}
	}

	// 1. Try dpkg (Debian/Ubuntu)
	for _, alias := range aliases {
		if version, path := d.detectViaDpkg(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "dpkg"
			return result
		}
	}

	// 2. Try rpm (RHEL/CentOS/Fedora)
	for _, alias := range aliases {
		if version, path := d.detectViaRpm(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "rpm"
			return result
		}
	}

	// 3. Try pacman (Arch Linux)
	for _, alias := range aliases {
		if version, path := d.detectViaPacman(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "pacman"
			return result
		}
	}

	// 4. Try snap
	for _, alias := range aliases {
		if version, path := d.detectViaSnap(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "snap"
			return result
		}
	}

	// 5. Try flatpak
	for _, alias := range aliases {
		if version, path := d.detectViaFlatpak(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "flatpak"
			return result
		}
	}

	// 6. Try .desktop files
	for _, alias := range aliases {
		if path, version := d.detectViaDesktopFile(alias); path != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "desktop"
			return result
		}
	}

	return result
}

// detectViaDpkg checks installed packages via dpkg
func (d *SoftwareDetector) detectViaDpkg(name string) (string, string) {
	// Check if dpkg exists
	if _, err := exec.LookPath("dpkg"); err != nil {
		return "", ""
	}

	// Query package status
	cmd := exec.Command("dpkg-query", "-W", "-f=${Version}\t${Package}\n", name)
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		parts := strings.Split(strings.TrimSpace(string(output)), "\t")
		if len(parts) >= 1 {
			version := parts[0]
			// Get install path
			pathCmd := exec.Command("dpkg", "-L", name)
			pathOutput, _ := pathCmd.Output()
			lines := strings.Split(string(pathOutput), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "/usr/bin/") || strings.HasPrefix(line, "/opt/") {
					return version, line
				}
			}
			return version, ""
		}
	}

	// Try fuzzy search
	cmd = exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\n")
	output, err = cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			pkgName := strings.ToLower(parts[0])
			if strings.Contains(pkgName, nameLower) || strings.Contains(nameLower, pkgName) {
				version := parts[1]
				return version, ""
			}
		}
	}

	return "", ""
}

// detectViaRpm checks installed packages via rpm
func (d *SoftwareDetector) detectViaRpm(name string) (string, string) {
	if _, err := exec.LookPath("rpm"); err != nil {
		return "", ""
	}

	cmd := exec.Command("rpm", "-q", "--queryformat", "%{VERSION}-%{RELEASE}", name)
	output, err := cmd.Output()
	if err == nil && !strings.Contains(string(output), "is not installed") {
		version := strings.TrimSpace(string(output))
		// Get install path
		pathCmd := exec.Command("rpm", "-ql", name)
		pathOutput, _ := pathCmd.Output()
		lines := strings.Split(string(pathOutput), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "/usr/bin/") || strings.HasPrefix(line, "/opt/") {
				return version, line
			}
		}
		return version, ""
	}

	// Try fuzzy search
	cmd = exec.Command("rpm", "-qa", "--queryformat", "%{NAME}\t%{VERSION}\n")
	output, err = cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			pkgName := strings.ToLower(parts[0])
			if strings.Contains(pkgName, nameLower) || strings.Contains(nameLower, pkgName) {
				return parts[1], ""
			}
		}
	}

	return "", ""
}

// detectViaPacman checks installed packages via pacman (Arch Linux)
func (d *SoftwareDetector) detectViaPacman(name string) (string, string) {
	if _, err := exec.LookPath("pacman"); err != nil {
		return "", ""
	}

	cmd := exec.Command("pacman", "-Q", name)
	output, err := cmd.Output()
	if err == nil {
		parts := strings.Fields(string(output))
		if len(parts) >= 2 {
			return parts[1], ""
		}
	}

	// Try fuzzy search
	cmd = exec.Command("pacman", "-Qs", name)
	output, err = cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "local/") {
			pkgInfo := strings.TrimPrefix(line, "local/")
			parts := strings.Fields(pkgInfo)
			if len(parts) >= 2 {
				pkgName := strings.ToLower(parts[0])
				if strings.Contains(pkgName, nameLower) {
					return parts[1], ""
				}
			}
		}
	}

	return "", ""
}

// detectViaSnap checks installed snaps
func (d *SoftwareDetector) detectViaSnap(name string) (string, string) {
	if _, err := exec.LookPath("snap"); err != nil {
		return "", ""
	}

	cmd := exec.Command("snap", "list")
	output, err := cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			pkgName := strings.ToLower(parts[0])
			if pkgName == nameLower || strings.Contains(pkgName, nameLower) {
				version := parts[1]
				path := "/snap/bin/" + parts[0]
				return version, path
			}
		}
	}

	return "", ""
}

// detectViaFlatpak checks installed flatpaks
func (d *SoftwareDetector) detectViaFlatpak(name string) (string, string) {
	if _, err := exec.LookPath("flatpak"); err != nil {
		return "", ""
	}

	cmd := exec.Command("flatpak", "list", "--columns=name,version,app")
	output, err := cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) >= 2 {
			appName := strings.ToLower(parts[0])
			if strings.Contains(appName, nameLower) {
				version := parts[1]
				return version, ""
			}
		}
	}

	return "", ""
}

// detectViaDesktopFile checks for .desktop files
func (d *SoftwareDetector) detectViaDesktopFile(name string) (string, string) {
	nameLower := strings.ToLower(name)

	// Desktop file directories to search
	desktopDirs := []string{
		"/usr/share/applications",
		"/usr/local/share/applications",
		filepath.Join(os.Getenv("HOME"), ".local/share/applications"),
		"/var/lib/snapd/desktop/applications",
	}

	for _, dir := range desktopDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".desktop") {
				continue
			}

			fileName := strings.ToLower(strings.TrimSuffix(entry.Name(), ".desktop"))
			if fileName == nameLower || strings.Contains(fileName, nameLower) {
				filePath := filepath.Join(dir, entry.Name())
				// Read desktop file to get Exec and Version
				version, execPath := d.parseDesktopFile(filePath)
				return execPath, version
			}
		}
	}

	return "", ""
}

// parseDesktopFile extracts info from .desktop file
func (d *SoftwareDetector) parseDesktopFile(filePath string) (string, string) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	var version, execPath string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Exec=") {
			execPath = strings.TrimPrefix(line, "Exec=")
			// Remove arguments
			if idx := strings.Index(execPath, " "); idx > 0 {
				execPath = execPath[:idx]
			}
		}
		if strings.HasPrefix(line, "X-AppImage-Version=") {
			version = strings.TrimPrefix(line, "X-AppImage-Version=")
		}
		if strings.HasPrefix(line, "Version=") && version == "" {
			version = strings.TrimPrefix(line, "Version=")
		}
	}

	return version, execPath
}
