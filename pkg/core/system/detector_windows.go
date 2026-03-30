//go:build windows

package system

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// WindowsAppInfo contains Windows-specific app detection info
type WindowsAppInfo struct {
	Name         string
	Version      string
	Publisher    string
	InstallPath  string
	UninstallCmd string
}

// WindowsAppAliases maps common Chinese/English names to Windows registry names
var WindowsAppAliases = map[string][]string{
	// Communication
	"wechat":    {"微信", "WeChat", "Tencent WeChat"},
	"weixin":    {"微信", "WeChat", "Tencent WeChat"},
	"微信":       {"微信", "WeChat", "Tencent WeChat"},
	"qq":        {"QQ", "Tencent QQ"},
	"tim":       {"TIM", "Tencent TIM"},
	"dingtalk":  {"钉钉", "DingTalk", "阿里钉钉"},
	"dingding":  {"钉钉", "DingTalk", "阿里钉钉"},
	"钉钉":       {"钉钉", "DingTalk", "阿里钉钉"},
	"feishu":    {"飞书", "Feishu", "Lark"},
	"lark":      {"飞书", "Feishu", "Lark"},
	"飞书":       {"飞书", "Feishu", "Lark"},
	"telegram":  {"Telegram", "Telegram Desktop"},
	"discord":   {"Discord"},
	"slack":     {"Slack"},
	"skype":     {"Skype"},

	// Browsers
	"chrome":      {"Google Chrome", "Chrome"},
	"googlechrome": {"Google Chrome", "Chrome"},
	"firefox":     {"Mozilla Firefox", "Firefox"},
	"edge":        {"Microsoft Edge", "Edge"},
	"browser":     {"Google Chrome", "Mozilla Firefox", "Microsoft Edge"},

	// Development
	"vscode":       {"Visual Studio Code", "VSCode", "VS Code"},
	"visualstudiocode": {"Visual Studio Code", "VSCode"},
	"webstorm":     {"WebStorm"},
	"idea":         {"IntelliJ IDEA"},
	"pycharm":      {"PyCharm"},
	"goland":       {"GoLand"},
	"sublime":      {"Sublime Text"},
	"notepad++":    {"Notepad++", "Notepad Plus Plus"},
	"git":          {"Git"},
	"github":       {"GitHub Desktop"},
	"sourcetree":   {"Sourcetree"},

	// Media
	"spotify":      {"Spotify"},
	"netflix":      {"Netflix"},
	"vlc":          {"VLC", "VLC media player"},
	"potplayer":    {"PotPlayer", "Daum PotPlayer"},
	"obs":          {"OBS Studio", "Open Broadcaster Software"},

	// Games
	"steam":        {"Steam"},
	"epic":         {"Epic Games Launcher"},
	"battlenet":    {"Battle.net"},

	// Productivity
	"office":       {"Microsoft Office", "Office 365"},
	"word":         {"Microsoft Word"},
	"excel":        {"Microsoft Excel"},
	"powerpoint":   {"Microsoft PowerPoint"},
	"teams":        {"Microsoft Teams"},
	"zoom":         {"Zoom"},
	"notion":       {"Notion"},
	"figma":        {"Figma"},

	// Utilities
	"7zip":         {"7-Zip", "7zip"},
	"winrar":       {"WinRAR"},
	"everything":   {"Everything", "voidtools Everything"},
	"powertoys":    {"PowerToys", "Microsoft PowerToys"},
	"terminal":     {"Windows Terminal"},

	// Cloud Storage
	"onedrive":     {"OneDrive", "Microsoft OneDrive"},
	"dropbox":      {"Dropbox"},
	"baidunetdisk": {"百度网盘", "Baidu Netdisk"},
	"baidu":        {"百度网盘", "Baidu Netdisk"},
	"百度网盘":        {"百度网盘", "Baidu Netdisk"},
}

// Common Windows install paths to check
var WindowsAppPaths = []string{
	// Program Files
	"C:\\Program Files",
	"C:\\Program Files (x86)",
	// User local
	os.Getenv("LOCALAPPDATA"),
	os.Getenv("APPDATA"),
	// Common locations
	"C:\\ProgramData",
}

// DetectPlatformApp detects Windows applications via registry and file system
func (d *SoftwareDetector) DetectPlatformApp(name string) *InstalledSoftware {
	result := &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}

	// Normalize name
	normalizedName := strings.ToLower(strings.TrimSpace(name))

	// Get aliases for this app
	aliases := WindowsAppAliases[normalizedName]
	if aliases == nil {
		// Try to find matching alias
		for key, values := range WindowsAppAliases {
			if strings.Contains(key, normalizedName) || strings.Contains(normalizedName, key) {
				aliases = values
				break
			}
		}
	}
	if aliases == nil {
		aliases = []string{name}
	}

	// 1. Try registry detection
	for _, alias := range aliases {
		if info := d.detectViaRegistry(alias); info != nil {
			result.IsInstalled = true
			result.Version = info.Version
			result.Path = info.InstallPath
			result.InstallMethod = "windows-installer"
			return result
		}
	}

	// 2. Try file system detection
	for _, alias := range aliases {
		if path, version := d.detectViaFileSystem(alias); path != "" {
			result.IsInstalled = true
			result.Version = version
			result.Path = path
			result.InstallMethod = "windows-binary"
			return result
		}
	}

	return result
}

// detectViaRegistry searches Windows registry for installed applications
func (d *SoftwareDetector) detectViaRegistry(name string) *WindowsAppInfo {
	nameLower := strings.ToLower(name)

	// Registry paths to search
	regPaths := []struct {
		key  registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}

	for _, rp := range regPaths {
		key, err := registry.OpenKey(rp.key, rp.path, registry.READ)
		if err != nil {
			continue
		}

		// Enumerate subkeys
		subkeys, err := key.ReadSubKeyNames(-1)
		key.Close()
		if err != nil {
			continue
		}

		for _, subkeyName := range subkeys {
			subkey, err := registry.OpenKey(rp.key, rp.path+`\`+subkeyName, registry.READ)
			if err != nil {
				continue
			}

			info := d.readAppInfo(subkey)
			subkey.Close()

			if info != nil && info.Name != "" {
				// Check if name matches
				if strings.Contains(strings.ToLower(info.Name), nameLower) ||
					strings.Contains(nameLower, strings.ToLower(info.Name)) {
					return info
				}
			}
		}
	}

	return nil
}

// readAppInfo reads application info from a registry key
func (d *SoftwareDetector) readAppInfo(key registry.Key) *WindowsAppInfo {
	displayName, _, _ := key.GetStringValue("DisplayName")
	if displayName == "" {
		return nil
	}

	info := &WindowsAppInfo{
		Name: displayName,
	}

	// Read version
	displayVersion, _, _ := key.GetStringValue("DisplayVersion")
	info.Version = displayVersion

	// Read publisher
	publisher, _, _ := key.GetStringValue("Publisher")
	info.Publisher = publisher

	// Read install path
	installLocation, _, _ := key.GetStringValue("InstallLocation")
	if installLocation == "" {
		// Try alternative keys
		installLocation, _, _ = key.GetStringValue("InstallDir")
	}
	if installLocation == "" {
		// Try to extract from uninstall command
		uninstallCmd, _, _ := key.GetStringValue("UninstallString")
		if uninstallCmd != "" {
			info.UninstallCmd = uninstallCmd
			// Extract path from command like "C:\Program Files\App\uninstall.exe"
			if strings.Contains(uninstallCmd, ":\\") {
				parts := strings.SplitN(uninstallCmd, ":\\", 2)
				if len(parts) == 2 {
					pathPart := parts[1]
					if idx := strings.Index(pathPart, "\\"); idx > 0 {
						installLocation = parts[0] + ":\\" + pathPart[:idx]
					}
				}
			}
		}
	}
	info.InstallPath = installLocation

	return info
}

// detectViaFileSystem searches common installation paths for the application
func (d *SoftwareDetector) detectViaFileSystem(name string) (string, string) {
	nameLower := strings.ToLower(name)

	// Common app folder name patterns
	patterns := []string{
		name,
		strings.ReplaceAll(name, " ", ""),
		strings.ReplaceAll(name, " ", "-"),
	}

	for _, baseDir := range WindowsAppPaths {
		if baseDir == "" {
			continue
		}

		for _, pattern := range patterns {
			// Check direct match
			appPath := filepath.Join(baseDir, pattern)
			if d.isValidAppDir(appPath) {
				version := d.detectAppVersion(appPath)
				return appPath, version
			}

			// Check Programs subdirectory
			appPath = filepath.Join(baseDir, "Programs", pattern)
			if d.isValidAppDir(appPath) {
				version := d.detectAppVersion(appPath)
				return appPath, version
			}

			// Check Microsoft subdirectory (for local appdata)
			appPath = filepath.Join(baseDir, "Microsoft", pattern)
			if d.isValidAppDir(appPath) {
				version := d.detectAppVersion(appPath)
				return appPath, version
			}
		}

		// Try fuzzy matching with Glob
		for _, pattern := range patterns {
			matches, _ := filepath.Glob(filepath.Join(baseDir, "*"+pattern+"*"))
			if len(matches) > 0 {
				for _, match := range matches {
					if d.isValidAppDir(match) {
						version := d.detectAppVersion(match)
						return match, version
					}
				}
			}
		}
	}

	// Special handling for Chinese apps
	if containsChinese(name) {
		return d.detectChineseApp(nameLower)
	}

	return "", ""
}

// isValidAppDir checks if a directory contains a valid application (has executable files)
func (d *SoftwareDetector) isValidAppDir(appPath string) bool {
	info, err := os.Stat(appPath)
	if err != nil || !info.IsDir() {
		return false
	}

	// Check for executable files to verify this is a valid app installation
	// An app directory without exe files is likely leftover from incomplete uninstall
	exes, err := filepath.Glob(filepath.Join(appPath, "*.exe"))
	if err != nil || len(exes) == 0 {
		// Also check subdirectories one level deep
		subdirs, _ := filepath.Glob(filepath.Join(appPath, "*"))
		for _, subdir := range subdirs {
			subInfo, err := os.Stat(subdir)
			if err == nil && subInfo.IsDir() {
				exes, err = filepath.Glob(filepath.Join(subdir, "*.exe"))
				if err == nil && len(exes) > 0 {
					return true
				}
			}
		}
		return false
	}

	return true
}

// detectChineseApp handles detection for apps with Chinese names
func (d *SoftwareDetector) detectChineseApp(name string) (string, string) {
	// Chinese apps often install in specific locations
	chineseAppPaths := map[string][]string{
		"微信": {
			"C:\\Program Files\\Tencent\\WeChat",
			"C:\\Program Files (x86)\\Tencent\\WeChat",
			filepath.Join(os.Getenv("LOCALAPPDATA"), "WeChat"),
		},
		"百度网盘": {
			"C:\\Program Files\\baidu\\BaiduNetdisk",
			"C:\\Program Files (x86)\\baidu\\BaiduNetdisk",
		},
		"钉钉": {
			"C:\\Program Files\\DingDing\\Dingtalk",
			"C:\\Program Files (x86)\\DingDing\\Dingtalk",
			filepath.Join(os.Getenv("LOCALAPPDATA"), "DingTalk"),
		},
		"飞书": {
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Feishu"),
			filepath.Join(os.Getenv("APPDATA"), "Lark"),
		},
	}

	for chineseName, paths := range chineseAppPaths {
		if strings.Contains(name, chineseName) {
			for _, path := range paths {
				if d.isValidAppDir(path) {
					version := d.detectAppVersion(path)
					return path, version
				}
			}
		}
	}

	return "", ""
}

// detectAppVersion tries to find version info from app directory
func (d *SoftwareDetector) detectAppVersion(appPath string) string {
	// Look for version files
	versionFiles := []string{
		"version",
		"VERSION",
		"version.txt",
		"version.json",
		"package.json", // For Node-based apps
	}

	for _, file := range versionFiles {
		path := filepath.Join(appPath, file)
		if content, err := os.ReadFile(path); err == nil {
			contentStr := string(content)
			// Try to extract version number
			if version := extractVersion(contentStr); version != "" {
				return version
			}
		}
	}

	// Check executable for version info (look for .exe files)
	exes, _ := filepath.Glob(filepath.Join(appPath, "*.exe"))
	if len(exes) > 0 {
		// Try to get file version info
		// This is a simplified check - full implementation would use Windows API
		if info, err := os.Stat(exes[0]); err == nil {
			return info.ModTime().Format("2006.01.02")
		}
	}

	return ""
}

// extractVersion extracts version number from string
func extractVersion(s string) string {
	// Simple regex-like extraction for version patterns
	// Look for patterns like "version": "1.2.3" or "1.2.3"
	s = strings.TrimSpace(s)

	// Try JSON-style version
	if idx := strings.Index(s, `"version"`); idx >= 0 {
		rest := s[idx+9:]
		if start := strings.Index(rest, `"`); start >= 0 {
			rest = rest[start+1:]
			if end := strings.Index(rest, `"`); end > 0 {
				return rest[:end]
			}
		}
	}

	return ""
}

// containsChinese checks if string contains Chinese characters
func containsChinese(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
