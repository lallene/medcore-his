package hospitalizations

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func hospServiceScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL hospitalisation ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("hosp_scope_%d", time.Now().UnixNano())
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
	if err := db.AutoMigrate(
		&patients.Patient{},
		&medical_records.MedicalRecord{},
		&consultations.Consultation{},
		&medical_records.MedicalTimelineEvent{},
		&Hospitalization{},
		&Room{},
		&Bed{},
		&BedAssignment{},
	); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY, name TEXT, email TEXT, password_hash TEXT, role TEXT, is_active BOOLEAN DEFAULT true
		)`,
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
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO organization_departments(id, code, name, active, created_by, updated_by) VALUES
		(1, 'CLIN', 'Clinique', true, 1, 1) ON CONFLICT DO NOTHING`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func hospStaffAccess(userID uint, perms ...string) Access {
	a := Access{UserID: userID, Permissions: map[string]bool{}}
	for _, p := range perms {
		a.Permissions[p] = true
	}
	return a
}

func isHospNotFound(err error) bool {
	var app *coreerrors.AppError
	return errors.As(err, &app) && app.Status == 404
}

type f28fHospFixture struct {
	db                 *gorm.DB
	svc                *Service
	medID, genID       uint
	userA, userB       uint
	accessA, accessB   Access
	patientA, patientB patients.Patient
	hospMedA, hospMedB Hospitalization
	hospGen            Hospitalization
}

func seedF28FHospFixture(t *testing.T) f28fHospFixture {
	t.Helper()
	db := hospServiceScopeDB(t)
	const medID, genID uint = 10, 11
	const userA, userB uint = 601, 602
	if err := db.Exec(`INSERT INTO organization_services(id, department_id, name, code, service_type, active, supports_hospitalization, created_by, updated_by) VALUES
		(?, 1, 'Médecine', 'MED', 'CLINICAL', true, true, 1, 1),
		(?, 1, 'Médecine générale', 'GEN', 'CLINICAL', true, true, 1, 1)`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users(id, name, email, password_hash, role, is_active) VALUES
		(?, 'Dr MED', 'f28f-med@test.local', 'x', 'doctor', true),
		(?, 'Dr GEN', 'f28f-gen@test.local', 'x', 'doctor', true)`, userA, userB).Error; err != nil {
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

	pa := patients.Patient{CodePatient: "F28F-PA", NumeroDossier: "F28F-DA", Nom: "Alpha", Prenoms: "Pat"}
	pb := patients.Patient{CodePatient: "F28F-PB", NumeroDossier: "F28F-DB", Nom: "Beta", Prenoms: "Pat"}
	if err := db.Create(&pa).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pb).Error; err != nil {
		t.Fatal(err)
	}
	mra := medical_records.MedicalRecord{PatientID: pa.ID, RecordNumber: "MR-F28F-A", Status: "active"}
	mrb := medical_records.MedicalRecord{PatientID: pb.ID, RecordNumber: "MR-F28F-B", Status: "active"}
	if err := db.Create(&mra).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mrb).Error; err != nil {
		t.Fatal(err)
	}

	medSID, genSID := medID, genID
	mkConsult := func(patientID uint, sid uint, name string) consultations.Consultation {
		c := consultations.Consultation{
			PatientID: patientID, DoctorName: "Dr", Service: name, ServiceID: &sid,
			Status: consultations.ConsultationStatusCompleted, HospitalizationRequired: true,
			HospitalizationReason: "Surveillance", HospitalizationType: "medicale",
		}
		if err := db.Create(&c).Error; err != nil {
			t.Fatal(err)
		}
		return c
	}
	cMedA := mkConsult(pa.ID, medSID, "Médecine")
	cMedB := mkConsult(pb.ID, medSID, "Médecine")
	cGen := mkConsult(pa.ID, genSID, "Médecine générale")

	mkHosp := func(patientID, mrID, consultID uint, sid uint, dept, num string) Hospitalization {
		h := Hospitalization{
			PatientID: patientID, MedicalRecordID: mrID, SourceConsultationID: consultID,
			AdmissionNumber: num, HospitalizationType: "medicale", AdmissionReason: "Surveillance",
			Department: dept, ServiceID: &sid, Status: StatusPlanned,
		}
		if err := db.Create(&h).Error; err != nil {
			t.Fatal(err)
		}
		return h
	}
	hospMedA := mkHosp(pa.ID, mra.ID, cMedA.ID, medSID, "Médecine", "HOSP-F28F-MED-A")
	hospMedB := mkHosp(pb.ID, mrb.ID, cMedB.ID, medSID, "Médecine", "HOSP-F28F-MED-B")
	hospGen := mkHosp(pa.ID, mra.ID, cGen.ID, genSID, "Médecine générale", "HOSP-F28F-GEN")

	return f28fHospFixture{
		db: db, svc: NewService(db, NewRepository(db)),
		medID: medID, genID: genID, userA: userA, userB: userB,
		accessA:  hospStaffAccess(userA, "hospitalizations.read", "hospitalizations.update", "hospitalizations.cancel"),
		accessB:  hospStaffAccess(userB, "hospitalizations.read", "hospitalizations.update", "hospitalizations.cancel"),
		patientA: pa, patientB: pb,
		hospMedA: hospMedA, hospMedB: hospMedB, hospGen: hospGen,
	}
}

// F101/F103 — same-service cross-patient read succeeds; cross-service read is 404.
func TestPostgresLOT28FHospitalizationSameServiceCrossPatientAndCrossService(t *testing.T) {
	f := seedF28FHospFixture(t)

	got, err := f.svc.FindByID(f.hospMedA.ID, f.accessA)
	if err != nil {
		t.Fatalf("A: Service A + Service A object: %v", err)
	}
	if got.ID != f.hospMedA.ID {
		t.Fatalf("A: wrong id %d", got.ID)
	}

	crossPatient, err := f.svc.FindByID(f.hospMedB.ID, f.accessA)
	if err != nil {
		t.Fatalf("B/F103: same-service cross-patient must succeed: %v", err)
	}
	if crossPatient.PatientID != f.patientB.ID {
		t.Fatalf("B: patient want %d got %d", f.patientB.ID, crossPatient.PatientID)
	}

	_, err = f.svc.FindByID(f.hospGen.ID, f.accessA)
	if !isHospNotFound(err) {
		t.Fatalf("C/F101: cross-service read want 404 got %v", err)
	}
	_, err = f.svc.FindByID(999999, f.accessA)
	if !isHospNotFound(err) {
		t.Fatalf("V: nonexistent want 404 got %v", err)
	}
}

// F102 — cross-service mutation rejected; object unchanged.
func TestPostgresLOT28FHospitalizationCrossServiceMutation(t *testing.T) {
	f := seedF28FHospFixture(t)
	before, err := f.svc.FindByID(f.hospMedA.ID, UnrestrictedAccess(1))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.Admit(f.hospMedA.ID, AdmitRequest{}, f.userB, f.accessB)
	if !isHospNotFound(err) {
		t.Fatalf("E/F102: cross-service Admit want 404 got %v", err)
	}
	after, err := f.svc.FindByID(f.hospMedA.ID, UnrestrictedAccess(1))
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status || after.Status != StatusPlanned {
		t.Fatalf("E: object mutated: before=%s after=%s", before.Status, after.Status)
	}

	_, err = f.svc.Cancel(f.hospMedA.ID, f.userB, f.accessB)
	if !isHospNotFound(err) {
		t.Fatalf("E: cross-service Cancel want 404 got %v", err)
	}
	after, _ = f.svc.FindByID(f.hospMedA.ID, UnrestrictedAccess(1))
	if after.Status != StatusPlanned {
		t.Fatalf("E: Cancel mutated status=%s", after.Status)
	}
}

// Same-service Admit succeeds (F103 mutation path).
func TestPostgresLOT28FHospitalizationSameServiceAdmit(t *testing.T) {
	f := seedF28FHospFixture(t)
	out, err := f.svc.Admit(f.hospMedB.ID, AdmitRequest{}, f.userA, f.accessA)
	if err != nil {
		t.Fatalf("same-service cross-patient Admit: %v", err)
	}
	if out.Status != StatusAdmitted {
		t.Fatalf("status=%s", out.Status)
	}
}

func TestPostgresLOT28FHospitalizationListServerAuthoritative(t *testing.T) {
	f := seedF28FHospFixture(t)
	list, err := f.svc.List(ListFilter{Page: 1, Limit: 50}, f.accessA)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[uint]bool{}
	for _, h := range list.Data {
		ids[h.ID] = true
		if h.ServiceID == nil || *h.ServiceID != f.medID {
			t.Fatalf("list leaked non-MED service: %#v", h)
		}
	}
	if !ids[f.hospMedA.ID] || !ids[f.hospMedB.ID] {
		t.Fatal("list missing same-service hospitalizations")
	}
	if ids[f.hospGen.ID] {
		t.Fatal("list revealed GEN hospitalization to MED actor")
	}
	// Client cannot widen via serviceId filter.
	genFilter := f.genID
	widened, err := f.svc.List(ListFilter{Page: 1, Limit: 50, ServiceID: &genFilter}, f.accessA)
	if err != nil {
		t.Fatal(err)
	}
	if len(widened.Data) != 0 {
		t.Fatalf("client serviceId widen leaked %d rows", len(widened.Data))
	}
}
