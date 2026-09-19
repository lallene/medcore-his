package microsoft365_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/shared/email"
	"github.com/lallene/medcore-his/backend/internal/shared/email/microsoft365"
)

func TestAdversarialGraphErrorNeverEchoesSecrets(t *testing.T) {
	sensitive := []string{
		"eyJhbGciOiJIUzI1NiJ9.super-secret.jwt",
		"super-secret-token-value",
		"Bearer secret-access-token",
		"client-secret-value",
		"UNIQUE_PROVIDER_MESSAGE_SHOULD_NEVER_APPEAR",
	}
	body := `{"error":{"code":"InvalidAuthenticationToken","message":"` + strings.Join(sensitive, " ") + `"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tr, err := microsoft365.NewTransport(microsoft365.Config{
		Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
	}, staticToken("local-access-token-xyz"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Send(context.Background(), validMsg())
	if !errors.Is(err, email.ErrPermanent) {
		t.Fatalf("err=%v", err)
	}
	got := err.Error()
	for _, s := range sensitive {
		if strings.Contains(got, s) {
			t.Fatalf("leaked %q in %q", s, got)
		}
	}
	if strings.Contains(got, "UNIQUE_PROVIDER") {
		t.Fatalf("provider message echoed: %q", got)
	}
	if !strings.Contains(got, "graph http 401") {
		t.Fatalf("missing structural status: %q", got)
	}
	if !strings.Contains(got, "InvalidAuthenticationToken") {
		t.Fatalf("safe code missing: %q", got)
	}
	assertNoLeak(t, err, "local-access-token-xyz")
}

func TestAdversarialTokenErrorNeverEchoesDescription(t *testing.T) {
	desc := "client secret sekrit-leaked and access token eyJhbGciOi.secret.jwt present"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"` + desc + `"}`))
	}))
	defer srv.Close()

	ts, err := microsoft365.NewClientSecretTokenSource(testCfg(srv.URL, srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ts.Token(context.Background())
	if !errors.Is(err, email.ErrPermanent) {
		t.Fatalf("err=%v", err)
	}
	got := err.Error()
	for _, s := range []string{desc, "sekrit-leaked", "eyJhbGciOi.secret.jwt", "error_description"} {
		if strings.Contains(got, s) {
			t.Fatalf("leaked %q in %q", s, got)
		}
	}
	if !strings.Contains(got, "token http 400: invalid_client") {
		t.Fatalf("got %q", got)
	}
	assertNoLeak(t, err, "sekrit")
}

func TestUnsafeProviderCodesOmitted(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"overlong", strings.Repeat("A", 81)},
		{"whitespace", "Bad Code"},
		{"crlf", "Bad\r\nCode"},
		{"tokenish", "eyJhbGciOi:not-a-safe-code"},
		{"at_sign", "Error@Access"},
		{"slash", "Error/Access"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":` + jsonString(tc.code) + `,"message":"IGNORE_ME_SECRET"}}`))
			}))
			defer srv.Close()
			tr, err := microsoft365.NewTransport(microsoft365.Config{
				Sender: "a@b.co", GraphBaseURL: srv.URL, HTTPClient: srv.Client(),
			}, staticToken("t"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tr.Send(context.Background(), validMsg())
			if !errors.Is(err, email.ErrPermanent) {
				t.Fatalf("err=%v", err)
			}
			got := err.Error()
			if got != "graph http 400" && !strings.HasPrefix(got, "email:") {
				// Permanent wraps: "email: permanent failure: graph http 400"
			}
			if !strings.Contains(got, "graph http 400") {
				t.Fatalf("got %q", got)
			}
			if strings.Contains(got, "IGNORE_ME_SECRET") || strings.Contains(got, tc.code) {
				t.Fatalf("unsafe code/message leaked: %q", got)
			}
		})
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestSenderPathConfinementRejected(t *testing.T) {
	invalid := []string{
		".", "..", "../x", "x/..", `\\`, `x\y`, "x?y", "x#y",
		"a\rb@c.com", "a\nb@c.com", "a\tb@c.com",
		"John <a@b.co>", "notify bot@clinic.example",
	}
	for _, sender := range invalid {
		t.Run(sender, func(t *testing.T) {
			err := microsoft365.Config{
				TenantID: testTenantGUID, ClientID: "c", ClientSecret: "s", Sender: sender,
			}.Validate()
			if !errors.Is(err, email.ErrNotConfigured) {
				t.Fatalf("sender %q: err=%v", sender, err)
			}
			_, err = microsoft365.NewTransport(microsoft365.Config{Sender: sender}, staticToken("t"))
			if !errors.Is(err, email.ErrNotConfigured) {
				t.Fatalf("NewTransport sender %q: err=%v", sender, err)
			}
		})
	}

	err := microsoft365.Config{
		TenantID: testTenantGUID, ClientID: "c", ClientSecret: "s", Sender: "sender@example.com",
	}.Validate()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTenantIDValidation(t *testing.T) {
	invalid := []string{
		".", "..", "../tenant", "tenant/x", `tenant\x`, "tenant?x", "tenant#x",
		"ten ant", "tenant\nid", "tid",
	}
	for _, tenant := range invalid {
		t.Run(tenant, func(t *testing.T) {
			err := microsoft365.Config{
				TenantID: tenant, ClientID: "c", ClientSecret: "s", Sender: "a@b.co",
			}.Validate()
			if !errors.Is(err, email.ErrNotConfigured) {
				t.Fatalf("tenant %q: err=%v", tenant, err)
			}
		})
	}

	for _, tenant := range []string{testTenantGUID, "contoso.onmicrosoft.com"} {
		err := microsoft365.Config{
			TenantID: tenant, ClientID: "c", ClientSecret: "s", Sender: "a@b.co",
		}.Validate()
		if err != nil {
			t.Fatalf("valid tenant %q: %v", tenant, err)
		}
	}
}
