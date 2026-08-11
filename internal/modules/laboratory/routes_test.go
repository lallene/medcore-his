package laboratory

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLaboratoryModelsUseRepositoryTableNames(t *testing.T) {
	tests := []struct {
		got  string
		want string
	}{
		{Order{}.TableName(), "laboratory_orders"},
		{Sample{}.TableName(), "laboratory_samples"},
		{Result{}.TableName(), "laboratory_results"},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("table GORM = %q, attendu %q", test.got, test.want)
		}
	}
}

func TestLaboratoryCategoryClassification(t *testing.T) {
	for _, category := range []string{"Laboratoire", " Biologie ", "Biochimie", "Hématologie", "Microbiologie"} {
		if !IsLaboratoryCategory(category) {
			t.Errorf("catégorie biologique refusée: %s", category)
		}
	}
	for _, category := range []string{"Imagerie", "Cardiologie", "ORL"} {
		if IsLaboratoryCategory(category) {
			t.Errorf("catégorie non biologique acceptée: %s", category)
		}
	}
}

func TestLaboratoryRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterRoutes(api, NewHandler(nil))
	want := map[string]bool{
		"GET /api/laboratory/orders":               false,
		"POST /api/laboratory/orders/:id/collect":  false,
		"PUT /api/laboratory/orders/:id/results":   false,
		"POST /api/laboratory/orders/:id/validate": false,
		"GET /api/patients/:id/laboratory-orders":  false,
	}
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
