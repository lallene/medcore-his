package consultations

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
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// consultationServiceScopeDB opens an ephemeral schema for F24-01 characterization.
func consultationServiceScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL consultations ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("consultation_scope_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&patients.Patient{},
		&Consultation{},
		&ConsultationVitals{},
		&ConsultationReason{},
		&MedicalExam{},
		&ConsultationPrescription{},
		&ConsultationAntecedent{},
		&PhysicalExamArea{},
		&ConsultationPhysicalExam{},
		&ConsultationAdministeredTreatment{},
		&ConsultationPreviousMedication{},
		&ConsultationSurgicalHistory{},
		&ConsultationGynecoObstetricHistory{},
		&ConsultationSOAP{},
		&ConsultationSpecialtyData{},
	); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS patient_queue_tickets (
		id BIGSERIAL PRIMARY KEY, consultation_id BIGINT
	)`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func seedOrgAndStaffIsolationTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS organization_departments (
			id BIGSERIAL PRIMARY KEY, code TEXT, name TEXT, active BOOLEAN NOT NULL DEFAULT true,
			created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS organization_services (
			id BIGSERIAL PRIMARY KEY, department_id BIGINT NOT NULL DEFAULT 1,
			name TEXT, code TEXT, service_type TEXT NOT NULL DEFAULT 'CLINICAL',
			active BOOLEAN NOT NULL DEFAULT true, clinical BOOLEAN NOT NULL DEFAULT false,
			supports_hospitalization BOOLEAN NOT NULL DEFAULT false,
			supports_consultation BOOLEAN NOT NULL DEFAULT false,
			supports_beds BOOLEAN NOT NULL DEFAULT false,
			sort_order INT NOT NULL DEFAULT 0,
			created_by BIGINT NOT NULL DEFAULT 1, updated_by BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE IF NOT EXISTS staff_profiles (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL UNIQUE, active BOOLEAN NOT NULL DEFAULT true,
			primary_service_id BIGINT, employee_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS staff_service_assignments (
			id BIGSERIAL PRIMARY KEY, profile_id BIGINT NOT NULL, service_id BIGINT NOT NULL,
			active BOOLEAN NOT NULL DEFAULT true, is_primary BOOLEAN NOT NULL DEFAULT false,
			created_by BIGINT NOT NULL DEFAULT 1
		)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO organization_departments(id, code, name, active, created_by, updated_by) VALUES
		(1, 'CLIN', 'Clinique', true, 1, 1)
		ON CONFLICT DO NOTHING`).Error; err != nil {
		t.Fatal(err)
	}
}

func staffAccess(userID uint, perms ...string) Access {
	a := Access{UserID: userID, Permissions: map[string]bool{}}
	for _, p := range perms {
		a.Permissions[p] = true
	}
	return a
}

func starAccess(userID uint) Access {
	return Access{UserID: userID, Permissions: map[string]bool{"*": true}}
}

type f2401Fixture struct {
	db                        *gorm.DB
	svc                       *Service
	patient                   patients.Patient
	medID, genID              uint
	userA, userB, userStar    uint
	accessA, accessB, accessS Access
	medConsult                Consultation
	genConsult                Consultation
	nilSvcConsult             Consultation
}

func seedF2401Fixture(t *testing.T) f2401Fixture {
	t.Helper()
	db := consultationServiceScopeDB(t)
	seedOrgAndStaffIsolationTables(t, db)

	const medID, genID uint = 10, 11
	const userA, userB, userStar uint = 501, 502, 503
	if err := db.Exec(`INSERT INTO organization_services(id, department_id, name, code, service_type, active, supports_consultation, created_by, updated_by) VALUES
		(?, 1, 'Médecine', 'MED', 'CLINICAL', true, true, 1, 1),
		(?, 1, 'Médecine générale', 'GEN', 'CLINICAL', true, true, 1, 1)`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users(id, name, email, password_hash, role, is_active) VALUES
		(?, 'Dr MED', 'f2401-med@test.local', 'x', 'doctor', true),
		(?, 'Dr GEN', 'f2401-gen@test.local', 'x', 'doctor', true),
		(?, 'Admin *', 'f2401-star@test.local', 'x', 'admin', true)`, userA, userB, userStar).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES
		(1, ?, true, ?, 'MED-A'),
		(2, ?, true, ?, 'GEN-B')`, userA, medID, userB, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES
		(1, ?, true, 1),
		(2, ?, true, 1)`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}

	p := patients.Patient{CodePatient: "F2401-P", NumeroDossier: "F2401-D", Nom: "Scope", Prenoms: "Test"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}

	medSID, genSID := medID, genID
	docA, docB := userA, userB
	med := Consultation{
		PatientID: p.ID, DoctorName: "Dr MED", DoctorUserID: &docA,
		Service: "Médecine", ServiceID: &medSID, Status: ConsultationStatusDraft, Diagnosis: "MED scope",
	}
	gen := Consultation{
		PatientID: p.ID, DoctorName: "Dr GEN", DoctorUserID: &docB,
		Service: "Médecine générale", ServiceID: &genSID, Status: ConsultationStatusDraft, Diagnosis: "GEN scope",
	}
	nilSvc := Consultation{
		PatientID: p.ID, DoctorName: "Dr Orphan", DoctorUserID: &docA,
		Service: "Sans service", Status: ConsultationStatusDraft, Diagnosis: "nil ServiceID",
	}
	if err := db.Create(&med).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&gen).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&nilSvc).Error; err != nil {
		t.Fatal(err)
	}

	return f2401Fixture{
		db: db, svc: NewService(NewRepository(db), nil), patient: p,
		medID: medID, genID: genID, userA: userA, userB: userB, userStar: userStar,
		accessA:    staffAccess(userA, "consultations.read", "consultations.update"),
		accessB:    staffAccess(userB, "consultations.read", "consultations.update"),
		accessS:    starAccess(userStar),
		medConsult: med, genConsult: gen, nilSvcConsult: nilSvc,
	}
}

func assertScopeDenied(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want NotFound/anti-enumeration denial for out-of-scope Access, got nil", what)
	}
	if !errors.Is(err, ErrConsultationNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("%s: want NotFound semantics, got %v", what, err)
	}
}

func TestPostgresConsultationServiceScopeAssignedCanGetF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	got, err := f.svc.GetConsultation(f.medConsult.ID, f.accessA)
	if err != nil {
		t.Fatalf("MED-assigned actor Get MED consultation: %v", err)
	}
	if got.ServiceID == nil || *got.ServiceID != f.medID {
		t.Fatalf("ServiceID want MED=%d got %v", f.medID, got.ServiceID)
	}
}

func TestPostgresConsultationServiceScopeGetDeniesCrossServiceF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	_, err := f.svc.GetConsultation(f.medConsult.ID, f.accessB)
	assertScopeDenied(t, err, "GetConsultation GEN→MED")
}

func TestPostgresConsultationServiceScopeUpdateDeniesCrossServiceF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	before := "MED scope"
	intrusion := "cross-service mutation"
	_, err := f.svc.UpdateConsultation(f.medConsult.ID, UpdateConsultationRequest{
		ExpectedVersion: 1,
		Diagnosis:       &intrusion,
	}, f.userB, f.accessB)
	assertScopeDenied(t, err, "UpdateConsultation GEN→MED")
	var diagnosis string
	if err := f.db.Raw(`SELECT diagnosis FROM consultations WHERE id=?`, f.medConsult.ID).Scan(&diagnosis).Error; err != nil {
		t.Fatal(err)
	}
	if diagnosis != before {
		t.Fatalf("diagnosis must remain %q after denied Update, got %q", before, diagnosis)
	}
}

func TestPostgresConsultationServiceScopeUpdateStatusDeniesCrossServiceF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	_, err := f.svc.UpdateStatus(f.medConsult.ID, UpdateConsultationStatusRequest{
		Status: ConsultationStatusInProgress,
	}, f.userB, f.accessB)
	assertScopeDenied(t, err, "UpdateStatus GEN→MED")
	var status string
	if err := f.db.Raw(`SELECT status FROM consultations WHERE id=?`, f.medConsult.ID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != ConsultationStatusDraft {
		t.Fatalf("status must remain draft after denied UpdateStatus, got %s", status)
	}
}

func TestPostgresConsultationServiceScopeListServerAuthoritativeF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	list, err := f.svc.ListConsultations(ConsultationListFilter{Page: 1, Limit: 50}, f.accessB)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Data {
		if item.ServiceID != nil && *item.ServiceID == f.medID {
			t.Fatalf("ListConsultations for GEN Access exposed MED consultation id=%d among %d rows", item.ID, len(list.Data))
		}
	}
	var sawGEN bool
	for _, item := range list.Data {
		if item.ServiceID != nil && *item.ServiceID == f.genID {
			sawGEN = true
		}
	}
	if !sawGEN {
		t.Fatalf("ListConsultations for GEN Access missing GEN consultation: %#v", list.Data)
	}
}

func TestPostgresConsultationServiceScopeListClientServiceIDCannotWidenF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	medFilter := f.medID
	list, err := f.svc.ListConsultations(ConsultationListFilter{Page: 1, Limit: 50, ServiceID: &medFilter}, f.accessB)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 0 {
		t.Fatalf("client serviceId=MED must not widen GEN Access ListConsultations; got %d rows", len(list.Data))
	}
}

func TestPostgresConsultationServiceScopePatientListScopedF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	rows, err := f.svc.GetPatientConsultations(f.patient.ID, f.accessB)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rows {
		if c.ServiceID != nil && *c.ServiceID == f.medID {
			t.Fatalf("GetPatientConsultations for GEN Access exposed MED consultation among %d rows", len(rows))
		}
	}
}

func TestPostgresConsultationServiceScopeSOAPAndSpecialtyDenyCrossServiceF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	if _, err := f.svc.UpsertSOAP(f.medConsult.ID, UpsertConsultationSOAPRequest{
		ChiefComplaint: "céphalée", PrimaryDiagnosis: "migraine",
	}, f.userA, f.accessA); err != nil {
		t.Fatalf("seed SOAP: %v", err)
	}
	if _, err := f.svc.UpsertSpecialtyData(f.medConsult.ID, UpsertConsultationSpecialtyRequest{
		SpecialtyCode: "CARDIOLOGY", Data: map[string]any{"note": "seed"},
	}, f.userA, f.accessA); err != nil {
		t.Fatalf("seed specialty: %v", err)
	}

	_, getSOAPErr := f.svc.GetSOAP(f.medConsult.ID, f.accessB)
	_, upsertSOAPErr := f.svc.UpsertSOAP(f.medConsult.ID, UpsertConsultationSOAPRequest{
		ChiefComplaint: "intrusion",
	}, f.userB, f.accessB)
	_, getSpecErr := f.svc.GetSpecialtyData(f.medConsult.ID, f.accessB)
	_, upsertSpecErr := f.svc.UpsertSpecialtyData(f.medConsult.ID, UpsertConsultationSpecialtyRequest{
		SpecialtyCode: "CARDIOLOGY", Data: map[string]any{"note": "intrusion"},
	}, f.userB, f.accessB)

	assertScopeDenied(t, getSOAPErr, "GetSOAP GEN→MED")
	assertScopeDenied(t, upsertSOAPErr, "UpsertSOAP GEN→MED")
	assertScopeDenied(t, getSpecErr, "GetSpecialtyData GEN→MED")
	assertScopeDenied(t, upsertSpecErr, "UpsertSpecialtyData GEN→MED")
}

func TestPostgresConsultationServiceScopeNilServiceIDFailClosedF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	_, err := f.svc.GetConsultation(f.nilSvcConsult.ID, f.accessA)
	assertScopeDenied(t, err, "GetConsultation nil ServiceID with MED Access")
}

func TestPostgresConsultationServiceScopeStarBypassF2401(t *testing.T) {
	f := seedF2401Fixture(t)
	var n int64
	_ = f.db.Raw(`SELECT COUNT(1) FROM staff_service_assignments ssa
		JOIN staff_profiles sp ON sp.id=ssa.profile_id WHERE sp.user_id=?`, f.userStar).Scan(&n)
	if n != 0 {
		t.Fatalf("userStar must have no staff assignments, got %d", n)
	}
	got, err := f.svc.GetConsultation(f.medConsult.ID, f.accessS)
	if err != nil || got == nil {
		t.Fatalf("* Access must read MED consultation: %#v %v", got, err)
	}
}

// TestHTTPSOAPSpecialtyCrossServiceReturns404F2401 proves anti-enumeration at HTTP:
// an actor assigned to another service must get 404 (not 403/500) for SOAP/Specialty.
func TestHTTPSOAPSpecialtyCrossServiceReturns404F2401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := seedF2401Fixture(t)
	if _, err := f.svc.UpsertSOAP(f.medConsult.ID, UpsertConsultationSOAPRequest{
		ChiefComplaint: "céphalée", PrimaryDiagnosis: "migraine",
	}, f.userA, f.accessA); err != nil {
		t.Fatalf("seed SOAP: %v", err)
	}
	if _, err := f.svc.UpsertSpecialtyData(f.medConsult.ID, UpsertConsultationSpecialtyRequest{
		SpecialtyCode: "CARDIOLOGY", Data: map[string]any{"note": "seed"},
	}, f.userA, f.accessA); err != nil {
		t.Fatalf("seed specialty: %v", err)
	}

	handler := NewHandler(f.svc)
	id := fmt.Sprintf("%d", f.medConsult.ID)
	perms := []string{"consultations.read", "consultations.update"}

	assertHTTP404 := func(t *testing.T, name string, method, path string, body []byte, call func(*gin.Context)) {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		ctx.Request = req
		ctx.Params = gin.Params{{Key: "id", Value: id}}
		ctx.Set(rbac.ContextUserID, f.userB)
		ctx.Set(rbac.ContextPermissions, perms)
		call(ctx)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404 anti-enumeration, got %d body=%s", name, recorder.Code, recorder.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatalf("%s: invalid JSON: %v body=%s", name, err, recorder.Body.String())
		}
		if resp["error"] != ErrConsultationNotFound.Error() {
			t.Fatalf("%s: error want %q got %#v", name, ErrConsultationNotFound.Error(), resp["error"])
		}
	}

	assertHTTP404(t, "GET SOAP", http.MethodGet, "/api/consultations/"+id+"/soap", nil, handler.GetSOAP)
	assertHTTP404(t, "PUT SOAP", http.MethodPut, "/api/consultations/"+id+"/soap",
		[]byte(`{"chiefComplaint":"intrusion"}`), handler.UpsertSOAP)
	assertHTTP404(t, "GET Specialty", http.MethodGet, "/api/consultations/"+id+"/specialty", nil, handler.GetSpecialtyData)
	assertHTTP404(t, "PUT Specialty", http.MethodPut, "/api/consultations/"+id+"/specialty",
		[]byte(`{"specialtyCode":"CARDIOLOGY","data":{"note":"intrusion"}}`), handler.UpsertSpecialtyData)
}
