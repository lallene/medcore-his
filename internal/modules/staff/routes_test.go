package staff

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaffBackendRejectsMissingPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { rbac.SetUser(c, 7, "staff", []string{"staff.read"}); c.Next() })
	RegisterRoutes(r.Group("/api"), NewHandler(nil))
	req := httptest.NewRequest(http.MethodPost, "/api/staff", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
