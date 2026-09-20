package microsoft365

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"

	"github.com/google/uuid"

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
//
// Correlation (LOT 26H-3):
//   - stable message correlation via internetMessageHeaders when IdempotencyKey
//     matches notification-intent:<id> (not Graph idempotency / dedupe)
//   - unique client-request-id UUID per HTTP attempt
//
// Ambiguous outcomes: when httptrace WroteRequest succeeds (Err==nil) and Do
// fails without a usable response, returns email.ErrAmbiguousDelivery.
// WroteRequest does not prove Graph acceptance — only that the outcome may be
// uncertain and must not auto-retry as ordinary transient.
//
// Residual gap: HTTP 202 then process crash / DB finalize failure can still leave
// PROCESSING and later resend — correlation aids forensics, not exactly-once.
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

	clientRequestID := uuid.NewString()
	req.Header.Set("client-request-id", clientRequestID)
	req.Header.Set("return-client-request-id", "true")

	// WroteRequest means the request write finished (or failed); it does NOT mean
	// Graph accepted the message. Only a successful write (Err==nil) raises the
	// ambiguous-outcome flag if Do later returns without a response.
	var requestWritten bool
	trace := &httptrace.ClientTrace{
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				requestWritten = true
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	res, err := t.httpClient.Do(req)
	if err != nil {
		return email.Result{}, mapDoError(err, requestWritten)
	}
	defer res.Body.Close()

	graphRequestID := res.Header.Get("request-id")
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		// Response headers already received — classify by status, not ambiguous.
		if res.StatusCode == http.StatusAccepted {
			return email.Result{ProviderMessageID: ""}, nil
		}
		return email.Result{}, classifyGraphHTTP(res.StatusCode, nil, graphRequestID, res.Header.Get("Retry-After"))
	}

	if res.StatusCode == http.StatusAccepted {
		// 202 Accepted: queued by Graph; empty body; no provider message id.
		// Do not invent ProviderMessageID from request-id / client-request-id.
		return email.Result{ProviderMessageID: ""}, nil
	}
	return email.Result{}, classifyGraphHTTP(res.StatusCode, body, graphRequestID, res.Header.Get("Retry-After"))
}

var _ email.Transport = (*Transport)(nil)
