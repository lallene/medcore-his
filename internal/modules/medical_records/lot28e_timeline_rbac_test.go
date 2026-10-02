package medical_records

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

// A05: medical_records.read remains required for timeline.
func TestLot28E_A05_TimelineRequiresMedicalRecordsRead(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:lot28e_mr_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&MedicalRecord{}, &MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 7, "staff", []string{"patients.360.read", "patients:read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/medical-records/1/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without medical_records.read, got %d", w.Code)
	}

	r2 := gin.New()
	r2.Use(func(c *gin.Context) {
		rbac.SetUser(c, 7, "staff", []string{"medical_records.read"})
		c.Next()
	})
	api2 := r2.Group("/api")
	RegisterRoutes(api2, NewHandler(NewService(NewRepository(db))))
	req2 := httptest.NewRequest(http.MethodGet, "/api/medical-records/1/timeline", nil)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	// Permission passes; missing record may be 404/200 empty — must not be 403.
	if w2.Code == http.StatusForbidden {
		t.Fatalf("medical_records.read must authorize timeline route, got 403")
	}
}
