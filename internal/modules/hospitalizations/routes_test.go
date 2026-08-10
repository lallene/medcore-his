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

	RegisterRoutes(api, NewHandler(nil))

	routes := router.Routes()
	wanted := "/api/patients/:id/hospitalizations"
	for _, route := range routes {
		if route.Method == "GET" && route.Path == wanted {
			return
		}
	}

	t.Fatalf("route %s non enregistrée", wanted)
}
