package microsoft365

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// TokenSource obtains a bearer access token for Graph. Implementations may use
// client secret (bootstrap), certificate assertion, or federated identity later
// without changing email.Transport.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// ClientSecretTokenSource performs OAuth2 client_credentials with a client secret.
// Suitable for development/bootstrap; production should prefer cert/federation.
//
// Cache: expiration-aware with ~60s refresh skew. Concurrent callers may both
// refresh when the cache is cold/expired (mutex is not held across network I/O).
type ClientSecretTokenSource struct {
	clientID     string
	clientSecret string
	scope        string
	tokenURL     string
	httpClient   *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewClientSecretTokenSource builds a TokenSource from Config.
func NewClientSecretTokenSource(cfg Config) (*ClientSecretTokenSource, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &ClientSecretTokenSource{
		clientID:     strings.TrimSpace(cfg.ClientID),
		clientSecret: cfg.ClientSecret, // keep exact secret; do not trim mid-secret
		scope:        graphScopeDefault,
		tokenURL:     cfg.tokenURL(),
		httpClient:   cfg.httpClient(),
	}, nil
}

// Token returns a cached access token or fetches a new one.
func (s *ClientSecretTokenSource) Token(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.mu.Lock()
	if s.token != "" && time.Now().Before(s.expiresAt.Add(-tokenRefreshSkew)) {
		tok := s.token
		s.mu.Unlock()
		return tok, nil
	}
	s.mu.Unlock()

	tok, exp, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.token = tok
	s.expiresAt = exp
	s.mu.Unlock()
	return tok, nil
}

func (s *ClientSecretTokenSource) fetch(ctx context.Context) (string, time.Time, error) {
	form := url.Values{}
	form.Set("client_id", s.clientID)
	form.Set("client_secret", s.clientSecret)
	form.Set("grant_type", "client_credentials")
	form.Set("scope", s.scope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, email.Transient(fmt.Errorf("token request build failed"))
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := s.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, mapTransportError(err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return "", time.Time{}, email.Transient(fmt.Errorf("token response read failed"))
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", time.Time{}, classifyTokenHTTP(res.StatusCode, body)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", time.Time{}, email.Permanent(fmt.Errorf("token response invalid JSON"))
	}
	if strings.TrimSpace(parsed.AccessToken) == "" {
		return "", time.Time{}, email.Permanent(fmt.Errorf("token response missing access_token"))
	}
	if parsed.ExpiresIn <= 0 {
		return "", time.Time{}, email.Permanent(fmt.Errorf("token response missing usable expires_in"))
	}
	exp := time.Now().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	return parsed.AccessToken, exp, nil
}
