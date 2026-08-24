package insurance_receivables

import (
	"github.com/gin-gonic/gin"
	"testing"
)

func TestRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r.Group("/api"), NewHandler(nil))
	want := map[string]bool{"GET /api/insurance-receivables": false, "GET /api/insurance-receivables/kpis": false, "POST /api/insurance-receivables/:id/followups": false, "POST /api/insurance-receivables/settlements": false, "POST /api/insurance-receivables/settlements/:id/allocations": false, "POST /api/insurance-receivables/settlements/:id/post": false, "POST /api/insurance-receivables/batches": false, "POST /api/insurance-receivables/batches/:id/submit": false, "GET /api/patients/:id/insurance-receivables": false}
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
