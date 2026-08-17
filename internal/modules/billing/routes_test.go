package billing

import (
	"github.com/gin-gonic/gin"
	"testing"
)

func TestRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/api"), &Handler{})
	want := map[string]bool{"GET /api/billing/invoices": false, "POST /api/billing/invoices": false, "GET /api/billing/billable-acts": false, "GET /api/billing/act-status": false, "POST /api/billing/invoices/:id/payments": false, "GET /api/patients/:id/invoices": false}
	for _, x := range r.Routes() {
		k := x.Method + " " + x.Path
		if _, ok := want[k]; ok {
			want[k] = true
		}
	}
	for route, ok := range want {
		if !ok {
			t.Fatalf("route missing: %s", route)
		}
	}
}
