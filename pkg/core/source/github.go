package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kyd-w/installclaw/pkg/core/metadata"
)

// GitHubVerifier verifies GitHub sources by checking star count
type GitHubVerifier struct {
	minStars    int
	httpClient  *http.Client
	rateLimited bool
}

// NewGitHubVerifier creates a new GitHub verifier
func NewGitHubVerifier(minStars int) *GitHubVerifier {
	if minStars <= 0 {
		minStars = 200 // Default minimum
	}

	return &GitHubVerifier{
		minStars: minStars,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the verifier name
func (v *GitHubVerifier) Name() string {
	return "github"
}

// Verify checks if all GitHub sources meet the minimum star requirement
func (v *GitHubVerifier) Verify(ctx context.Context, pkg *metadata.PackageMetadata) error {
	for _, source := range pkg.Sources {
		if source.Type != metadata.SourceGitHub {
			continue
		}

		// If stars are already in metadata, use them
		if source.Stars > 0 {
			if source.Stars < v.minStars {
				return fmt.Errorf("GitHub repository %s has %d stars, minimum required is %d",
					source.URL, source.Stars, v.minStars)
			}
			continue
		}

		// Otherwise, fetch stars from GitHub API
		owner, repo, err := parseGitHubURL(source.URL)
		if err != nil {
			return fmt.Errorf("failed to parse GitHub URL %s: %w", source.URL, err)
		}

		stars, err := v.getStars(ctx, owner, repo)
		if err != nil {
			// If rate limited, check if minStars is set in source
			if v.rateLimited && source.MinStars > 0 && source.MinStars >= v.minStars {
				continue // Trust the source's minStars setting
			}
			return fmt.Errorf("failed to get stars for %s/%s: %w", owner, repo, err)
		}

		if stars < v.minStars {
			return fmt.Errorf("GitHub repository %s/%s has %d stars, minimum required is %d",
				owner, repo, stars, v.minStars)
		}
	}

	return nil
}

// getStars fetches the star count from GitHub API
func (v *GitHubVerifier) getStars(ctx context.Context, owner, repo string) (int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	// User-Agent is required by GitHub API
	req.Header.Set("User-Agent", "Universal-Installer/1.0")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Check for rate limiting
	if resp.StatusCode == http.StatusForbidden {
		v.rateLimited = true
		return 0, fmt.Errorf("GitHub API rate limited")
	}

	if resp.StatusCode == http.StatusNotFound {
		return 0, fmt.Errorf("repository not found: %s/%s", owner, repo)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var result struct {
		StargazersCount int `json:"stargazers_count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.StargazersCount, nil
}

// parseGitHubURL extracts owner and repo from a GitHub URL
func parseGitHubURL(url string) (owner, repo string, err error) {
	// Handle various GitHub URL formats:
	// - https://github.com/owner/repo
	// - https://github.com/owner/repo.git
	// - git@github.com:owner/repo.git
	// - owner/repo

	// Simple owner/repo format
	if !strings.Contains(url, "/") && !strings.Contains(url, "github.com") {
		return "", "", fmt.Errorf("invalid GitHub format: %s", url)
	}

	// Extract from full URL
	re := regexp.MustCompile(`(?:https?://github\.com/|git@github\.com:|^)?([^/]+)/([^/]+?)(?:\.git)?(?:/.*)?$`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", "", fmt.Errorf("failed to parse GitHub URL: %s", url)
	}

	return matches[1], matches[2], nil
}

// GetRepoInfo returns repository information including stars
func (v *GitHubVerifier) GetRepoInfo(ctx context.Context, owner, repo string) (*GitHubRepoInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Universal-Installer/1.0")

	resp, err := v.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var info GitHubRepoInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &info, nil
}

// GitHubRepoInfo contains repository information
type GitHubRepoInfo struct {
	FullName          string    `json:"full_name"`
	Description       string    `json:"description"`
	StargazersCount   int       `json:"stargazers_count"`
	WatchersCount     int       `json:"watchers_count"`
	ForksCount        int       `json:"forks_count"`
	OpenIssuesCount   int       `json:"open_issues_count"`
	License           *License  `json:"license"`
	Homepage          string    `json:"homepage"`
	Language          string    `json:"language"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	PushedAt          time.Time `json:"pushed_at"`
	Archived          bool      `json:"archived"`
	Disabled          bool      `json:"disabled"`
}

// License contains license information
type License struct {
	Name string `json:"name"`
	SpdxID string `json:"spdx_id"`
	URL   string `json:"url"`
}
