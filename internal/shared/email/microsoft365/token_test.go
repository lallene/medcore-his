package microsoft365_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

const testTenantGUID = "11111111-1111-1111-1111-111111111111"

func testCfg(tokenURL string, client *http.Client) microsoft365.Config {
	return microsoft365.Config{
		TenantID: testTenantGUID, ClientID: "cid", ClientSecret: "sekrit",
		Sender: "noreply@example.com", TokenURL: tokenURL, HTTPClient: client,
	}
}

func TestClientSecretTokenRequestAndCache(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			t.Errorf("Content-Type=%q", ct)
		}
		b, _ := io.ReadAll(r.Body)
		form, err := url.ParseQuery(string(b))
		if err != nil {
			t.Fatal(err)
		}
		if form.Get("client_id") != "cid" {
			t.Errorf("client_id=%q", form.Get("client_id"))
		}
		if form.Get("client_secret") != "sekrit" {
			t.Errorf("client_secret mismatch")
		}
		if form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type=%q", form.Get("grant_type"))
		}
		if form.Get("scope") != "https://graph.microsoft.com/.default" {
			t.Errorf("scope=%q", form.Get("scope"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-1",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()

	ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}

	tok1, err := ts.Token(context.Background())
	if err != nil || tok1 != "tok-1" {
		t.Fatalf("tok1=%q err=%v", tok1, err)
	}
	tok2, err := ts.Token(context.Background())
	if err != nil || tok2 != "tok-1" {
		t.Fatalf("tok2=%q err=%v", tok2, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("cache miss: hits=%d", hits.Load())
	}
}

func TestClientSecretTokenRefreshNearExpiry(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("tok-%d", n),
			"expires_in":   30,
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()

	ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() < 2 {
		t.Fatalf("expected refresh near expiry, hits=%d", hits.Load())
	}
}

func TestClientSecretTokenClassifications(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"400", 400, `{"error":"invalid_client","error_description":"bad"}`, email.ErrPermanent},
		{"401", 401, `{"error":"invalid_client"}`, email.ErrPermanent},
		{"403", 403, `{"error":"unauthorized_client"}`, email.ErrPermanent},
		{"408", 408, `{"error":"temporarily_unavailable"}`, email.ErrTransient},
		{"429", 429, `{"error":"temporarily_unavailable"}`, email.ErrTransient},
		{"500", 500, `{"error":"server_error"}`, email.ErrTransient},
		{"malformed", 200, `{not-json`, email.ErrPermanent},
		{"empty_token", 200, `{"access_token":"","expires_in":3600}`, email.ErrPermanent},
		{"no_expiry", 200, `{"access_token":"x","expires_in":0}`, email.ErrPermanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
			if err != nil {
				t.Fatal(err)
			}
			_, err = ts.Token(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			assertNoLeak(t, err, "sekrit", "access_token_value", "bad")
		})
	}
}

func TestClientSecretTokenContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"access_token":"x","expires_in":3600}`))
	}))
	defer srv.Close()
	ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ts.Token(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientSecretTokenConcurrentColdRefresh(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(20 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "shared-tok",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer srv.Close()

	ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}

	const n = 16
	var wg sync.WaitGroup
	errs := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tok, e := ts.Token(context.Background())
			if e != nil {
				errs <- e
				return
			}
			if tok == "" {
				errs <- fmt.Errorf("empty token")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	if hits.Load() < 1 {
		t.Fatal("expected at least one token HTTP request")
	}
	// Concurrent cold refresh may issue more than one request; that is allowed.
}

func TestConfigNotConfigured(t *testing.T) {
	_, err := microsoft365.New(microsoft365.Config{})
	if !errors.Is(err, email.ErrNotConfigured) {
		t.Fatalf("err=%v", err)
	}
	assertNoLeak(t, err, "sekrit")
}

func assertNoLeak(t *testing.T, err error, forbidden ...string) {
	t.Helper()
	if err == nil {
		return
	}
	s := err.Error()
	for _, f := range forbidden {
		if f != "" && strings.Contains(s, f) {
			t.Fatalf("error leaked %q: %s", f, s)
		}
	}
	if strings.Contains(strings.ToLower(s), "bearer ") {
		t.Fatalf("error leaked bearer: %s", s)
	}
}
