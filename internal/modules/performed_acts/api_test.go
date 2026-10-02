package performed_acts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func apiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:performed_acts_api_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &ProducerMap{},
		&billing.Invoice{}, &billing.InvoiceLine{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedPatientCatalog(t *testing.T, db *gorm.DB) (patients.Patient, act_catalog.Entry) {
	t.Helper()
	p := patients.Patient{CodePatient: "P-PA-1", NumeroDossier: "D-PA-1", Nom: "Test"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "CONS-CARD", Label: "Consultation cardio", Description: "CS", Category: "CONSULTATION",
		BasePrice: 25000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "MR-PA-1", Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return p, cat
}

func apiRouter(db *gorm.DB, perms []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 77, "staff", perms)
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(db)))
	return r
}

func TestAPIForbiddenWithoutPermission(t *testing.T) {
	db := apiDB(t)
	r := apiRouter(db, []string{"billing.read"})
	req := httptest.NewRequest(http.MethodGet, "/api/performed-acts", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAPIReadOnlyCannotCreateOrVoid(t *testing.T) {
	db := apiDB(t)
	p, cat := seedPatientCatalog(t, db)
	r := apiRouter(db, []string{"performed_acts.read"})

	body, _ := json.Marshal(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("create status=%d", w.Code)
	}

	s := NewService(db)
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	voidBody := []byte(`{"reason":"erreur"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/performed-acts/"+strconv.FormatUint(uint64(act.ID), 10)+"/void", bytes.NewReader(voidBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("void status=%d", w.Code)
	}
}

func TestAPICRUDVoidAndFilters(t *testing.T) {
	db := apiDB(t)
	p, cat := seedPatientCatalog(t, db)
	r := apiRouter(db, []string{"performed_acts.read", "performed_acts.create", "performed_acts.void"})

	body, _ := json.Marshal(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: 1})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created Act
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ActCode != "CONS-CARD" || created.BasePrice != 25000 || created.Status != StatusPerformed {
		t.Fatalf("created=%+v", created)
	}
	if created.PerformedBy != 77 {
		t.Fatalf("performer spoof risk: %d", created.PerformedBy)
	}

	// Client cannot override snapshot via extra JSON fields — server used catalogue.
	req = httptest.NewRequest(http.MethodGet, "/api/performed-acts/"+strconv.FormatUint(uint64(created.ID), 10), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/performed-acts?patientId="+strconv.FormatUint(uint64(p.ID), 10), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d", w.Code)
	}
	var page Page
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Total != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}

	// Invalid quantity
	bad, _ := json.Marshal(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID, Quantity: -1})
	req = httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("neg qty status=%d", w.Code)
	}

	voidBody := []byte(`{"reason":"saisie erronée"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/performed-acts/"+strconv.FormatUint(uint64(created.ID), 10)+"/void", bytes.NewReader(voidBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("void status=%d body=%s", w.Code, w.Body.String())
	}
	var voided Act
	_ = json.Unmarshal(w.Body.Bytes(), &voided)
	if voided.Status != StatusVoided || voided.ActCode != "CONS-CARD" {
		t.Fatalf("voided=%+v", voided)
	}

	// Still readable
	req = httptest.NewRequest(http.MethodGet, "/api/performed-acts/"+strconv.FormatUint(uint64(created.ID), 10), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get voided status=%d", w.Code)
	}

	// No DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/performed-acts/"+strconv.FormatUint(uint64(created.ID), 10), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK || w.Code == http.StatusNoContent {
		t.Fatal("DELETE must not succeed")
	}

	_ = time.Now()
}

func TestAPIInactiveCatalogRejected(t *testing.T) {
	db := apiDB(t)
	p, cat := seedPatientCatalog(t, db)
	db.Model(&cat).Update("is_active", false)
	r := apiRouter(db, []string{"performed_acts.create"})
	body, _ := json.Marshal(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIMissingPatient(t *testing.T) {
	db := apiDB(t)
	_, cat := seedPatientCatalog(t, db)
	r := apiRouter(db, []string{"performed_acts.create"})
	body, _ := json.Marshal(CreateRequest{PatientID: 999999, ActCatalogEntryID: cat.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestAPIProducerMapsRBACAndNoDelete(t *testing.T) {
	db := apiDB(t)
	_, cat := seedPatientCatalog(t, db)

	forbidden := apiRouter(db, []string{"performed_acts.create"})
	req := httptest.NewRequest(http.MethodGet, "/api/performed-acts/producer-maps", nil)
	w := httptest.NewRecorder()
	forbidden.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("read without perm status=%d", w.Code)
	}

	r := apiRouter(db, []string{"performed_acts.producer_map.read", "performed_acts.producer_map.manage"})
	body, _ := json.Marshal(UpsertProducerMapRequest{
		SourceType: SourceConsultation, ClinicalKey: "", ActCatalogEntryID: cat.ID,
	})
	req = httptest.NewRequest(http.MethodPost, "/api/performed-acts/producer-maps", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", w.Code, w.Body.String())
	}
	var created ProducerMap
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/performed-acts/producer-maps/readiness", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("readiness status=%d", w.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/performed-acts/producer-maps/"+strconv.FormatUint(uint64(created.ID), 10), nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK || w.Code == http.StatusNoContent {
		t.Fatal("DELETE producer-map must not succeed")
	}

	req = httptest.NewRequest(http.MethodPost, "/api/performed-acts/producer-maps/"+strconv.FormatUint(uint64(created.ID), 10)+"/deactivate", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("deactivate status=%d body=%s", w.Code, w.Body.String())
	}
}
