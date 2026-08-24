package receivables

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	RegisterRoutes(g, NewHandler(nil))
	want := map[string]bool{"GET /api/receivables": false, "GET /api/receivables/kpis": false, "GET /api/receivables/:id": false, "PUT /api/receivables/:id/due-date": false, "POST /api/receivables/:id/follow-ups": false, "GET /api/patients/:id/receivables": false}
	for _, x := range r.Routes() {
		k := x.Method + " " + x.Path
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Fatal(k)
		}
	}
}

func TestRoutesEnforceDedicatedPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Test-Permissions"); raw != "" {
			c.Set(rbac.ContextPermissions, strings.Split(raw, ","))
		}
		c.Next()
	})
	g := r.Group("/api")
	RegisterRoutes(g, NewHandler(nil))

	request := func(method, path string, permissions []string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if permissions != nil {
			req.Header.Set("X-Test-Permissions", strings.Join(permissions, ","))
		}
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := request(http.MethodGet, "/api/receivables", nil); got != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", got)
	}
	if got := request(http.MethodPost, "/api/receivables/1/follow-ups", []string{"receivables.read"}); got != http.StatusForbidden {
		t.Fatalf("read permission unexpectedly created follow-up: %d", got)
	}
	if got := request(http.MethodPut, "/api/receivables/1/due-date", []string{"receivables.followup.create"}); got != http.StatusForbidden {
		t.Fatalf("follow-up permission unexpectedly changed due date: %d", got)
	}
}
