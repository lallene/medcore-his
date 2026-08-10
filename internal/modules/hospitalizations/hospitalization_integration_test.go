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
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&patients.Patient{}, &medical_records.MedicalRecord{}, &consultations.Consultation{}, &medical_records.MedicalTimelineEvent{}, &Hospitalization{}, &Room{}, &Bed{}, &BedAssignment{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX ux_test_active_bed ON hospitalization_bed_assignments (bed_id) WHERE released_at IS NULL AND deleted_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX ux_test_active_stay ON hospitalization_bed_assignments (hospitalization_id) WHERE released_at IS NULL AND deleted_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func seedRoomAndBeds(t *testing.T, db *gorm.DB) (Room, Bed, Bed) {
	t.Helper()
	room := Room{Code: "R-" + fmt.Sprint(time.Now().UnixNano()), Name: "Chambre test", Department: "Médecine", Floor: "1", RoomType: "STANDARD", IsActive: true}
	if err := db.Create(&room).Error; err != nil {
		t.Fatal(err)
	}
	first := Bed{RoomID: room.ID, Code: room.Code + "-A", Label: "Lit A", BedType: "STANDARD", Status: BedAvailable, IsActive: true}
	second := Bed{RoomID: room.ID, Code: room.Code + "-B", Label: "Lit B", BedType: "STANDARD", Status: BedAvailable, IsActive: true}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	return room, first, second
}

func TestBedReservationAdmissionTransferReleaseAndTimeline(t *testing.T) {
	db := hospitalizationDB(t)
	f := seedHospitalization(t, db, "BED-LIFE", true)
	_, first, second := seedRoomAndBeds(t, db)
	service := NewService(db, NewRepository(db))
	stay, _, err := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 40)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := service.AssignBed(stay.ID, first.ID, 41)
	if err != nil || reservation.AssignmentType != AssignmentReserved || reservation.CreatedBy == nil || *reservation.CreatedBy != 41 {
		t.Fatalf("réservation: %#v %v", reservation, err)
	}
	if bed, _ := service.FindBed(first.ID); bed.Status != BedReserved {
		t.Fatalf("statut après réservation=%s", bed.Status)
	}
	if _, err = service.Admit(stay.ID, AdmitRequest{}, 42); err != nil {
		t.Fatal(err)
	}
	items, _ := service.ListAssignments(stay.ID)
	if len(items) != 1 || items[0].AssignmentType != AssignmentOccupied || items[0].UpdatedBy == nil || *items[0].UpdatedBy != 42 {
		t.Fatalf("conversion réservation: %#v", items)
	}
	transferred, err := service.TransferBed(stay.ID, second.ID, 43)
	if err != nil || transferred.BedID != second.ID {
		t.Fatalf("transfert: %#v %v", transferred, err)
	}
	items, _ = service.ListAssignments(stay.ID)
	if len(items) != 2 || items[1].ReleasedAt == nil {
		t.Fatalf("historique transfert perdu: %#v", items)
	}
	released, err := service.ReleaseBed(stay.ID, 44)
	if err != nil || released.ReleasedAt == nil || released.UpdatedBy == nil || *released.UpdatedBy != 44 {
		t.Fatalf("libération: %#v %v", released, err)
	}
	if bed, _ := service.FindBed(second.ID); bed.Status != BedAvailable {
		t.Fatalf("lit non disponible: %s", bed.Status)
	}
	var eventTypes []string
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_id=?", stay.ID).Order("id").Pluck("event_type", &eventTypes)
	for _, expected := range []string{"bed_reserved", "bed_assigned", "bed_transferred", "bed_released"} {
		found := false
		for _, actual := range eventTypes {
			if actual == expected {
				found = true
			}
		}
		if !found {
			t.Fatalf("événement %s absent: %#v", expected, eventTypes)
		}
	}
}

func TestBedIntegrityRulesAndAutomaticRelease(t *testing.T) {
	db := hospitalizationDB(t)
	service := NewService(db, NewRepository(db))
	f1 := seedHospitalization(t, db, "BED-I1", true)
	f2 := seedHospitalization(t, db, "BED-I2", true)
	room, first, second := seedRoomAndBeds(t, db)
	s1, _, _ := service.Create(CreateRequest{PatientID: f1.patient.ID, SourceConsultationID: f1.consultation.ID}, 50)
	s2, _, _ := service.Create(CreateRequest{PatientID: f2.patient.ID, SourceConsultationID: f2.consultation.ID}, 50)
	if _, err := service.AssignBed(s1.ID, first.ID, 51); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssignBed(s2.ID, first.ID, 52); err == nil {
		t.Fatal("double affectation du lit acceptée")
	}
	if _, err := service.AssignBed(s1.ID, second.ID, 52); err == nil {
		t.Fatal("double affectation du séjour acceptée")
	}
	out := BedOutOfService
	if _, err := service.UpdateBed(second.ID, UpdateBedRequest{Status: &out}, 53); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AssignBed(s2.ID, second.ID, 54); err == nil {
		t.Fatal("lit hors service accepté")
	}
	inactive := false
	if _, err := service.UpdateRoom(room.ID, UpdateRoomRequest{IsActive: &inactive}, 55); err == nil {
		t.Fatal("chambre avec réservation désactivée")
	}
	if _, err := service.Cancel(s1.ID, 56); err != nil {
		t.Fatal(err)
	}
	if bed, _ := service.FindBed(first.ID); bed.Status != BedAvailable {
		t.Fatalf("annulation sans libération: %s", bed.Status)
	}
	if _, err := service.AssignBed(s1.ID, first.ID, 57); err == nil {
		t.Fatal("séjour annulé réaffecté")
	}
}

func TestDischargeReleasesOccupiedBedAndAdmissionWithoutBedIsAllowed(t *testing.T) {
	db := hospitalizationDB(t)
	service := NewService(db, NewRepository(db))
	f := seedHospitalization(t, db, "BED-D", true)
	_, bed, _ := seedRoomAndBeds(t, db)
	stay, _, _ := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 60)
	if _, err := service.Admit(stay.ID, AdmitRequest{}, 61); err != nil {
		t.Fatalf("admission sans lit refusée: %v", err)
	}
	if _, err := service.AssignBed(stay.ID, bed.ID, 62); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Discharge(stay.ID, DischargeRequest{DischargeDiagnosis: "OK", DischargeSummary: "Sortie"}, 63); err != nil {
		t.Fatal(err)
	}
	items, _ := service.ListAssignments(stay.ID)
	if len(items) != 1 || items[0].ReleasedAt == nil {
		t.Fatalf("sortie sans libération: %#v", items)
	}
	if actual, _ := service.FindBed(bed.ID); actual.Status != BedAvailable {
		t.Fatalf("statut=%s", actual.Status)
	}
	if _, err := service.AssignBed(stay.ID, bed.ID, 64); err == nil {
		t.Fatal("séjour sorti réaffecté")
	}
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
