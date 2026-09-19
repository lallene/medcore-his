package patients

import (
	"errors"
	"net/mail"
	"strings"
)

const maxPatientEmailLen = 150

// errInvalidPatientEmail is returned for malformed or display-name mailbox forms.
// The message must not echo the rejected input.
var errInvalidPatientEmail = errors.New("email invalide")

// NormalizePatientEmail trims surrounding whitespace and validates a canonical
// patient communication email. Empty/whitespace becomes "". Non-empty values
// must be a single addr-spec (no display-name mailbox form). Case is preserved
// after trim. Does not lowercase.
func NormalizePatientEmail(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if len(s) > maxPatientEmailLen {
		return "", errInvalidPatientEmail
	}
	parsed, err := mail.ParseAddress(s)
	if err != nil || parsed.Name != "" || parsed.Address != s {
		return "", errInvalidPatientEmail
	}
	return s, nil
}
