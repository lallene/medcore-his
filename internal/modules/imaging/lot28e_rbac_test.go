package imaging

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A07: imaging.read remains independent of patients.360.read.
func TestLot28E_A07_ImagingIndependentOf360(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:lot28e_img_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)

	deny := gin.New()
	deny.Use(func(c *gin.Context) {
		rbac.SetUser(c, 4, "staff", []string{"patients.360.read", "patients:read"})
		c.Next()
	})
	RegisterRoutes(deny.Group("/api"), NewHandler(NewService(NewRepository(db))))
	w := httptest.NewRecorder()
	deny.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/patients/1/imaging-orders", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("360-only expected 403, got %d", w.Code)
	}

	allow := gin.New()
	allow.Use(func(c *gin.Context) {
		rbac.SetUser(c, 4, "staff", []string{"imaging.read"})
		c.Next()
	})
	RegisterRoutes(allow.Group("/api"), NewHandler(NewService(NewRepository(db))))
	w2 := httptest.NewRecorder()
	allow.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/patients/1/imaging-orders", nil))
	if w2.Code == http.StatusForbidden {
		t.Fatal("imaging.read must authorize patient imaging-orders")
	}
}
