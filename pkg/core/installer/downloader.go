package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kyd-w/installclaw/pkg/core/metadata"
)

// Downloader handles secure file downloads
type Downloader struct {
	httpClient *http.Client
	cacheDir   string
}

// NewDownloader creates a new downloader
func NewDownloader() *Downloader {
	cacheDir := os.Getenv("INSTALLER_CACHE_DIR")
	if cacheDir == "" {
		homeDir, _ := os.UserHomeDir()
		cacheDir = filepath.Join(homeDir, ".installer", "cache")
	}

	return &Downloader{
		httpClient: &http.Client{
			Timeout: 30 * time.Minute,
		},
		cacheDir: cacheDir,
	}
}

// Download downloads a file from an asset
func (d *Downloader) Download(ctx context.Context, asset *metadata.Asset) (string, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(d.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Generate cache filename
	cacheFile := filepath.Join(d.cacheDir, asset.Name)

	// Check if already cached and valid
	if info, err := os.Stat(cacheFile); err == nil {
		if info.Size() == asset.Size {
			// Verify checksum if available
			if asset.Checksum != "" {
				if d.verifyChecksum(cacheFile, asset.Checksum) {
					return cacheFile, nil // Use cached file
				}
			} else {
				return cacheFile, nil // Use cached file (no checksum to verify)
			}
		}
	}

	// Create temporary file for download
	tempFile := cacheFile + ".tmp"
	out, err := os.Create(tempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer out.Close()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Universal-Installer/1.0")
	req.Header.Set("Accept", "application/octet-stream")

	// Execute download
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Download with progress tracking
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("download write failed: %w", err)
	}

	// Verify checksum if available
	if asset.Checksum != "" {
		if !d.verifyChecksum(tempFile, asset.Checksum) {
			os.Remove(tempFile)
			return "", fmt.Errorf("checksum verification failed")
		}
	}

	// Move to final location
	if err := os.Rename(tempFile, cacheFile); err != nil {
		os.Remove(tempFile)
		return "", fmt.Errorf("failed to move downloaded file: %w", err)
	}

	return cacheFile, nil
}

// verifyChecksum verifies the SHA256 checksum of a file
func (d *Downloader) verifyChecksum(filePath, expectedChecksum string) bool {
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	return strings.EqualFold(actualChecksum, expectedChecksum)
}

// ClearCache clears the download cache
func (d *Downloader) ClearCache() error {
	return os.RemoveAll(d.cacheDir)
}

// GetCacheSize returns the size of the cache in bytes
func (d *Downloader) GetCacheSize() (int64, error) {
	var size int64
	err := filepath.Walk(d.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}
