package authorization

import (
	"errors"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func authStaffAccess(userID uint, perms ...string) Access {
	a := Access{UserID: userID, Permissions: map[string]bool{}}
	for _, p := range perms {
		a.Permissions[p] = true
	}
	return a
}

func isAuthNotFound(err error) bool {
	var app *coreerrors.AppError
	return errors.As(err, &app) && app.Status == 404
}

func TestPostgresLOT28FInsuranceAuthorizationServiceScope(t *testing.T) {
	db := authorizationDB(t)
	const medID, genID uint = 10, 11
	const userA, userB uint = 801, 802

	for _, sql := range []string{
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
			active BOOLEAN NOT NULL DEFAULT true, created_by BIGINT NOT NULL DEFAULT 1
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
	if err := db.Exec(`INSERT INTO organization_services(id, department_id, name, code, service_type, active, created_by, updated_by) VALUES
		(?, 1, 'Médecine', 'MED', 'CLINICAL', true, 1, 1),
		(?, 1, 'Médecine générale', 'GEN', 'CLINICAL', true, 1, 1)
		ON CONFLICT DO NOTHING`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES
		(1, ?, true, ?, 'MED-A'), (2, ?, true, ?, 'GEN-B')`, userA, medID, userB, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES
		(1, ?, true, 1), (2, ?, true, 1)`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}

	f := seedAuthorization(t, db)
	medSID, genSID := medID, genID
	pB := patients.Patient{CodePatient: "PEC-F28F-B", NumeroDossier: "PEC-F28F-DB", Nom: "Beta", Prenoms: "Pat"}
	if err := db.Create(&pB).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&medical_records.MedicalRecord{PatientID: pB.ID, RecordNumber: "PEC-F28F-MRB", Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	covB := coverage.PatientCoverage{
		PatientID: pB.ID, CompanyID: f.coverage.CompanyID, GuarantorID: f.coverage.GuarantorID,
		MemberNumber: "PEC-F28F-B", CoverageRate: 80, IsPrincipal: true, IsActive: true,
	}
	if err := db.Create(&covB).Error; err != nil {
		t.Fatal(err)
	}

	cMedA := authorizationConsultation{PatientID: f.patient.ID, ServiceID: &medSID, Service: "Médecine", Status: "COMPLETED"}
	cMedB := authorizationConsultation{PatientID: pB.ID, ServiceID: &medSID, Service: "Médecine", Status: "COMPLETED"}
	cGen := authorizationConsultation{PatientID: f.patient.ID, ServiceID: &genSID, Service: "GEN", Status: "COMPLETED"}
	for _, c := range []*authorizationConsultation{&cMedA, &cMedB, &cGen} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}

	s := NewService(db)
	amount := 1000.0
	authMedA, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: cMedA.ID, RequestedAmount: &amount,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	if authMedA.ServiceID == nil || *authMedA.ServiceID != medID {
		t.Fatalf("Create must persist ServiceID from consultation: %#v", authMedA.ServiceID)
	}
	authMedB, err := s.Create(CreateRequest{
		PatientID: pB.ID, PatientCoverageID: covB.ID,
		ReferenceType: "CONSULTATION", ReferenceID: cMedB.ID, RequestedAmount: &amount,
	}, userA)
	if err != nil {
		t.Fatal(err)
	}
	authGen, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: cGen.ID, RequestedAmount: &amount,
	}, userB)
	if err != nil {
		t.Fatal(err)
	}
	if authGen.ServiceID == nil || *authGen.ServiceID != genID {
		t.Fatalf("GEN ServiceID: %#v", authGen.ServiceID)
	}

	accessA := authStaffAccess(userA, "insurance.authorization.read", "insurance.authorization.cancel")
	accessB := authStaffAccess(userB, "insurance.authorization.read", "insurance.authorization.cancel")

	if _, err := s.FindByIDForAccess(authMedA.ID, accessA); err != nil {
		t.Fatalf("A: %v", err)
	}
	if _, err := s.FindByIDForAccess(authMedB.ID, accessA); err != nil {
		t.Fatalf("B/F103: %v", err)
	}
	if _, err := s.FindByIDForAccess(authGen.ID, accessA); !isAuthNotFound(err) {
		t.Fatalf("C/F101: want 404 got %v", err)
	}
	_, err = s.Cancel(authMedA.ID, userB, accessB)
	if !isAuthNotFound(err) {
		t.Fatalf("E/F102: Cancel want 404 got %v", err)
	}
	still, err := s.FindByID(authMedA.ID)
	if err != nil || still.Status != StatusDraft {
		t.Fatalf("E: mutated status=%s err=%v", still.Status, err)
	}

	// NULL ServiceID: SERVICE_AUTHORITY_UNRESOLVED — do not invent deny.
	nullRow := InsuranceAuthorization{
		AuthorizationNumber: "PEC-NULL-SCOPE",
		PatientID:           f.patient.ID,
		MedicalRecordID:     still.MedicalRecordID,
		PatientCoverageID:   f.coverage.ID,
		InsuranceCompanyID:  f.coverage.CompanyID,
		GuarantorID:         f.coverage.GuarantorID,
		ReferenceType:       "CONSULTATION",
		ReferenceID:         cMedA.ID + 9000,
		Service:             "orphan",
		ServiceID:           nil,
		RequestedAmount:     &amount,
		RequestedAt:         time.Now(),
		RequestedBy:         userA,
		Status:              StatusDraft,
		CreatedBy:           userA,
		UpdatedBy:           userA,
	}
	if err := db.Create(&nullRow).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindByIDForAccess(nullRow.ID, accessA); err != nil {
		t.Fatalf("NULL ServiceID unresolved must not invent deny: %v", err)
	}
}
