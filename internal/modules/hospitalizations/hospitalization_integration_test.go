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

func TestRoomAdministrationRulesAndCounts(t *testing.T) {
	db := hospitalizationDB(t)
	service := NewService(db, NewRepository(db))
	room, err := service.CreateRoom(CreateRoomRequest{Code: " ADMIN-R1 ", Name: " Chambre 1 ", Department: " Médecine ", RoomType: " STANDARD "}, 70)
	if err != nil || room.Code != "ADMIN-R1" || room.CreatedBy == nil || *room.CreatedBy != 70 {
		t.Fatalf("création chambre: %#v %v", room, err)
	}
	if _, err := service.CreateRoom(CreateRoomRequest{Code: "ADMIN-R1", Name: "Doublon", Department: "Médecine", RoomType: "STANDARD"}, 70); err == nil {
		t.Fatal("code chambre dupliqué accepté")
	}
	if _, err := service.CreateRoom(CreateRoomRequest{Code: "   ", Name: "Vide", Department: "Médecine", RoomType: "STANDARD"}, 70); err == nil {
		t.Fatal("code chambre vide accepté")
	}
	newCode, newName := "ADMIN-R1B", "Chambre renommée"
	room, err = service.UpdateRoom(room.ID, UpdateRoomRequest{Code: &newCode, Name: &newName}, 71)
	if err != nil || room.Code != newCode || room.Name != newName || room.UpdatedBy == nil || *room.UpdatedBy != 71 {
		t.Fatalf("édition chambre: %#v %v", room, err)
	}
	bed, err := service.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "ADMIN-B1", Label: "Lit 1", BedType: "STANDARD"}, 72)
	if err != nil {
		t.Fatal(err)
	}
	rooms, err := service.ListRooms()
	if err != nil {
		t.Fatal(err)
	}
	var found *Room
	for i := range rooms {
		if rooms[i].ID == room.ID {
			found = &rooms[i]
		}
	}
	if found == nil || found.BedCount != 1 || found.AvailableBedCount != 1 {
		t.Fatalf("compteurs chambre: %#v", found)
	}
	inactive := false
	if _, err := service.UpdateRoom(room.ID, UpdateRoomRequest{IsActive: &inactive}, 73); err != nil {
		t.Fatalf("désactivation chambre vide: %v", err)
	}
	active := true
	if _, err := service.UpdateRoom(room.ID, UpdateRoomRequest{IsActive: &active}, 73); err != nil {
		t.Fatal(err)
	}
	f := seedHospitalization(t, db, "ADMIN-ROOM", true)
	stay, _, _ := service.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 74)
	if _, err := service.AssignBed(stay.ID, bed.ID, 74); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateRoom(room.ID, UpdateRoomRequest{IsActive: &inactive}, 75); err == nil {
		t.Fatal("chambre réservée désactivée")
	}
}

func TestBedAdministrationSafeTransitions(t *testing.T) {
	db := hospitalizationDB(t)
	service := NewService(db, NewRepository(db))
	room, first, _ := seedRoomAndBeds(t, db)
	otherRoom, err := service.CreateRoom(CreateRoomRequest{Code: "ADMIN-R2", Name: "Autre chambre", Department: "Médecine", RoomType: "STANDARD"}, 80)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateBed(CreateBedRequest{RoomID: room.ID, Code: " ADMIN-CREATE ", Label: " Nouveau ", BedType: " STANDARD "}, 80)
	if err != nil || created.Code != "ADMIN-CREATE" {
		t.Fatalf("création lit: %#v %v", created, err)
	}
	if _, err := service.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "ADMIN-CREATE", Label: "Doublon", BedType: "STANDARD"}, 80); err == nil {
		t.Fatal("code lit dupliqué accepté")
	}
	if _, err := service.CreateBed(CreateBedRequest{RoomID: 999999, Code: "ADMIN-NO-ROOM", Label: "Lit", BedType: "STANDARD"}, 80); err == nil {
		t.Fatal("chambre inexistante acceptée")
	}
	newCode, newLabel := "ADMIN-EDIT", "Lit édité"
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{Code: &newCode, Label: &newLabel, RoomID: &otherRoom.ID}, 81); err != nil {
		t.Fatal(err)
	}
	inactive := false
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{IsActive: &inactive}, 82); err != nil {
		t.Fatalf("désactivation AVAILABLE: %v", err)
	}
	active := true
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{IsActive: &active}, 82); err != nil {
		t.Fatal(err)
	}
	off := BedOutOfService
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{Status: &off}, 83); err != nil {
		t.Fatal(err)
	}
	available := BedAvailable
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{Status: &available}, 84); err != nil {
		t.Fatal(err)
	}
	occupied, reserved := BedOccupied, BedReserved
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{Status: &occupied}, 85); err == nil {
		t.Fatal("AVAILABLE vers OCCUPIED accepté")
	}
	if _, err := service.UpdateBed(created.ID, UpdateBedRequest{Status: &reserved}, 85); err == nil {
		t.Fatal("AVAILABLE vers RESERVED accepté")
	}
	f1 := seedHospitalization(t, db, "ADMIN-BED1", true)
	s1, _, _ := service.Create(CreateRequest{PatientID: f1.patient.ID, SourceConsultationID: f1.consultation.ID}, 86)
	if _, err := service.AssignBed(s1.ID, first.ID, 86); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateBed(first.ID, UpdateBedRequest{IsActive: &inactive}, 87); err == nil {
		t.Fatal("lit réservé désactivé")
	}
	if _, err := service.UpdateBed(first.ID, UpdateBedRequest{RoomID: &otherRoom.ID}, 87); err == nil {
		t.Fatal("lit réservé déplacé")
	}
	if _, err := service.Admit(s1.ID, AdmitRequest{}, 88); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateBed(first.ID, UpdateBedRequest{Status: &off}, 89); err == nil {
		t.Fatal("lit occupé hors service")
	}
	if _, err := service.UpdateBed(first.ID, UpdateBedRequest{RoomID: &otherRoom.ID}, 89); err == nil {
		t.Fatal("lit occupé déplacé")
	}
}

func TestBedAdministrationHandlerUsesJWTAuthor(t *testing.T) {
	db := hospitalizationDB(t)
	_, _, _ = seedRoomAndBeds(t, db)
	handler := NewBedHandler(NewService(db, NewRepository(db)))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/rooms", func(c *gin.Context) { rbac.SetUser(c, 91, "admin", []string{"rooms.manage"}); c.Next() }, handler.CreateRoom)
	body := bytes.NewBufferString(`{"code":"JWT-ROOM","name":"JWT","department":"Test","roomType":"STANDARD","createdBy":999}`)
	request := httptest.NewRequest(http.MethodPost, "/rooms", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var room Room
	if err := db.Where("code=?", "JWT-ROOM").First(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.CreatedBy == nil || *room.CreatedBy != 91 {
		t.Fatalf("auteur client accepté: %#v", room)
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
