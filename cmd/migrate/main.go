package main

import (
	"log"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL)

	err := db.AutoMigrate(
		&audit.AuditLog{},
		&workflow.History{},
		&auth.User{},
		&patients.Patient{},

		&company.InsuranceCompany{},
		&guarantor.InsuranceGuarantor{},
		&coverage.PatientCoverage{},
		&voucher.InsuranceVoucher{},

		&medical_records.MedicalRecord{},
		&medical_records.MedicalAlert{},
		&medical_records.Allergy{},
		&medical_records.MedicalHistory{},
		&medical_records.VitalSign{},
		&medical_records.MedicalTimelineEvent{},
		&medical_records.PatientMedicalProfile{},
		&medical_records.SurgicalHistory{},
		&medical_records.FamilyMedicalHistory{},
		&medical_records.RegularTreatment{},
		&medical_records.Vaccination{},
		&medical_records.Disability{},
		&medical_records.Lifestyle{},
		&medical_records.MedicalDevice{},
		&medical_records.MedicalDocument{},

		&pharmacy.MedicationFamily{},
		&pharmacy.Medication{},
		&pharmacy.MedicationPresentation{},
		&pharmacy.PharmacyStock{},
		&pharmacy.PharmacyBatch{},
		&pharmacy.StockMovement{},
		&pharmacy.PharmacyDispensation{},
		&pharmacy.PharmacyDispensationItem{},

		&consultations.ConsultationReason{},
		&consultations.MedicalExam{},
		&consultations.PhysicalExamArea{},
		&consultations.Consultation{},
		&consultations.ConsultationVitals{},
		&consultations.ConsultationExamRequest{},
		&consultations.ConsultationPrescription{},
		&consultations.ConsultationAntecedent{},
		&consultations.ConsultationPhysicalExam{},
		&consultations.ConsultationAdministeredTreatment{},
		&consultations.ConsultationPreviousMedication{},
		&consultations.ConsultationSurgicalHistory{},
		&consultations.ConsultationGynecoObstetricHistory{},
		&consultations.ConsultationSOAP{},
		&consultations.ConsultationSpecialtyData{},
		&hospitalizations.Hospitalization{},
		&hospitalizations.Room{},
		&hospitalizations.Bed{},
		&hospitalizations.BedAssignment{},
	)

	if err != nil {
		log.Fatal("Erreur migration:", err)
	}
	for _, statement := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_bed ON hospitalization_bed_assignments (bed_id) WHERE released_at IS NULL AND deleted_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_stay ON hospitalization_bed_assignments (hospitalization_id) WHERE released_at IS NULL AND deleted_at IS NULL",
	} {
		if err := db.Exec(statement).Error; err != nil {
			log.Fatal("Erreur index bed management:", err)
		}
	}

	log.Println("Migrations exécutées avec succès")
}
