package consultations

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func consultationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: test PostgreSQL JWT ignoré")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := fmt.Sprintf("consultation_jwt_%d", time.Now().UnixNano())
	if err := admin.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schemaName + `" CASCADE`).Error })
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{
		NamingStrategy:                           schema.NamingStrategy{TablePrefix: `"` + schemaName + `".`},
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE "` + schemaName + `"."patients" (LIKE public.patients INCLUDING ALL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&Consultation{}, &ConsultationVitals{}, &ConsultationReason{},
		&MedicalExam{}, &ConsultationPrescription{}, &ConsultationAntecedent{},
		&PhysicalExamArea{}, &ConsultationPhysicalExam{}, &ConsultationAdministeredTreatment{},
		&ConsultationPreviousMedication{}, &ConsultationSurgicalHistory{},
		&ConsultationGynecoObstetricHistory{}, &ConsultationSOAP{}, &ConsultationSpecialtyData{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
		&pharmacy.PharmacyDispensation{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDispensedPrescriptionUpdateGuardsAndRollback(t *testing.T) {
	db := consultationIntegrationDB(t)
	c := Consultation{PatientID: 1, DoctorName: "Dr Guard", Service: "Médecine", Status: ConsultationStatusDraft, Diagnosis: "initial"}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	presentationA, presentationB := uint(11), uint(12)
	p1 := ConsultationPrescription{ConsultationID: c.ID, PresentationID: &presentationA, MedicationName: "COMMERCIAL A", Quantity: 10}
	p2 := ConsultationPrescription{ConsultationID: c.ID, PresentationID: &presentationB, MedicationName: "COMMERCIAL B", Quantity: 5}
	if err := db.Create(&[]ConsultationPrescription{p1, p2}).Error; err != nil {
		t.Fatal(err)
	}
	var saved []ConsultationPrescription
	db.Where("consultation_id = ?", c.ID).Order("id").Find(&saved)
	p1 = saved[0]
	p2 = saved[1]
	ref := p1.ID
	if err := db.Create(&pharmacy.PharmacyDispensation{PresentationID: presentationA, Quantity: 4, Status: pharmacy.DispensationStatusCompleted, ReferenceType: "CONSULTATION_PRESCRIPTION", ReferenceID: &ref}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	update := func(quantity float64, presentation *uint, include bool, diagnosis string) error {
		lines := []ConsultationPrescription{{ID: p2.ID, ConsultationID: c.ID, PresentationID: p2.PresentationID, MedicationName: p2.MedicationName, Quantity: p2.Quantity}}
		if include {
			lines = append(lines, ConsultationPrescription{ID: p1.ID, ConsultationID: c.ID, PresentationID: presentation, MedicationName: p1.MedicationName, Quantity: quantity})
		}
		return repo.UpdateConsultation(c.ID, 1, map[string]interface{}{"diagnosis": diagnosis}, nil, nil, false, nil, false, lines, true, nil, nil, nil, nil, nil, nil)
	}
	if err := update(8, &presentationA, true, "authorized-8"); err != nil {
		t.Fatal(err)
	}
	if err := update(4, &presentationA, true, "authorized-4"); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func() error{
		"below dispensed":       func() error { return update(3, &presentationA, true, "invalid-below") },
		"removed":               func() error { return update(4, &presentationA, false, "invalid-remove") },
		"presentation replaced": func() error { return update(4, &presentationB, true, "invalid-presentation") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrDispensedPrescriptionConflict) {
				t.Fatalf("erreur=%v", err)
			}
		})
	}
	var after Consultation
	db.First(&after, c.ID)
	if after.Diagnosis != "authorized-4" {
		t.Fatalf("rollback absent: %s", after.Diagnosis)
	}
	// Après réduction autorisée à 4, la prescription est totalement dispensée : structure verrouillée, instructions cliniques éditables.
	if err := update(12, &presentationA, true, "invalid-complete"); !errors.Is(err, ErrDispensedPrescriptionConflict) {
		t.Fatalf("prescription complète modifiable: %v", err)
	}
	lines := []ConsultationPrescription{{ID: p1.ID, ConsultationID: c.ID, PresentationID: &presentationA, MedicationName: p1.MedicationName, Quantity: 4, Instructions: "clinique modifiée"}, {ID: p2.ID, ConsultationID: c.ID, PresentationID: p2.PresentationID, MedicationName: p2.MedicationName, Quantity: p2.Quantity}}
	if err := repo.UpdateConsultation(c.ID, 1, nil, nil, nil, false, nil, false, lines, true, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("champ clinique refusé: %v", err)
	}
}

func TestConsultationTimelineUsesAuthenticatedAuthor(t *testing.T) {
	db := consultationIntegrationDB(t)
	patient := patients.Patient{CodePatient: "JWT-T-P", NumeroDossier: "JWT-T-D", Nom: "Timeline"}
	if err := db.Table(db.NamingStrategy.TableName("patients")).Create(&patient).Error; err != nil {
		t.Fatal(err)
	}
	record := medical_records.MedicalRecord{PatientID: patient.ID, RecordNumber: "JWT-T-MR", Status: "active"}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	medicalService := medical_records.NewService(medical_records.NewRepository(db))
	service := NewService(NewRepository(db), medicalService)
	const createAuthor uint = 81
	consultation, err := service.CreateConsultation(CreateConsultationRequest{PatientID: patient.ID, DoctorName: "Dr JWT", Service: "Médecine"}, createAuthor)
	if err != nil {
		t.Fatal(err)
	}
	var created medical_records.MedicalTimelineEvent
	if err := db.Where("reference_id = ? AND event_type = ?", consultation.ID, "consultation_created").First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.CreatedBy != createAuthor {
		t.Fatalf("created_by timeline création = %d", created.CreatedBy)
	}

	const statusAuthor uint = 82
	if _, err := service.UpdateStatus(consultation.ID, UpdateConsultationStatusRequest{Status: ConsultationStatusInProgress}, statusAuthor); err != nil {
		t.Fatal(err)
	}
	var status medical_records.MedicalTimelineEvent
	if err := db.Where("reference_id = ? AND event_type = ?", consultation.ID, "consultation_status_changed").First(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status.CreatedBy != statusAuthor {
		t.Fatalf("created_by timeline statut = %d", status.CreatedBy)
	}
}

func TestListConsultationsPaginationAndFilters(t *testing.T) {
	db := consultationIntegrationDB(t)
	patientA := patients.Patient{CodePatient: "LIST-A", NumeroDossier: "DOS-A", Nom: "Alpha", Prenoms: "Alice"}
	patientB := patients.Patient{CodePatient: "LIST-B", NumeroDossier: "DOS-B", Nom: "Beta", Prenoms: "Bob"}
	if err := db.Table(db.NamingStrategy.TableName("patients")).Create(&patientA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table(db.NamingStrategy.TableName("patients")).Create(&patientB).Error; err != nil {
		t.Fatal(err)
	}
	created := []Consultation{
		{PatientID: patientA.ID, DoctorName: "Dr One", Service: "Cardiologie", Status: ConsultationStatusDraft, Diagnosis: "Diagnostic ancien", CreatedAt: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)},
		{PatientID: patientA.ID, DoctorName: "Dr Two", Service: "Cardiologie", Status: ConsultationStatusCompleted, Diagnosis: "Diagnostic récent", CreatedAt: time.Date(2026, 1, 3, 8, 0, 0, 0, time.UTC)},
		{PatientID: patientB.ID, DoctorName: "Dr Three", Service: "Pédiatrie", Status: ConsultationStatusInProgress, Diagnosis: "Autre", CreatedAt: time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC)},
	}
	for index := range created {
		if err := db.Create(&created[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewRepository(db)
	page, err := repo.List(ConsultationListFilter{Page: 1, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.TotalPages != 2 || len(page.Data) != 2 || page.Data[0].ID != created[1].ID || page.Data[1].ID != created[2].ID {
		t.Fatalf("pagination/ordre incorrects: %#v", page)
	}
	filtered, err := repo.List(ConsultationListFilter{Page: 1, Limit: 20, PatientID: &patientA.ID, Status: ConsultationStatusCompleted, Service: "cardiologie", Search: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Data) != 1 || filtered.Data[0].ID != created[1].ID || filtered.Data[0].PatientName != "Alpha Alice" {
		t.Fatalf("filtres incorrects: %#v", filtered)
	}
}

func TestSOAPAndSpecialtyAuthorsComeOnlyFromJWT(t *testing.T) {
	db := consultationIntegrationDB(t)
	patient := patients.Patient{CodePatient: "JWT-P", NumeroDossier: "JWT-D", Nom: "Patient"}
	if err := db.Table(db.NamingStrategy.TableName("patients")).Create(&patient).Error; err != nil {
		t.Fatal(err)
	}
	consultation := Consultation{PatientID: patient.ID, DoctorName: "Dr JWT", Status: ConsultationStatusDraft}
	if err := db.Create(&consultation).Error; err != nil {
		t.Fatal(err)
	}
	service := NewService(NewRepository(db), nil)
	const jwtUserID uint = 73

	var soapRequest UpsertConsultationSOAPRequest
	if err := json.Unmarshal([]byte(`{"chiefComplaint":"test","userId":999}`), &soapRequest); err != nil {
		t.Fatal(err)
	}
	soap, err := service.UpsertSOAP(consultation.ID, soapRequest, jwtUserID)
	if err != nil {
		t.Fatal(err)
	}
	if soap.CreatedBy != jwtUserID || soap.UpdatedBy != jwtUserID {
		t.Fatalf("auteurs SOAP = %d/%d", soap.CreatedBy, soap.UpdatedBy)
	}

	var specialtyRequest UpsertConsultationSpecialtyRequest
	if err := json.Unmarshal([]byte(`{"specialtyCode":"CARDIOLOGY","data":{"note":"test"},"userId":999}`), &specialtyRequest); err != nil {
		t.Fatal(err)
	}
	specialty, err := service.UpsertSpecialtyData(consultation.ID, specialtyRequest, jwtUserID)
	if err != nil {
		t.Fatal(err)
	}
	if specialty.CreatedBy != jwtUserID || specialty.UpdatedBy != jwtUserID {
		t.Fatalf("auteurs spécialité = %d/%d", specialty.CreatedBy, specialty.UpdatedBy)
	}
}
