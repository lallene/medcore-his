package microsoft365

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

const (
	defaultGraphBaseURL = "https://graph.microsoft.com"
	graphScopeDefault   = "https://graph.microsoft.com/.default"
	providerName        = "microsoft365"
	tokenRefreshSkew    = 60 * time.Second
	maxProviderCodeLen  = 80
)

// Config holds bootstrap settings for Microsoft Graph app-only email.
// Secrets must never appear in logs, String(), or error text.
//
// Sender must be a mailbox addr-spec / UPN (email form only). Arbitrary Graph
// user object IDs and path-like identifiers are rejected.
//
// TenantID must be a canonical tenant GUID or a strict DNS-like tenant domain
// (e.g. contoso.onmicrosoft.com). Path fragments are rejected.
type Config struct {
	TenantID     string
	ClientID     string
	ClientSecret string // bootstrap/dev only; prefer cert/federation in production
	Sender       string // mailbox addr-spec / UPN only

	// GraphBaseURL defaults to https://graph.microsoft.com (injectable for tests).
	GraphBaseURL string
	// TokenURL overrides the derived login.microsoftonline.com token endpoint (tests).
	TokenURL string

	HTTPClient *http.Client
}

// Validate reports email.ErrNotConfigured when required bootstrap fields are missing
// or fail the conservative identifier contracts. Secret values are never included.
func (c Config) Validate() error {
	if err := validateTenantID(c.TenantID); err != nil {
		return email.NotConfigured(err)
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return email.NotConfigured(fmt.Errorf("M365 client id missing"))
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return email.NotConfigured(fmt.Errorf("M365 client secret missing"))
	}
	if err := validateSender(c.Sender); err != nil {
		return email.NotConfigured(err)
	}
	return nil
}

// validateSender requires a single addr-spec (no display-name form) safe for a
// URL path segment. Dot-segments and path/control delimiters are rejected.
func validateSender(sender string) error {
	s := strings.TrimSpace(sender)
	if s == "" {
		return fmt.Errorf("M365 sender missing")
	}
	if s == "." || s == ".." {
		return fmt.Errorf("M365 sender invalid")
	}
	if strings.ContainsAny(s, `/\\?#`+"\r\n\t") {
		return fmt.Errorf("M365 sender invalid for URL path")
	}
	parsed, err := mail.ParseAddress(s)
	if err != nil || parsed.Name != "" || parsed.Address != s {
		return fmt.Errorf("M365 sender must be an email addr-spec")
	}
	return nil
}

func validateTenantID(tenant string) error {
	s := strings.TrimSpace(tenant)
	if s == "" {
		return fmt.Errorf("M365 tenant id missing")
	}
	if s == "." || s == ".." {
		return fmt.Errorf("M365 tenant id invalid")
	}
	if strings.ContainsAny(s, `/\\?#`+" \t\r\n") {
		return fmt.Errorf("M365 tenant id invalid")
	}
	if isCanonicalGUID(s) || isTenantDomain(s) {
		return nil
	}
	return fmt.Errorf("M365 tenant id invalid")
}

func isCanonicalGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	// 8-4-4-4-12 hexadecimal with lowercase/uppercase hex digits.
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHexDigit(r) {
				return false
			}
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isTenantDomain(s string) bool {
	if len(s) < 3 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") || strings.Contains(s, "..") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func (c Config) graphBase() string {
	b := strings.TrimRight(strings.TrimSpace(c.GraphBaseURL), "/")
	if b == "" {
		return defaultGraphBaseURL
	}
	return b
}

func (c Config) tokenURL() string {
	if u := strings.TrimSpace(c.TokenURL); u != "" {
		return u
	}
	tenant := strings.TrimSpace(c.TenantID)
	return "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"
}

func (c Config) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}
