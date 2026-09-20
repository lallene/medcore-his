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

	"github.com/google/uuid"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

func TestTransportStableCorrelationAndClientRequestID26H3(t *testing.T) {
	var clientIDs []string
	var headerValues []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cid := r.Header.Get("client-request-id")
		clientIDs = append(clientIDs, cid)
		if cid == "" {
			t.Error("missing client-request-id")
		}
		if _, err := uuid.Parse(cid); err != nil {
			t.Errorf("client-request-id not UUID: %q", cid)
		}
		if r.Header.Get("return-client-request-id") != "true" {
			t.Errorf("return-client-request-id=%q", r.Header.Get("return-client-request-id"))
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		msg := payload["message"].(map[string]any)
		if msg["subject"] != "Rappel" {
			t.Errorf("subject mutated: %v", msg["subject"])
		}
		body := msg["body"].(map[string]any)
		if body["content"] != "Ouvrez MedCore HIS." {
			t.Errorf("body mutated: %v", body)
		}
		recs := msg["toRecipients"].([]any)
		ea := recs[0].(map[string]any)["emailAddress"].(map[string]any)
		if ea["address"] != "patient@example.com" {
			t.Errorf("recipient mutated: %v", ea)
		}
		headers, _ := msg["internetMessageHeaders"].([]any)
		if len(headers) != 1 {
			t.Fatalf("internetMessageHeaders=%v", headers)
		}
		h := headers[0].(map[string]any)
		if h["name"] != "x-medcore-notification-intent-id" || h["value"] != "42" {
			t.Fatalf("header=%v", h)
		}
		headerValues = append(headerValues, h["value"].(string))
		w.Header().Set("request-id", "11111111-2222-3333-4444-555555555555")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "noreply@clinic.example", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("test-token"))
	if err != nil {
		t.Fatal(err)
	}
	msg := validMsg()
	msg.IdempotencyKey = "notification-intent:42"

	for i := 0; i < 2; i++ {
		res, err := tr.Send(context.Background(), msg)
		if err != nil {
			t.Fatal(err)
		}
		if res.ProviderMessageID != "" {
			t.Fatalf("ProviderMessageID=%q", res.ProviderMessageID)
		}
	}
	if len(clientIDs) != 2 || clientIDs[0] == "" || clientIDs[0] == clientIDs[1] {
		t.Fatalf("client-request-id must differ per attempt: %v", clientIDs)
	}
	if len(headerValues) != 2 || headerValues[0] != "42" || headerValues[1] != "42" {
		t.Fatalf("stable correlation broken: %v", headerValues)
	}
}

func TestTransportNonCanonicalIdempotencyKeyOmitsCorrelationHeader26H3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		msg := payload["message"].(map[string]any)
		if _, ok := msg["internetMessageHeaders"]; ok {
			t.Fatalf("unexpected internetMessageHeaders: %v", msg["internetMessageHeaders"])
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, _ := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	msg := validMsg()
	msg.IdempotencyKey = "unrelated-key"
	if _, err := tr.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
}

func TestTransportConfirmedHTTPNotAmbiguous26H3(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{http.StatusBadRequest, email.ErrPermanent},
		{http.StatusTooManyRequests, email.ErrTransient},
		{http.StatusServiceUnavailable, email.ErrTransient},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("request-id", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"code":"ErrorServerBusy","message":"busy detail"}}`))
			}))
			defer srv.Close()
			tr, err := microsoft365.NewTransport(microsoft365.Config{
				Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
			}, staticToken("t"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tr.Send(context.Background(), validMsg())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if errors.Is(err, email.ErrAmbiguousDelivery) {
				t.Fatal("confirmed HTTP must not be ambiguous")
			}
			if !strings.Contains(err.Error(), "request-id=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") {
				t.Fatalf("missing safe request-id: %v", err)
			}
			assertNoLeak(t, err, "busy detail", "patient@example.com")
		})
	}
}

func TestTransportAmbiguousAfterRequestWritten26H3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("ResponseWriter is not a Hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), validMsg())
	if !errors.Is(err, email.ErrAmbiguousDelivery) {
		t.Fatalf("err=%v want ErrAmbiguousDelivery", err)
	}
	if errors.Is(err, email.ErrTransient) {
		t.Fatal("ambiguous must not also be classified only as transient for worker terminal path")
	}
	assertNoLeak(t, err, "patient@example.com", "Ouvrez MedCore")
}

func TestTransportTokenFailureNotAmbiguous26H3(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, failToken{err: email.Transient(errors.New("token unavailable"))})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), validMsg())
	if !errors.Is(err, email.ErrTransient) {
		t.Fatalf("err=%v", err)
	}
	if errors.Is(err, email.ErrAmbiguousDelivery) {
		t.Fatal("token failure is pre-dispatch")
	}
	if hits.Load() != 0 {
		t.Fatal("sendMail must not be called")
	}
}

func TestTransportCancelAfterDispatchAmbiguous26H3(t *testing.T) {
	arrived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(arrived)
		<-r.Context().Done()
	}))
	defer srv.Close()

	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, e := tr.Send(ctx, validMsg())
		done <- e
	}()
	<-arrived
	cancel()
	err = <-done
	if !errors.Is(err, email.ErrAmbiguousDelivery) {
		t.Fatalf("err=%v want ErrAmbiguousDelivery", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("underlying cancel should unwrap: %v", err)
	}
}
