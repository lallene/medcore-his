package coverage

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// LOT30-A′: GET /insurance/coverages (all patients) requires coverage administration rights.
func TestLOT30APrime_CoverageListRequiresAdministration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := func(perms []string, path string) int {
		r := gin.New()
		// Recovery turns the nil-service panic past the RBAC gate into a 500 (gate passed).
		r.Use(gin.RecoveryWithWriter(io.Discard))
		r.Use(func(c *gin.Context) {
			rbac.SetUser(c, 9, "staff", perms)
			c.Next()
		})
		RegisterRoutes(r.Group("/api"), NewHandler(nil))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w.Code
	}
	if got := status([]string{"insurance.coverage.read"}, "/api/insurance/coverages"); got != http.StatusForbidden {
		t.Fatalf("read-only actor must not list every membership, got %d", got)
	}
	if got := status([]string{"patients:read", "patients.360.read"}, "/api/insurance/coverages"); got != http.StatusForbidden {
		t.Fatalf("clinical actor must not list memberships, got %d", got)
	}
	for _, admin := range [][]string{
		{"insurance.coverage.read", "insurance.coverage.update"},
		{"insurance.coverage.read", "insurance.coverage.create"},
		{"*"},
	} {
		if got := status(admin, "/api/insurance/coverages"); got == http.StatusForbidden || got == http.StatusUnauthorized {
			t.Fatalf("coverage administrator %v must pass RBAC, got %d", admin, got)
		}
	}
}
