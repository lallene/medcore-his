package imaging

import (
	"github.com/gin-gonic/gin"
	"testing"
)

func TestImagingTableNamesCategoriesAndModalities(t *testing.T) {
	if (Order{}).TableName() != "imaging_orders" || (Report{}).TableName() != "imaging_reports" {
		t.Fatal("noms de tables imaging invalides")
	}
	for _, c := range []string{"Imagerie", " imagerie "} {
		if !IsImagingCategory(c) {
			t.Errorf("catégorie refusée: %s", c)
		}
	}
	for _, c := range []string{"Laboratoire", "Biologie", "Cardiologie", "ORL", "Radiologie"} {
		if IsImagingCategory(c) {
			t.Errorf("catégorie absorbée: %s", c)
		}
	}
	cases := map[string]string{"CHEST_XRAY": "XRAY", "ABDOMINAL_ULTRASOUND": "ULTRASOUND", "CT_SCAN": "CT", "MRI": "MRI", "MAMMOGRAPHY": "MAMMOGRAPHY", "UNKNOWN": "OTHER"}
	for code, want := range cases {
		if got := modalityForExamCode(code); got != want {
			t.Errorf("%s=%s, attendu %s", code, got, want)
		}
	}
}

func TestImagingRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterRoutes(api, NewHandler(nil))
	want := map[string]bool{"GET /api/imaging/orders": false, "POST /api/imaging/orders/:id/schedule": false, "POST /api/imaging/orders/:id/start": false, "PUT /api/imaging/orders/:id/report": false, "POST /api/imaging/orders/:id/validate": false, "GET /api/patients/:id/imaging-orders": false}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("route absente: %s", route)
		}
	}
}
