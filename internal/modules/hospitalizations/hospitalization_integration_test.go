package hospitalizations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func hospitalizationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL hospitalisation ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connexion PostgreSQL: %v", err)
	}
	schemaName := fmt.Sprintf("hospitalizations_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: `"` + schemaName + `.`, SingularTable: false}})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&patients.Patient{}, &medical_records.MedicalRecord{}, &consultations.Consultation{}, &medical_records.MedicalTimelineEvent{}, &Hospitalization{}); err != nil {
		t.Fatal(err)
	}
	return db
}

type hospitalizationFixture struct {
	patient      patients.Patient
	record       medical_records.MedicalRecord
	consultation consultations.Consultation
}

func seedHospitalization(t *testing.T, db *gorm.DB, suffix string, recommended bool) hospitalizationFixture {
	t.Helper()
	f := hospitalizationFixture{patient: patients.Patient{CodePatient: "HOSP-P-" + suffix, NumeroDossier: "HOSP-D-" + suffix, Nom: "Patient", Prenoms: suffix}}
	if err := db.Create(&f.patient).Error; err != nil {
		t.Fatal(err)
	}
	f.record = medical_records.MedicalRecord{PatientID: f.patient.ID, RecordNumber: "HOSP-MR-" + suffix, Status: "active"}
	if err := db.Create(&f.record).Error; err != nil {
		t.Fatal(err)
	}
	f.consultation = consultations.Consultation{PatientID: f.patient.ID, DoctorName: "Dr Test", Service: "Médecine", Status: consultations.ConsultationStatusCompleted, HospitalizationRequired: recommended, HospitalizationReason: "Surveillance", HospitalizationType: "medicale", HospitalizationDuration: 3}
	if err := db.Create(&f.consultation).Error; err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCreateIsIdempotentAndUsesAuthenticatedAuthor(t *testing.T) {
	db := hospitalizationDB(t)
	f := seedHospitalization(t, db, "CREATE", true)
	service := NewService(db, NewRepository(db))
	item, created, err := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID, AdmissionDiagnosis: "Diagnostic"}, 42)
	if err != nil || !created {
		t.Fatalf("création: created=%v err=%v", created, err)
	}
	if item.Status != StatusPlanned || item.CreatedBy == nil || *item.CreatedBy != 42 || item.UpdatedBy == nil || *item.UpdatedBy != 42 {
		t.Fatalf("auteurs/statut incorrects: %#v", item)
	}
	duplicate, created, err := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 999)
	if err != nil || created || duplicate.ID != item.ID {
		t.Fatalf("idempotence: item=%v created=%v err=%v", duplicate.ID, created, err)
	}
	var count int64
	db.Model(&Hospitalization{}).Where("source_consultation_id = ?", f.consultation.ID).Count(&count)
	if count != 1 {
		t.Fatalf("doublons=%d", count)
	}
	var event medical_records.MedicalTimelineEvent
	if err := db.Where("reference_type = ? AND reference_id = ? AND event_type = ?", "hospitalization", item.ID, "hospitalization_created").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.CreatedBy != 42 || event.MedicalRecordID != f.record.ID || event.PatientID != f.patient.ID {
		t.Fatalf("timeline incorrecte: %#v", event)
	}
}

func TestCreateHandlerIgnoresClientAuthorAndUsesJWTContext(t *testing.T) {
	db := hospitalizationDB(t)
	f := seedHospitalization(t, db, "JWT", true)
	handler := NewHandler(NewService(db, NewRepository(db)))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/hospitalizations", func(c *gin.Context) {
		rbac.SetUser(c, 77, "admin", []string{"*"})
		c.Next()
	}, handler.Create)
	body, _ := json.Marshal(map[string]any{"patientId": f.patient.ID, "sourceConsultationId": f.consultation.ID, "createdBy": 999, "updatedBy": 999})
	request := httptest.NewRequest(http.MethodPost, "/hospitalizations", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var item Hospitalization
	if err := db.Where("source_consultation_id = ?", f.consultation.ID).First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.CreatedBy == nil || *item.CreatedBy != 77 || item.UpdatedBy == nil || *item.UpdatedBy != 77 {
		t.Fatalf("usurpation auteur acceptée: %#v", item)
	}
}

func TestLifecycleTransitionsAndTimeline(t *testing.T) {
	db := hospitalizationDB(t)
	f := seedHospitalization(t, db, "LIFE", true)
	service := NewService(db, NewRepository(db))
	item, _, _ := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 10)
	admitted, err := service.Admit(item.ID, AdmitRequest{AdmissionDiagnosis: "Admission"}, 11)
	if err != nil || admitted.Status != StatusAdmitted || admitted.UpdatedBy == nil || *admitted.UpdatedBy != 11 {
		t.Fatalf("admission: %#v %v", admitted, err)
	}
	if _, err := service.Admit(item.ID, AdmitRequest{}, 12); err == nil {
		t.Fatal("double admission acceptée")
	}
	discharged, err := service.Discharge(item.ID, DischargeRequest{DischargeDiagnosis: "Guéri", DischargeSummary: "Retour domicile"}, 13)
	if err != nil || discharged.Status != StatusDischarged || discharged.DischargedAt == nil {
		t.Fatalf("sortie: %#v %v", discharged, err)
	}
	if _, err := service.Cancel(item.ID, 14); err == nil {
		t.Fatal("annulation après sortie acceptée")
	}
	var events []medical_records.MedicalTimelineEvent
	db.Where("reference_type = ? AND reference_id = ?", "hospitalization", item.ID).Order("id").Find(&events)
	if len(events) != 3 || events[1].EventType != "hospitalization_admitted" || events[2].EventType != "hospitalization_discharged" || events[2].CreatedBy != 13 {
		t.Fatalf("timeline cycle: %#v", events)
	}
}

func TestCancellationValidationAndFilters(t *testing.T) {
	db := hospitalizationDB(t)
	service := NewService(db, NewRepository(db))
	planned := seedHospitalization(t, db, "CANCEL", true)
	noDecision := seedHospitalization(t, db, "NO", false)
	item, _, _ := service.Create(CreateRequest{PatientID: planned.patient.ID, SourceConsultationID: planned.consultation.ID}, 20)
	cancelled, err := service.Cancel(item.ID, 21)
	if err != nil || cancelled.Status != StatusCancelled || cancelled.UpdatedBy == nil || *cancelled.UpdatedBy != 21 {
		t.Fatalf("annulation: %#v %v", cancelled, err)
	}
	var cancelledEvent medical_records.MedicalTimelineEvent
	if err := db.Where("reference_id = ? AND event_type = ?", item.ID, "hospitalization_cancelled").First(&cancelledEvent).Error; err != nil || cancelledEvent.CreatedBy != 21 {
		t.Fatalf("timeline annulation: %#v %v", cancelledEvent, err)
	}
	if _, _, err := service.Create(CreateRequest{PatientID: noDecision.patient.ID, SourceConsultationID: noDecision.consultation.ID}, 20); err == nil {
		t.Fatal("création sans décision acceptée")
	}
	if _, _, err := service.Create(CreateRequest{PatientID: 999999, SourceConsultationID: 999999}, 20); err == nil {
		t.Fatal("patient inexistant accepté")
	}
	if _, _, err := service.Create(CreateRequest{PatientID: noDecision.patient.ID, SourceConsultationID: 999999}, 20); err == nil {
		t.Fatal("consultation inexistante acceptée")
	}
	if _, _, err := service.Create(CreateRequest{PatientID: noDecision.patient.ID, SourceConsultationID: planned.consultation.ID}, 20); err == nil {
		t.Fatal("consultation d'un autre patient acceptée")
	}
	status := StatusCancelled
	result, err := service.List(ListFilter{Page: 1, Limit: 20, PatientID: &planned.patient.ID, Status: status, Department: "médecine"})
	if err != nil || result.Total != 1 || len(result.Data) != 1 {
		t.Fatalf("filtres: %#v %v", result, err)
	}
}

func TestTimelineFailureRollsBackHospitalization(t *testing.T) {
	db := hospitalizationDB(t)
	f := seedHospitalization(t, db, "ROLLBACK", true)
	service := NewService(db, NewRepository(db))
	if err := db.Migrator().DropTable(&medical_records.MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	_, _, err := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 30)
	if err == nil {
		t.Fatal("échec timeline attendu")
	}
	var count int64
	if err := db.Model(&Hospitalization{}).Where("source_consultation_id = ?", f.consultation.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rollback incomplet: %d hospitalisation", count)
	}
}

func TestAppErrorsRemainClassifiable(t *testing.T) {
	err := coreerrors.Conflict("test")
	var appErr *coreerrors.AppError
	if !errors.As(err, &appErr) || appErr.Status != 409 {
		t.Fatal("erreur conflit non classifiable")
	}
}
