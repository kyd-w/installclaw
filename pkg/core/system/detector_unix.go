//go:build !windows && !darwin && !linux

package system

// DetectPlatformApp is a stub for other Unix platforms (FreeBSD, etc.)
func (d *SoftwareDetector) DetectPlatformApp(name string) *InstalledSoftware {
	return &InstalledSoftware{
		Name:        name,
		IsInstalled: false,
	}
}
