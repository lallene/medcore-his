package microsoft365

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"unicode"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

func mapTransportError(err error) error {
	if err == nil {
		return email.Transient(nil)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return email.Transient(fmt.Errorf("network error"))
	}
	return email.Transient(fmt.Errorf("transport error"))
}

func classifyTokenHTTP(status int, body []byte) error {
	detail := formatProviderError("token", status, parseOAuthErrorCode(body))
	switch {
	case status == http.StatusTooManyRequests:
		return email.Transient(fmt.Errorf("%s", detail))
	case status == http.StatusRequestTimeout:
		return email.Transient(fmt.Errorf("%s", detail))
	case status >= 500:
		return email.Transient(fmt.Errorf("%s", detail))
	case status >= 400:
		return email.Permanent(fmt.Errorf("%s", detail))
	default:
		return email.Permanent(fmt.Errorf("%s", detail))
	}
}

func classifyGraphHTTP(status int, body []byte) error {
	detail := formatProviderError("graph", status, parseGraphErrorCode(body))
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return email.Permanent(fmt.Errorf("%s", detail))
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return email.Transient(fmt.Errorf("%s", detail))
	default:
		if status >= 500 {
			return email.Transient(fmt.Errorf("%s", detail))
		}
		if status >= 400 {
			return email.Permanent(fmt.Errorf("%s", detail))
		}
		return email.Permanent(fmt.Errorf("%s", detail))
	}
}

func parseGraphErrorCode(body []byte) string {
	var env graphErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.Error == nil {
		return ""
	}
	return env.Error.Code
}

func parseOAuthErrorCode(body []byte) string {
	var oauth oauthErrorBody
	if err := json.Unmarshal(body, &oauth); err == nil && oauth.Error != "" {
		return oauth.Error
	}
	return parseGraphErrorCode(body)
}

// formatProviderError returns only trusted status plus an optional safely bounded
// provider error code. Provider descriptive text (message / error_description)
// is never included.
func formatProviderError(kind string, status int, code string) string {
	code = safeProviderCode(code)
	if code == "" {
		return fmt.Sprintf("%s http %d", kind, status)
	}
	return fmt.Sprintf("%s http %d: %s", kind, status, code)
}

func safeProviderCode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > maxProviderCodeLen {
		return ""
	}
	for _, r := range code {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return ""
	}
	return code
}
