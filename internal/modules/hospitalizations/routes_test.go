package hospitalizations

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRoutesRegisterAlongsideExistingPatientWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")

	api.GET("/patients/:id", func(c *gin.Context) {})
	api.GET("/patients/:id/medical-record", func(c *gin.Context) {})

	RegisterRoutes(api, NewHandler(nil), NewBedHandler(nil))

	routes := router.Routes()
	wanted := map[string]bool{"GET /api/patients/:id/hospitalizations": false, "GET /api/rooms": false, "GET /api/beds": false, "POST /api/hospitalizations/:id/bed-assignments": false, "POST /api/hospitalizations/:id/transfer": false, "POST /api/hospitalizations/:id/release-bed": false}
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, ok := wanted[key]; ok {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Fatalf("route %s non enregistrée", route)
		}
	}
}
