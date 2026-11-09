// Package features resolves integration feature flags.
package features

import (
	"strings"

	"morrow-api/internal/config"
)

// PepyIntegrationID is the integrations key for SoftwareSeni Pepy.
const PepyIntegrationID = "softwareseni-pepy"

// PepyEnabled reports whether Pepy is enabled for the account email. An empty
// allowlist means nobody: the Pepy account is shared, so access must be explicit.
func PepyEnabled(cfg *config.Config, email string) bool {
	if cfg == nil || !cfg.PepyEnabled || cfg.PepyUsername == "" || cfg.PepyPassword == "" {
		return false
	}

	email = strings.ToLower(strings.TrimSpace(email))
	for _, allowed := range cfg.PepyAllowedEmails {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "*" || (email != "" && allowed == email) {
			return true
		}
	}

	return false
}
