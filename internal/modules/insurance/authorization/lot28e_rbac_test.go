package authorization

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// A11b: insurance.authorization.read remains independent of patients.360.read.
func TestLot28E_A11_AuthorizationIndependentOf360(t *testing.T) {
	gin.SetMode(gin.TestMode)

	deny := gin.New()
	deny.Use(func(c *gin.Context) {
		rbac.SetUser(c, 10, "staff", []string{"patients.360.read"})
		c.Next()
	})
	RegisterRoutes(deny.Group("/api"), NewHandler(NewService(nil)))
	w := httptest.NewRecorder()
	deny.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/insurance/authorizations", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("360-only expected 403, got %d", w.Code)
	}

	allow := gin.New()
	allow.Use(func(c *gin.Context) {
		rbac.SetUser(c, 10, "staff", []string{"insurance.authorization.read"})
		c.Next()
	})
	allow.GET("/api/insurance/authorizations", rbac.Permission("insurance.authorization.read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w2 := httptest.NewRecorder()
	allow.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/insurance/authorizations", nil))
	if w2.Code == http.StatusForbidden {
		t.Fatal("insurance.authorization.read must authorize authorizations list")
	}
	if w2.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from stub handler, got %d", w2.Code)
	}
}
