package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSecurityHeadersAndCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	r.Use(CORS("https://app.example.test"))
	r.Use(SecurityHeaders())
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "db": "up"})
	})
	r.GET("/api/grc/reporting", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/swagger/index.html", func(c *gin.Context) {
		c.String(http.StatusOK, "swagger")
	})

	health := httptest.NewRequest(http.MethodGet, "/health", nil)
	health.Header.Set("Origin", "https://app.example.test")
	health.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, health)
	if rec.Code != http.StatusOK {
		t.Fatalf("health %d", rec.Code)
	}
	assertHeader(t, rec, "X-Content-Type-Options", "nosniff")
	assertHeader(t, rec, "X-Frame-Options", "DENY")
	assertHeader(t, rec, "Referrer-Policy", "strict-origin-when-cross-origin")
	assertHeader(t, rec, "Strict-Transport-Security", "max-age=31536000")
	assertHeader(t, rec, "Cache-Control", "no-store")
	assertHeader(t, rec, "Access-Control-Allow-Origin", "https://app.example.test")
	if rec.Header().Get("Permissions-Policy") == "" || rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("missing policy headers: %v", rec.Header())
	}
	if rec.Header().Values("Content-Security-Policy") != nil && len(rec.Header().Values("Content-Security-Policy")) != 1 {
		t.Fatalf("duplicate CSP: %v", rec.Header().Values("Content-Security-Policy"))
	}
	body := rec.Body.String()
	if body == "" || strings.Contains(body, "password") || strings.Contains(body, "/Users/") {
		t.Fatalf("health body %q", body)
	}

	api := httptest.NewRecorder()
	r.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/grc/reporting", nil))
	assertHeader(t, api, "Cache-Control", "no-store")
	assertHeader(t, api, "Content-Security-Policy", "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'")

	swagger := httptest.NewRecorder()
	r.ServeHTTP(swagger, httptest.NewRequest(http.MethodGet, "/swagger/index.html", nil))
	if swagger.Header().Get("Cache-Control") == "no-store" {
		t.Fatal("swagger assets must keep their own cache policy")
	}
	if strings.Contains(swagger.Header().Get("Content-Security-Policy"), "default-src") {
		t.Fatal("swagger CSP must not inherit default-src 'none'")
	}
	if swagger.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("swagger lost nosniff")
	}

	denied := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/grc/reporting", nil)
	req.Header.Set("Origin", "https://evil.example.test")
	r.ServeHTTP(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("cors %d", denied.Code)
	}
	if denied.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin was reflected")
	}
}

func assertHeader(t *testing.T, rec *httptest.ResponseRecorder, key, want string) {
	t.Helper()
	if got := rec.Header().Get(key); got != want {
		t.Fatalf("%s=%q want %q", key, got, want)
	}
}
