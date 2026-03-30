//go:build darwin

package system

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// macOSAppAliases maps common names to macOS app names
var macOSAppAliases = map[string][]string{
	// Communication
	"wechat":    {"WeChat", "微信"},
	"weixin":    {"WeChat", "微信"},
	"微信":       {"WeChat", "微信"},
	"qq":        {"QQ"},
	"dingtalk":  {"DingTalk", "钉钉"},
	"钉钉":       {"DingTalk", "钉钉"},
	"feishu":    {"Feishu", "Lark", "飞书"},
	"lark":      {"Feishu", "Lark", "飞书"},
	"飞书":       {"Feishu", "Lark", "飞书"},
	"telegram":  {"Telegram"},
	"discord":   {"Discord"},
	"slack":     {"Slack"},
	"skype":     {"Skype"},

	// Browsers
	"chrome":  {"Google Chrome"},
	"firefox": {"Firefox", "Mozilla Firefox"},
	"edge":    {"Microsoft Edge"},
	"safari":  {"Safari"},
	"brave":   {"Brave Browser"},

	// Development
	"vscode":  {"Visual Studio Code", "VSCode"},
	"webstorm": {"WebStorm"},
	"idea":    {"IntelliJ IDEA"},
	"pycharm": {"PyCharm"},
	"goland":  {"GoLand"},
	"xcode":   {"Xcode"},
	"sublime": {"Sublime Text"},
	"cursor":  {"Cursor"},

	// Media
	"spotify": {"Spotify"},
	"vlc":     {"VLC"},
	"obs":     {"OBS", "OBS Studio"},
	"iina":    {"IINA"},

	// Productivity
	"notion": {"Notion"},
	"figma":  {"Figma"},
	"zoom":   {"zoom.us", "Zoom"},
	"teams":  {"Microsoft Teams"},

	// Utilities
	"docker":     {"Docker"},
	"parallels":  {"Parallels Desktop"},
	"vmware":     {"VMware Fusion"},
	"alfred":     {"Alfred"},
	"raycast":    {"Raycast"},
	"cleanmymac": {"CleanMyMac"},

	// Cloud Storage
	"dropbox":      {"Dropbox"},
	"onedrive":     {"OneDrive"},
	"baidunetdisk": {"BaiduNetdisk", "百度网盘"},
	"百度网盘":        {"BaiduNetdisk", "百度网盘"},
}

// brewNameMappings maps common names to Homebrew formula/cask names
var brewNameMappings = map[string]string{
	"visual studio code": "visual-studio-code",
	"vscode":             "visual-studio-code",
	"google chrome":      "google-chrome",
	"wechat":             "wechat",
	"qq":                 "qq",
	"cursor":             "cursor",
	"docker":             "docker",
	"slack":              "slack",
	"discord":            "discord",
	"notion":             "notion",
	"figma":              "figma",
}

// DetectPlatformApp detects macOS applications via Homebrew and /Applications
func (d *SoftwareDetector) DetectPlatformApp(name string) *InstalledSoftware {
	result := &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}

	normalizedName := strings.ToLower(strings.TrimSpace(name))

	// Get aliases
	aliases := macOSAppAliases[normalizedName]
	if aliases == nil {
		for key, values := range macOSAppAliases {
			if strings.Contains(key, normalizedName) || strings.Contains(normalizedName, key) {
				aliases = values
				break
			}
		}
	}
	if aliases == nil {
		aliases = []string{name}
	}

	// 1. Try Homebrew detection
	for _, alias := range aliases {
		if version, path := d.detectViaHomebrew(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "homebrew"
			return result
		}
	}

	// 2. Try /Applications detection
	for _, alias := range aliases {
		if path, version := d.detectMacApp(alias); path != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "app-bundle"
			return result
		}
	}

	// 3. Try pkgutil detection
	for _, alias := range aliases {
		if version, path := d.detectViaPkgutil(alias); version != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "pkg"
			return result
		}
	}

	return result
}

// detectViaHomebrew checks if package is installed via Homebrew
func (d *SoftwareDetector) detectViaHomebrew(name string) (string, string) {
	// Convert app name to homebrew formula name
	brewName := strings.ToLower(strings.ReplaceAll(name, " ", "-"))

	// Check mappings
	if mapped, ok := brewNameMappings[brewName]; ok {
		brewName = mapped
	} else {
		// Check partial mapping
		for key, value := range brewNameMappings {
			if strings.Contains(brewName, key) {
				brewName = value
				break
			}
		}
	}

	// Try brew list --formula
	cmd := exec.Command("brew", "list", "--formula")
	output, err := cmd.Output()
	if err == nil {
		formulae := strings.Split(string(output), "\n")
		for _, formula := range formulae {
			formula = strings.TrimSpace(formula)
			if formula == "" {
				continue
			}
			if formula == brewName || strings.Contains(formula, brewName) {
				// Get version
				versionCmd := exec.Command("brew", "list", "--versions", formula)
				versionOutput, err := versionCmd.Output()
				if err == nil {
					parts := strings.Fields(string(versionOutput))
					if len(parts) >= 2 {
						// Get path
						pathCmd := exec.Command("brew", "--prefix", formula)
						pathOutput, _ := pathCmd.Output()
						return parts[len(parts)-1], strings.TrimSpace(string(pathOutput))
					}
				}
			}
		}
	}

	// Try brew list --cask
	cmd = exec.Command("brew", "list", "--cask")
	output, err = cmd.Output()
	if err == nil {
		casks := strings.Split(string(output), "\n")
		for _, cask := range casks {
			cask = strings.TrimSpace(cask)
			if cask == "" {
				continue
			}
			// Cask names often match app names
			if strings.Contains(strings.ToLower(cask), brewName) || strings.Contains(brewName, strings.ToLower(cask)) {
				// Get version from brew info
				infoCmd := exec.Command("brew", "info", "--cask", cask, "--json=v2")
				infoOutput, err := infoCmd.Output()
				if err == nil {
					// Simple parse for version
					outputStr := string(infoOutput)
					if idx := strings.Index(outputStr, `"version":`); idx >= 0 {
						rest := outputStr[idx+10:]
						if start := strings.Index(rest, `"`); start >= 0 {
							rest = rest[start+1:]
							if end := strings.Index(rest, `"`); end > 0 {
								version := rest[:end]
								path := "/Applications/" + name + ".app"
								return version, path
							}
						}
					}
				}
			}
		}
	}

	return "", ""
}

// detectMacApp checks /Applications directory for installed apps
func (d *SoftwareDetector) detectMacApp(name string) (string, string) {
	appDirs := []string{
		"/Applications",
		filepath.Join(os.Getenv("HOME"), "Applications"),
		"/System/Applications",
	}

	nameLower := strings.ToLower(name)

	for _, appDir := range appDirs {
		if _, err := os.Stat(appDir); os.IsNotExist(err) {
			continue
		}

		// Try direct match first
		for _, suffix := range []string{".app", ""} {
			appPath := filepath.Join(appDir, name+suffix)
			if info, err := os.Stat(appPath); err == nil && info.IsDir() {
				version := d.getMacAppVersion(appPath)
				return appPath, version
			}
		}

		// Scan directory for fuzzy match
		entries, err := os.ReadDir(appDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}

			appName := strings.TrimSuffix(entry.Name(), ".app")
			appNameLower := strings.ToLower(appName)

			if appNameLower == nameLower ||
				strings.Contains(appNameLower, nameLower) ||
				strings.Contains(nameLower, appNameLower) {
				appPath := filepath.Join(appDir, entry.Name())
				version := d.getMacAppVersion(appPath)
				return appPath, version
			}
		}
	}

	return "", ""
}

// getMacAppVersion extracts version from Info.plist
func (d *SoftwareDetector) getMacAppVersion(appPath string) string {
	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if _, err := os.Stat(plistPath); err != nil {
		return ""
	}

	// Use defaults command to read plist
	cmd := exec.Command("defaults", "read", plistPath, "CFBundleShortVersionString")
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}

	// Try CFBundleVersion
	cmd = exec.Command("defaults", "read", plistPath, "CFBundleVersion")
	output, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}

	return ""
}

// detectViaPkgutil checks installed packages via pkgutil
func (d *SoftwareDetector) detectViaPkgutil(name string) (string, string) {
	cmd := exec.Command("pkgutil", "--pkgs")
	output, err := cmd.Output()
	if err != nil {
		return "", ""
	}

	nameLower := strings.ToLower(name)
	packages := strings.Split(string(output), "\n")

	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}

		if strings.Contains(strings.ToLower(pkg), nameLower) {
			// Get package info
			infoCmd := exec.Command("pkgutil", "--pkg-info", pkg)
			infoOutput, err := infoCmd.Output()
			if err == nil {
				// Parse version from output
				lines := strings.Split(string(infoOutput), "\n")
				for _, line := range lines {
					if strings.HasPrefix(line, "version:") {
						version := strings.TrimSpace(strings.TrimPrefix(line, "version:"))
						return version, ""
					}
				}
			}
		}
	}

	return "", ""
}
