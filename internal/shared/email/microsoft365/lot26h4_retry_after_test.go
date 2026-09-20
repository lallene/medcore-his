package microsoft365_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

func TestTransportRetryAfter42926H4(t *testing.T) {
	cases := []struct {
		name       string
		header     string
		wantHint   time.Duration
		wantHintOK bool
	}{
		{"with_120s", "120", 2 * time.Minute, true},
		{"absent", "", 0, false},
		{"malformed", "soon", 0, false},
		{"http_date", "Wed, 21 Oct 2015 07:28:00 GMT", 0, false},
		{"zero", "0", 0, false},
		{"negative_text", "-5", 0, false},
		{"whitespace_ok", "  90  ", 90 * time.Second, true},
		{"overflow_uint32", "4294967296", 0, false},                   // MaxUint32+1
		{"overflow_huge", "999999999999999999999999999", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Retry-After", tc.header)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests","message":"throttle detail"}}`))
			}))
			defer srv.Close()
			tr, err := microsoft365.NewTransport(microsoft365.Config{
				Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
			}, staticToken("t"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tr.Send(context.Background(), validMsg())
			if !errors.Is(err, email.ErrTransient) {
				t.Fatalf("err=%v want ErrTransient", err)
			}
			if errors.Is(err, email.ErrAmbiguousDelivery) {
				t.Fatal("confirmed 429 must not be ambiguous")
			}
			d, ok := email.RetryAfter(err)
			if ok != tc.wantHintOK || d != tc.wantHint {
				t.Fatalf("RetryAfter=%v ok=%v want %v/%v", d, ok, tc.wantHint, tc.wantHintOK)
			}
			assertNoLeak(t, err, "throttle detail", "patient@example.com")
		})
	}
}

func TestTransport503IgnoresRetryAfter26H4(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"ErrorServerBusy","message":"busy"}}`))
	}))
	defer srv.Close()
	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), validMsg())
	if !errors.Is(err, email.ErrTransient) {
		t.Fatalf("err=%v", err)
	}
	if _, ok := email.RetryAfter(err); ok {
		t.Fatal("503 must not attach Retry-After hint in 26H-4 (429-only scope)")
	}
}
