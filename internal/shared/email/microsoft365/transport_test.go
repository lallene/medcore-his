package microsoft365_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

type staticToken string

func (s staticToken) Token(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(s), nil
}

type failToken struct{ err error }

func (f failToken) Token(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", f.err
}

func validMsg() email.Message {
	return email.Message{
		To:       email.Address{Address: "patient@example.com", DisplayName: "Patient"},
		Subject:  "Rappel",
		TextBody: "Ouvrez MedCore HIS.",
	}
}

func TestTransportSendMailSuccessAndMapping(t *testing.T) {
	var saw atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/sendMail") || !strings.Contains(r.URL.Path, "noreply@clinic.example") {
			t.Errorf("path=%q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization=%q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type=%q", r.Header.Get("Content-Type"))
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		msg := payload["message"].(map[string]any)
		if msg["subject"] != "Rappel" {
			t.Errorf("subject=%v", msg["subject"])
		}
		body := msg["body"].(map[string]any)
		if body["contentType"] != "Text" || body["content"] != "Ouvrez MedCore HIS." {
			t.Errorf("body=%v", body)
		}
		recs := msg["toRecipients"].([]any)
		ea := recs[0].(map[string]any)["emailAddress"].(map[string]any)
		if ea["address"] != "patient@example.com" || ea["name"] != "Patient" {
			t.Errorf("emailAddress=%v", ea)
		}
		for _, k := range []string{"diagnosis", "prescription", "payload", "patientId"} {
			if _, ok := payload[k]; ok {
				t.Errorf("unexpected field %s", k)
			}
			if _, ok := msg[k]; ok {
				t.Errorf("unexpected message field %s", k)
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "noreply@clinic.example", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}
	if tr.ProviderName() != "microsoft365" {
		t.Fatalf("ProviderName=%q", tr.ProviderName())
	}

	res, err := tr.Send(context.Background(), validMsg())
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderMessageID != "" {
		t.Fatalf("ProviderMessageID=%q want empty", res.ProviderMessageID)
	}
	if saw.Load() != 1 {
		t.Fatalf("hits=%d", saw.Load())
	}
}

func TestTransportHTMLPreferred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		body := payload["message"].(map[string]any)["body"].(map[string]any)
		if body["contentType"] != "HTML" || body["content"] != "<p>Hi</p>" {
			t.Errorf("body=%v", body)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	msg := email.Message{
		To: email.Address{Address: "x@y.z"}, Subject: "S",
		TextBody: "plain", HTMLBody: "<p>Hi</p>",
	}
	if _, err := tr.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestTransportDisplayNameOmittedWhenEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		ea := payload["message"].(map[string]any)["toRecipients"].([]any)[0].(map[string]any)["emailAddress"].(map[string]any)
		if _, ok := ea["name"]; ok {
			t.Errorf("name present: %v", ea)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, _ := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	msg := email.Message{To: email.Address{Address: "x@y.z"}, Subject: "S", TextBody: "t"}
	if _, err := tr.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestTransportGraphErrorMatrix(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   error
	}{
		{400, `{"error":{"code":"ErrorInvalidRecipient","message":"bad recipient"}}`, email.ErrPermanent},
		{401, `{"error":{"code":"InvalidAuthenticationToken","message":"token"}}`, email.ErrPermanent},
		{403, `{"error":{"code":"ErrorAccessDenied","message":"denied"}}`, email.ErrPermanent},
		{404, `{"error":{"code":"ErrorItemNotFound","message":"missing mailbox"}}`, email.ErrPermanent},
		{408, `{"error":{"code":"Timeout","message":"slow"}}`, email.ErrTransient},
		{409, `{"error":{"code":"Conflict","message":"conflict"}}`, email.ErrPermanent},
		{429, `{"error":{"code":"TooManyRequests","message":"throttle"}}`, email.ErrTransient},
		{500, `{"error":{"code":"ErrorInternalServerError","message":"boom"}}`, email.ErrTransient},
		{503, `{"error":{"code":"ErrorServerBusy","message":"busy"}}`, email.ErrTransient},
		{400, `{not-json`, email.ErrPermanent},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			tr, err := microsoft365.NewTransport(microsoft365.Config{
				Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
			}, staticToken("super-secret-token-value"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tr.Send(context.Background(), validMsg())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			assertNoLeak(t, err, "super-secret-token-value", "sekrit", "bad recipient", "missing mailbox", "throttle", "boom")
			if err != nil && strings.Contains(err.Error(), "message") && strings.Contains(tc.body, `"message"`) {
				// provider message text must not appear; structural word "message" in our format is unused.
			}
		})
	}
}

func TestTransportInvalidMessageSkipsNetwork(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, failToken{err: errors.New("token should not be called")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), email.Message{
		To: email.Address{Address: "John <john@example.com>"}, Subject: "S", TextBody: "x",
	})
	if !errors.Is(err, email.ErrInvalidMessage) {
		t.Fatalf("err=%v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("network called")
	}
}

func TestTransportContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, _ := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.Send(ctx, validMsg())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestTransportNetworkErrorTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: base, HTTPClient: &http.Client{Timeout: time.Second},
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), validMsg())
	if !errors.Is(err, email.ErrTransient) {
		t.Fatalf("err=%v", err)
	}
}

func TestTransportSenderAddrSpecAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "noreply@clinic.example") || !strings.HasSuffix(r.URL.Path, "/sendMail") {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "noreply@clinic.example", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Send(context.Background(), validMsg()); err != nil {
		t.Fatal(err)
	}
}
