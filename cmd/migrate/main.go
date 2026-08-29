package main

import (
	"log"

	"github.com/lallene/medcore-his/backend/internal/config"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/database"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/access"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance_receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/modules/qa"
	"github.com/lallene/medcore-his/backend/internal/modules/receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/modules/ticketing"
)

func main() {
	cfg := config.Load()

	logger.Init(cfg.AppEnv)

	db := database.Connect(cfg.DatabaseURL)

	err := db.AutoMigrate(
		&audit.AuditLog{},
		&workflow.History{},
		&auth.User{},
		&organization.Department{},
		&organization.Service{},
		&staff.Profile{},
		&organization.StaffServiceAssignment{},
		&staff.Function{},
		&staff.Specialty{},
		&staff.Capability{},
		&staff.AuditEvent{},
		&access.PermissionOverride{},
		&access.MatrixOverride{},
		&access.AccessAuditEvent{},
		&billing.Tariff{},
		&billing.Invoice{},
		&billing.InvoiceLine{},
		&billing.AuthorizationAllocation{},
		&billing.Payment{},
		&receivables.Metadata{},
		&receivables.FollowUp{},
		&insurance_receivables.Settlement{},
		&insurance_receivables.SettlementAllocation{},
		&insurance_receivables.ReceivableMetadata{},
		&insurance_receivables.FollowUp{},
		&insurance_receivables.SubmissionBatch{},
		&insurance_receivables.SubmissionBatchItem{},
		&cash.Register{},
		&cash.Session{},
		&cash.Receipt{},
		&patients.Patient{},

		&company.InsuranceCompany{},
		&guarantor.InsuranceGuarantor{},
		&coverage.PatientCoverage{},
		&voucher.InsuranceVoucher{},
		&authorization.InsuranceAuthorization{},
		&authorization.InsuranceAuthorizationAct{},

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
		&pharmacy.PharmacyVoucher{},
		&pharmacy.PharmacyVoucherLine{},

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
		&laboratory.Order{},
		&laboratory.Sample{},
		&laboratory.Result{},
		&imaging.Order{},
		&imaging.Report{},
		&qa.Campaign{},
		&qa.TestResult{},
		&qa.Artifact{},
		&ticketing.Category{},
		&ticketing.SLA{},
		&ticketing.Ticket{},
		&ticketing.Comment{},
		&ticketing.Attachment{},
		&ticketing.Assignment{},
		&ticketing.History{},
		&ticketing.Notification{},
		&patient_queue.AppointmentType{},
		&patient_queue.Appointment{},
		&patient_queue.AppointmentHistory{},
		&patient_queue.Ticket{},
		&patient_queue.History{},
		&patient_queue.StaffWorkingSchedule{},
		&patient_queue.ScheduleException{},
		&patient_queue.ScheduleAuditEvent{},
	)

	if err != nil {
		log.Fatal("Erreur migration:", err)
	}
	if err := patient_queue.EnsureAppointmentIndexes(db); err != nil {
		log.Fatal("Erreur index patient_queue appointments:", err)
	}
	if err := patient_queue.EnsureScheduleIndexes(db); err != nil {
		log.Fatal("Erreur index patient_queue schedules:", err)
	}
	if err := pharmacy.BackfillVouchers(db); err != nil {
		log.Fatal("Erreur matérialisation bons pharmacie:", err)
	}
	if err := organization.BackfillLegacy(db, 1); err != nil {
		log.Fatal("Erreur migration organisation:", err)
	}
	for _, statement := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_billing_active_billable_key ON billing_invoice_lines (billable_key) WHERE is_active = true",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_cash_sessions_open_register ON cash_sessions (cash_register_id) WHERE status = 'OPEN'",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_bed ON hospitalization_bed_assignments (bed_id) WHERE released_at IS NULL AND deleted_at IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_hospitalization_bed_assignments_active_stay ON hospitalization_bed_assignments (hospitalization_id) WHERE released_at IS NULL AND deleted_at IS NULL",
	} {
		if err := db.Exec(statement).Error; err != nil {
			log.Fatal("Erreur index bed management:", err)
		}
	}

	log.Println("Migrations exécutées avec succès")
}
