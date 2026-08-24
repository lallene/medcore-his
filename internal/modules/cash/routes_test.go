package cash

import (
	"github.com/gin-gonic/gin"
	"testing"
)

func TestRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/api"), &Handler{})
	want := map[string]bool{"GET /api/cash/registers": false, "POST /api/cash/registers": false, "POST /api/cash/sessions/open": false, "GET /api/cash/sessions/current": false, "POST /api/cash/sessions/:id/payments": false, "POST /api/cash/sessions/:id/close": false, "GET /api/cash/receipts/:id": false}
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
