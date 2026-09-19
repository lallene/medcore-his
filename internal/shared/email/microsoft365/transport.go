package microsoft365

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
)

// Transport sends mail through Microsoft Graph sendMail (app-only).
type Transport struct {
	tokens     TokenSource
	httpClient *http.Client
	graphBase  string
	sender     string
}

// NewTransport constructs a Graph email.Transport from Config and a TokenSource.
// Prefer New for the common client-secret bootstrap path.
func NewTransport(cfg Config, tokens TokenSource) (*Transport, error) {
	if tokens == nil {
		return nil, email.NotConfigured(fmt.Errorf("token source missing"))
	}
	if err := validateSender(cfg.Sender); err != nil {
		return nil, email.NotConfigured(err)
	}
	return &Transport{
		tokens:     tokens,
		httpClient: cfg.httpClient(),
		graphBase:  cfg.graphBase(),
		sender:     strings.TrimSpace(cfg.Sender),
	}, nil
}

// New builds Transport with ClientSecretTokenSource from Config.
func New(cfg Config) (*Transport, error) {
	ts, err := NewClientSecretTokenSource(cfg)
	if err != nil {
		return nil, err
	}
	return NewTransport(cfg, ts)
}

// ProviderName identifies this implementation (never includes credentials).
func (t *Transport) ProviderName() string { return providerName }

// Send posts a validated Message to Graph sendMail. No retries.
func (t *Transport) Send(ctx context.Context, msg email.Message) (email.Result, error) {
	if err := ctx.Err(); err != nil {
		return email.Result{}, err
	}
	if err := msg.Validate(); err != nil {
		return email.Result{}, err
	}

	token, err := t.tokens.Token(ctx)
	if err != nil {
		return email.Result{}, err
	}

	payload, err := mapMessage(msg)
	if err != nil {
		return email.Result{}, err
	}

	endpoint := t.graphBase + "/v1.0/users/" + url.PathEscape(t.sender) + "/sendMail"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return email.Result{}, email.Transient(fmt.Errorf("graph request build failed"))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	// IdempotencyKey is not forced into client-request-id (may not be UUID).
	// Correlation/dedupe remain LOT 26H; no exactly-once claim.

	res, err := t.httpClient.Do(req)
	if err != nil {
		return email.Result{}, mapTransportError(err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return email.Result{}, email.Transient(fmt.Errorf("graph response read failed"))
	}

	if res.StatusCode == http.StatusAccepted {
		// 202 Accepted: queued by Graph; empty body; no provider message id.
		return email.Result{ProviderMessageID: ""}, nil
	}
	return email.Result{}, classifyGraphHTTP(res.StatusCode, body)
}

var _ email.Transport = (*Transport)(nil)
