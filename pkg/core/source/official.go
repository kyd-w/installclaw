package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/kyd-w/installclaw/pkg/core/metadata"
)

// OfficialVerifier verifies official website sources
type OfficialVerifier struct {
	trustedDomains map[string]bool
}

// NewOfficialVerifier creates a new official source verifier
func NewOfficialVerifier(trustedDomains map[string]bool) *OfficialVerifier {
	if trustedDomains == nil {
		trustedDomains = getDefaultTrustedDomains()
	}

	return &OfficialVerifier{
		trustedDomains: trustedDomains,
	}
}

// Name returns the verifier name
func (v *OfficialVerifier) Name() string {
	return "official"
}

// Verify checks if official sources are from trusted domains
func (v *OfficialVerifier) Verify(ctx context.Context, pkg *metadata.PackageMetadata) error {
	for _, source := range pkg.Sources {
		if source.Type != metadata.SourceOfficial {
			continue
		}

		if source.URL == "" {
			return fmt.Errorf("official source has empty URL")
		}

		domain, err := extractDomain(source.URL)
		if err != nil {
			return fmt.Errorf("failed to extract domain from %s: %w", source.URL, err)
		}

		// Check if domain is trusted
		if !v.IsTrustedDomain(domain) {
			return fmt.Errorf("domain %s is not in the trusted list", domain)
		}
	}

	return nil
}

// IsTrustedDomain checks if a domain is trusted
func (v *OfficialVerifier) IsTrustedDomain(domain string) bool {
	// Direct match
	if v.trustedDomains[domain] {
		return true
	}

	// Check parent domain (e.g., subdomain.nodejs.org -> nodejs.org)
	parts := strings.Split(domain, ".")
	if len(parts) > 2 {
		parentDomain := strings.Join(parts[len(parts)-2:], ".")
		if v.trustedDomains[parentDomain] {
			return true
		}
	}

	return false
}

// AddTrustedDomain adds a domain to the trusted list
func (v *OfficialVerifier) AddTrustedDomain(domain string) {
	v.trustedDomains[domain] = true
}

// RemoveTrustedDomain removes a domain from the trusted list
func (v *OfficialVerifier) RemoveTrustedDomain(domain string) {
	delete(v.trustedDomains, domain)
}

// GetTrustedDomains returns all trusted domains
func (v *OfficialVerifier) GetTrustedDomains() []string {
	domains := make([]string, 0, len(v.trustedDomains))
	for domain := range v.trustedDomains {
		domains = append(domains, domain)
	}
	return domains
}
