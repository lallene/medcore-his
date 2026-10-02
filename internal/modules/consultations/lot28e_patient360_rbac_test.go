package consultations

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lot28eDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:lot28e_p360_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&patients.Patient{}, &Consultation{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func lot28eRouter(db *gorm.DB, perms []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 42, "staff", perms)
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutesWithHandler(api, NewHandler(NewService(NewRepository(db), nil)))
	return r
}

func seedLot28ePatientConsult(t *testing.T, db *gorm.DB) patients.Patient {
	t.Helper()
	p := patients.Patient{CodePatient: "P-28E-1", NumeroDossier: "D-28E-1", Nom: "Lot28E", Prenoms: "Test"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	c := Consultation{
		PatientID: p.ID, Status: ConsultationStatusCompleted, Service: "Médecine",
		DoctorName: "Dr Test", StartedAt: &now, CompletedAt: &now,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

// A13: legacy GET /patients/:id/360 is unregistered — no clinical aggregate under patients.360.read.
func TestLot28E_A13_Legacy360RouteRemoved(t *testing.T) {
	db := lot28eDB(t)
	p := seedLot28ePatientConsult(t, db)
	r := lot28eRouter(db, []string{"patients.360.read", "patients:read", "consultations.read"})

	req := httptest.NewRequest(http.MethodGet, "/api/patients/"+strconv.FormatUint(uint64(p.ID), 10)+"/360", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("legacy /360 must not succeed: status=%d body=%s", w.Code, w.Body.String())
	}
}

// A02/A03: patients.360.read without consultations.read cannot list consultations (no document inventory path).
func TestLot28E_A02_NoConsultationLeakWith360Only(t *testing.T) {
	db := lot28eDB(t)
	p := seedLot28ePatientConsult(t, db)
	r := lot28eRouter(db, []string{"patients.360.read", "patients:read", "laboratory.read"})

	req := httptest.NewRequest(http.MethodGet, "/api/patients/"+strconv.FormatUint(uint64(p.ID), 10)+"/consultations", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without consultations.read, got %d body=%s", w.Code, w.Body.String())
	}
}

// A04: consultations.read authorizes the canonical patient consultation route (RBAC middleware).
func TestLot28E_A04_ConsultationsReadStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 42, "staff", []string{"consultations.read"})
		c.Next()
	})
	r.GET("/api/patients/:id/consultations", rbac.Permission("consultations.read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/patients/1/consultations", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d", w.Code)
	}
}

// A14: ADMIN wildcard non-regression on consultations patient route permission.
func TestLot28E_A14_AdminWildcardConsultations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 1, "admin", []string{"*"})
		c.Next()
	})
	r.GET("/api/patients/:id/consultations", rbac.Permission("consultations.read"), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/patients/1/consultations", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("admin status=%d", w.Code)
	}
}
