package microsoft365

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// mapDoError classifies client.Do failures.
//
// requestWritten means httptrace.WroteRequest fired with Err==nil: the request
// bytes may have reached Graph. Those outcomes are ErrAmbiguousDelivery —
// automatic retry risks a duplicate patient email (LOT 26H-3). This is not
// proof of provider acceptance.
//
// Failures before a successful write remain ordinary transient / bare context errors.
func mapDoError(err error, requestWritten bool) error {
	if err == nil {
		return email.Transient(nil)
	}
	if requestWritten {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return email.AmbiguousDelivery(err)
		}
		return email.AmbiguousDelivery(fmt.Errorf("graph request outcome unknown"))
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

// mapTransportError classifies pre-dispatch / token HTTP client failures
// (never sendMail ambiguous — Graph mail was not attempted).
func mapTransportError(err error) error {
	return mapDoError(err, false)
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

func classifyGraphHTTP(status int, body []byte, requestID, retryAfterHeader string) error {
	detail := formatProviderError("graph", status, parseGraphErrorCode(body))
	if rid := safeRequestID(requestID); rid != "" {
		detail = detail + " request-id=" + rid
	}
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict:
		return email.Permanent(fmt.Errorf("%s", detail))
	case http.StatusRequestTimeout:
		return email.Transient(fmt.Errorf("%s", detail))
	case http.StatusTooManyRequests:
		// Retry-After floor applies only to confirmed 429 (LOT 26H-4).
		// delay-seconds only; HTTP-date intentionally unsupported.
		if d, ok := parseRetryAfterDelaySeconds(retryAfterHeader); ok {
			return email.TransientRetryAfter(fmt.Errorf("%s", detail), d)
		}
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

// parseRetryAfterDelaySeconds parses Graph/HTTP Retry-After as delay-seconds only.
// Malformed, zero, negative, overflow, and HTTP-date forms yield ok=false (ignore).
func parseRetryAfterDelaySeconds(raw string) (time.Duration, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, false
	}
	// Reject HTTP-date and any non-digit form; Graph throttling uses delay-seconds.
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	secs, err := strconv.ParseUint(s, 10, 32)
	if err != nil || secs == 0 {
		return 0, false
	}
	// Guard Duration multiply overflow even though uint32 seconds fit on amd64/arm64.
	const maxSecs = uint64(time.Duration(^uint64(0)>>1) / time.Second) // MaxInt64 / Second
	if secs > maxSecs {
		return 0, false
	}
	d := time.Duration(secs) * time.Second
	if d <= 0 {
		return 0, false
	}
	return d, true
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

// safeRequestID accepts opaque Graph request-id / client-request-id echo values
// (GUID-like). Rejects anything that could embed tokens or free text.
func safeRequestID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 80 {
		return ""
	}
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ""
	}
	return id
}
