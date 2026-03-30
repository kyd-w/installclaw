// Package source provides package source verification and security checks
package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kyd-w/installclaw/pkg/core/metadata"
)

// Verifier defines the interface for source verification
type Verifier interface {
	// Verify checks if the source is safe and trusted
	Verify(ctx context.Context, pkg *metadata.PackageMetadata) error

	// Name returns the verifier name
	Name() string
}

// SecurityConfig contains security verification configuration
type SecurityConfig struct {
	MinGitHubStars   int               // Minimum GitHub stars required (default: 200)
	TrustedDomains   map[string]bool   // Trusted official domains
	AllowUntrusted   bool              // Allow untrusted sources (not recommended)
	CheckChecksum    bool              // Verify checksums when available
	CheckSignature   bool              // Verify signatures when available
}

// DefaultSecurityConfig returns the default security configuration
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		MinGitHubStars: 200,
		TrustedDomains: getDefaultTrustedDomains(),
		AllowUntrusted: false,
		CheckChecksum:  true,
		CheckSignature: false, // Disabled by default as it requires GPG
	}
}

// getDefaultTrustedDomains returns the list of default trusted domains
func getDefaultTrustedDomains() map[string]bool {
	return map[string]bool{
		// Programming Languages
		"nodejs.org":       true,
		"python.org":       true,
		"golang.org":       true,
		"rust-lang.org":    true,
		"ruby-lang.org":    true,
		"php.net":          true,
		"java.com":         true,
		"openjdk.org":      true,

		// Databases
		"redis.io":         true,
		"mongodb.com":      true,
		"postgresql.org":   true,
		"mysql.com":        true,
		"sqlite.org":       true,

		// Web Servers
		"nginx.org":        true,
		"apache.org":       true,
		"caddyserver.com":  true,

		// Cloud & DevOps
		"docker.com":       true,
		"kubernetes.io":    true,
		"terraform.io":     true,
		"hashicorp.com":    true,
		"prometheus.io":    true,
		"grafana.com":      true,

		// Development Tools
		"git-scm.com":      true,
		"cmake.org":        true,
		"gnu.org":          true,

		// Package Registries
		"npmjs.com":        true,
		"pypi.org":         true,
		"crates.io":        true,
		"rubygems.org":     true,

		// Source Hosting
		"github.com":       true,
		"gitlab.com":       true,
		"bitbucket.org":    true,

		// Chinese Mirrors (commonly used in China)
		"npmmirror.com":    true,
		"tuna.tsinghua.edu.cn": true,
		"mirrors.aliyun.com": true,
		"goproxy.cn":       true,
	}
}

// CompositeVerifier combines multiple verifiers
type CompositeVerifier struct {
	verifiers []Verifier
	config    *SecurityConfig
}

// NewCompositeVerifier creates a new composite verifier
func NewCompositeVerifier(config *SecurityConfig) *CompositeVerifier {
	if config == nil {
		config = DefaultSecurityConfig()
	}

	cv := &CompositeVerifier{
		config: config,
	}

	// Add verifiers in order
	cv.verifiers = []Verifier{
		NewGitHubVerifier(config.MinGitHubStars),
		NewOfficialVerifier(config.TrustedDomains),
	}

	return cv
}

// Verify runs all verifiers on the package
func (v *CompositeVerifier) Verify(ctx context.Context, pkg *metadata.PackageMetadata) error {
	if pkg == nil {
		return fmt.Errorf("package is nil")
	}

	if len(pkg.Sources) == 0 {
		return fmt.Errorf("package %s has no sources defined", pkg.ID)
	}

	var errors []error
	for _, verifier := range v.verifiers {
		if err := verifier.Verify(ctx, pkg); err != nil {
			errors = append(errors, fmt.Errorf("[%s] %w", verifier.Name(), err))
		}
	}

	// If any source is trusted and verified, allow installation
	for _, source := range pkg.Sources {
		if source.Trusted {
			return nil
		}
	}

	if len(errors) > 0 && !v.config.AllowUntrusted {
		return fmt.Errorf("source verification failed: %v", errors)
	}

	return nil
}

// AddVerifier adds a custom verifier
func (v *CompositeVerifier) AddVerifier(verifier Verifier) {
	v.verifiers = append(v.verifiers, verifier)
}

// extractDomain extracts the domain from a URL
func extractDomain(rawURL string) (string, error) {
	// Handle URLs without scheme
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	return parsed.Hostname(), nil
}

// IsDomainTrusted checks if a domain is in the trusted list
func (v *CompositeVerifier) IsDomainTrusted(domain string) bool {
	return v.config.TrustedDomains[domain]
}

// VerificationResult contains detailed verification results
type VerificationResult struct {
	PackageID    string               `json:"packageId"`
	Safe         bool                 `json:"safe"`
	Score        int                  `json:"score"` // 0-100 safety score
	Results      []SourceVerification `json:"results"`
	Warnings     []string             `json:"warnings,omitempty"`
	Errors       []string             `json:"errors,omitempty"`
}

// SourceVerification contains verification result for a single source
type SourceVerification struct {
	SourceType string `json:"sourceType"`
	URL        string `json:"url"`
	Trusted    bool   `json:"trusted"`
	Safe       bool   `json:"safe"`
	Reason     string `json:"reason"`
}
