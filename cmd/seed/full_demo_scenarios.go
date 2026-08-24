package main

import (
	"fmt"
	"log"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// seedDemoSupportedScenarios completes the master dataset with states that are
// supported by the current product but are not naturally produced by the main
// happy-path fixtures. Every lookup uses a stable DEMO business key.
func seedDemoSupportedScenarios(
	db *gorm.DB,
	actor uint,
	patientByCode map[string]*patients.Patient,
	consultByPatient map[string]*consultations.Consultation,
	exams map[string]*consultations.MedicalExam,
	companies map[string]*company.InsuranceCompany,
) {
	now := time.Now().UTC().Truncate(time.Second)
	seedDemoAdditionalCoverages(db, patientByCode, companies, now)
	seedDemoConsultationStates(db, patientByCode, actor, now)
	seedDemoLaboratoryStates(db, patientByCode, consultByPatient, exams, actor, now)
	seedDemoImagingStates(db, patientByCode, consultByPatient, exams, actor, now)
	seedDemoBedsAndAssignments(db, actor, now)
	seedDemoAuthorizationStates(db, patientByCode, consultByPatient, actor, now)
	seedDemoInactiveStaff(db, actor)
}

func seedDemoAdditionalCoverages(db *gorm.DB, patientByCode map[string]*patients.Patient, companies map[string]*company.InsuranceCompany, now time.Time) {
	addDemoCoverage(db, patientByCode["P-DEMO-008"].ID, companies["DEMO-CNAM"].ID, "DEMO-MEMBER-P-DEMO-008-CNAM", 80, true, now.AddDate(1, 0, 0))
	addDemoCoverage(db, patientByCode["P-DEMO-009"].ID, companies["DEMO-ALLIANZ"].ID, "DEMO-MEMBER-P-DEMO-009-ALLIANZ", 90, true, now.AddDate(1, 0, 0))
	addDemoCoverage(db, patientByCode["P-DEMO-010"].ID, companies["DEMO-NSIA"].ID, "DEMO-MEMBER-P-DEMO-010-NSIA", 70, true, now.AddDate(1, 0, 0))

	var guarantorItem guarantor.InsuranceGuarantor
	must(db.Where("company_id=?", companies["DEMO-CNAM"].ID).First(&guarantorItem).Error)
	from, to := now.AddDate(0, -2, 0), now.AddDate(1, 0, 0)
	inactive := coverage.PatientCoverage{
		PatientID: patientByCode["P-DEMO-008"].ID, CompanyID: companies["DEMO-CNAM"].ID,
		GuarantorID: guarantorItem.ID, MemberNumber: "DEMO-MEMBER-INACTIVE", Subscriber: "DEMO",
		Beneficiary: "P-DEMO-008", CoverageRate: 80, ValidFrom: &from, ValidTo: &to,
		IsPrincipal: false, IsActive: false,
	}
	must(db.Where("member_number=?", inactive.MemberNumber).FirstOrCreate(&inactive).Error)
}

func seedDemoConsultationStates(db *gorm.DB, patientByCode map[string]*patients.Patient, actor uint, now time.Time) {
	specs := []struct {
		patient, marker, service, status string
	}{
		{"P-DEMO-008", "DEMO-CONSULTATION-DRAFT", "Médecine générale", consultations.ConsultationStatusDraft},
		{"P-DEMO-009", "DEMO-CONSULTATION-IN-PROGRESS", "Cardiologie", consultations.ConsultationStatusInProgress},
		{"P-DEMO-010", "DEMO-CONSULTATION-CANCELLED", "ORL", consultations.ConsultationStatusCancelled},
	}
	for index, spec := range specs {
		started := now.Add(-time.Duration(index+2) * time.Hour)
		item := consultations.Consultation{PatientID: patientByCode[spec.patient].ID, DoctorName: "Dr DEMO", Service: spec.service, Status: spec.status, StartedAt: &started, Diagnosis: spec.marker, Observations: "Scénario de statut DEMO"}
		if spec.status == consultations.ConsultationStatusCancelled || spec.status == consultations.ConsultationStatusCompleted {
			item.CompletedAt = &started
		}
		must(db.Where("diagnosis=?", spec.marker).FirstOrCreate(&item).Error)
		ensureDemoTimeline(db, item.PatientID, "consultation_"+spec.status, "clinical", "Consultation "+spec.status, spec.marker, "consultation", item.ID, actor, started)
	}
}

func seedDemoLaboratoryStates(db *gorm.DB, patientByCode map[string]*patients.Patient, consultByPatient map[string]*consultations.Consultation, exams map[string]*consultations.MedicalExam, actor uint, now time.Time) {
	specs := []struct {
		code, label, status, patient string
	}{
		{"DEMO-LAB-ORDERED", "Hémogramme en attente DEMO", laboratory.StatusOrdered, "P-DEMO-001"},
		{"DEMO-LAB-SAMPLE-PENDING", "Prélèvement attendu DEMO", laboratory.StatusSamplePending, "P-DEMO-002"},
		{"DEMO-LAB-COLLECTED", "Prélèvement collecté DEMO", laboratory.StatusSampleCollected, "P-DEMO-003"},
		{"DEMO-LAB-IN-PROGRESS", "Biochimie en analyse DEMO", laboratory.StatusInProgress, "P-DEMO-004"},
		{"DEMO-LAB-RESULT", "Résultat saisi DEMO", laboratory.StatusResultEntered, "P-DEMO-005"},
		{"DEMO-LAB-VALIDATED", "Résultat validé DEMO", laboratory.StatusValidated, "P-DEMO-006"},
		{"DEMO-LAB-CANCELLED", "Analyse annulée DEMO", laboratory.StatusCancelled, "P-DEMO-008"},
	}
	for index, spec := range specs {
		exam := consultations.MedicalExam{Code: spec.code, Name: spec.label, Category: "Laboratoire", IsActive: true}
		must(db.Where("code=?", spec.code).FirstOrCreate(&exam).Error)
		exams[spec.code] = &exam
		consult := consultByPatient[spec.patient]
		must(db.Where("consultation_id=? AND medical_exam_id=?", consult.ID, exam.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: consult.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: actor}).Error)
		var record medical_records.MedicalRecord
		must(db.Where("patient_id=?", consult.PatientID).First(&record).Error)
		order := laboratory.Order{RequestNumber: fmt.Sprintf("LAB-DEMO-STATE-%02d", index+1), ConsultationID: consult.ID, MedicalExamID: exam.ID, PatientID: consult.PatientID, MedicalRecordID: &record.ID, Priority: "ROUTINE", Status: spec.status, PrescribedBy: actor, CreatedBy: actor, UpdatedBy: actor}
		if spec.status == laboratory.StatusCancelled || spec.status == laboratory.StatusRejected {
			order.CancelledReason = "Scénario négatif DEMO"
		}
		if spec.status == laboratory.StatusValidated {
			order.ValidatedAt, order.ValidatedBy = &now, &actor
		}
		must(db.Where("request_number=?", order.RequestNumber).FirstOrCreate(&order).Error)
		if spec.status == laboratory.StatusSampleCollected || spec.status == laboratory.StatusInProgress || spec.status == laboratory.StatusResultEntered || spec.status == laboratory.StatusValidated || spec.status == laboratory.StatusRejected {
			sample := laboratory.Sample{OrderID: order.ID, SampleIdentifier: fmt.Sprintf("SMP-DEMO-%03d", index+1), SampleType: "Sang", Status: "COLLECTED", Comment: "Prélèvement DEMO", CollectedBy: actor, CollectedAt: now.Add(-time.Duration(index+1) * time.Hour)}
			must(db.Where("order_id=?", order.ID).FirstOrCreate(&sample).Error)
		}
		if spec.status == laboratory.StatusResultEntered || spec.status == laboratory.StatusValidated {
			value, minimum, maximum := 12.4+float64(index), 4.0, 15.0
			result := laboratory.Result{OrderID: order.ID, Parameter: "Paramètre DEMO", Value: fmt.Sprintf("%.1f", value), NumericValue: &value, Unit: "unité", ReferenceMin: &minimum, ReferenceMax: &maximum, Flag: "NORMAL", Comment: "Résultat structuré DEMO", EnteredBy: actor}
			must(db.Where("order_id=? AND parameter=?", order.ID, result.Parameter).FirstOrCreate(&result).Error)
		}
		ensureDemoTimeline(db, order.PatientID, "laboratory_"+spec.status, "laboratory", spec.label, order.RequestNumber, "laboratory_order", order.ID, actor, now.Add(time.Duration(index)*time.Minute))
	}
}

func seedDemoImagingStates(db *gorm.DB, patientByCode map[string]*patients.Patient, consultByPatient map[string]*consultations.Consultation, exams map[string]*consultations.MedicalExam, actor uint, now time.Time) {
	specs := []struct {
		code, label, modality, status, patient string
	}{
		{"DEMO-IMG-ORDERED", "Radiographie prescrite DEMO", "XRAY", imaging.StatusOrdered, "P-DEMO-001"},
		{"DEMO-IMG-SCHEDULED", "Échographie planifiée DEMO", "ULTRASOUND", imaging.StatusScheduled, "P-DEMO-002"},
		{"DEMO-IMG-PROGRESS", "Scanner en cours DEMO", "CT", imaging.StatusInProgress, "P-DEMO-003"},
		{"DEMO-IMG-DRAFTED", "Compte rendu brouillon DEMO", "XRAY", imaging.StatusReportDrafted, "P-DEMO-004"},
		{"DEMO-IMG-VALIDATED", "Imagerie validée DEMO", "ULTRASOUND", imaging.StatusValidated, "P-DEMO-005"},
		{"DEMO-IMG-CANCELLED", "Imagerie annulée DEMO", "CT", imaging.StatusCancelled, "P-DEMO-010"},
	}
	for index, spec := range specs {
		exam := consultations.MedicalExam{Code: spec.code, Name: spec.label, Category: "Imagerie", IsActive: true}
		must(db.Where("code=?", spec.code).FirstOrCreate(&exam).Error)
		exams[spec.code] = &exam
		consult := consultByPatient[spec.patient]
		must(db.Where("consultation_id=? AND medical_exam_id=?", consult.ID, exam.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: consult.ID, MedicalExamID: exam.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: actor}).Error)
		var record medical_records.MedicalRecord
		must(db.Where("patient_id=?", consult.PatientID).First(&record).Error)
		order := imaging.Order{OrderNumber: fmt.Sprintf("IMG-DEMO-STATE-%02d", index+1), AccessionNumber: fmt.Sprintf("ACC-DEMO-STATE-%02d", index+1), ConsultationID: consult.ID, MedicalExamID: exam.ID, PatientID: consult.PatientID, MedicalRecordID: &record.ID, Modality: spec.modality, Priority: "ROUTINE", Status: spec.status, PrescribedBy: actor, CreatedBy: actor, UpdatedBy: actor}
		if spec.status == imaging.StatusScheduled {
			order.ScheduledAt, order.ScheduledBy = &now, &actor
		}
		if spec.status == imaging.StatusInProgress || spec.status == imaging.StatusReportDrafted || spec.status == imaging.StatusValidated {
			order.PerformedAt, order.PerformedBy = &now, &actor
		}
		if spec.status == imaging.StatusCancelled {
			order.CancelledReason = "Annulation DEMO"
		}
		must(db.Where("order_number=?", order.OrderNumber).FirstOrCreate(&order).Error)
		if spec.status == imaging.StatusReportDrafted || spec.status == imaging.StatusValidated {
			report := imaging.Report{OrderID: order.ID, ClinicalIndication: "Indication DEMO", Technique: spec.modality, Findings: "Constatations structurées DEMO", Conclusion: "Conclusion DEMO", DraftedBy: actor, DraftedAt: now}
			if spec.status == imaging.StatusValidated {
				report.ValidatedBy, report.ValidatedAt = &actor, &now
			}
			must(db.Where("order_id=?", order.ID).FirstOrCreate(&report).Error)
		}
		ensureDemoTimeline(db, order.PatientID, "imaging_"+spec.status, "imaging", spec.label, order.OrderNumber, "imaging_order", order.ID, actor, now.Add(time.Duration(index)*time.Minute))
	}
}

func seedDemoBedsAndAssignments(db *gorm.DB, actor uint, now time.Time) {
	var cancelledConsult consultations.Consultation
	must(db.Where("diagnosis=?", "DEMO-CONSULTATION-CANCELLED").First(&cancelledConsult).Error)
	var cancelledRecord medical_records.MedicalRecord
	must(db.Where("patient_id=?", cancelledConsult.PatientID).First(&cancelledRecord).Error)
	cancelledHospitalization := hospitalizations.Hospitalization{PatientID: cancelledConsult.PatientID, MedicalRecordID: cancelledRecord.ID, SourceConsultationID: cancelledConsult.ID, AdmissionNumber: "HOSP-DEMO-CANCELLED", HospitalizationType: "Médicale", AdmissionReason: "Admission annulée DEMO", Department: "ORL", Status: hospitalizations.StatusCancelled}
	must(db.Where("admission_number=?", cancelledHospitalization.AdmissionNumber).FirstOrCreate(&cancelledHospitalization).Error)
	ensureDemoTimeline(db, cancelledHospitalization.PatientID, "hospitalization_cancelled", "hospitalization", "Hospitalisation annulée", cancelledHospitalization.AdmissionNumber, "hospitalization", cancelledHospitalization.ID, actor, now)
	var admittedPatient patients.Patient
	must(db.Where("code_patient=?", "P-DEMO-006").First(&admittedPatient).Error)
	withoutBedConsult := consultations.Consultation{PatientID: admittedPatient.ID, DoctorName: "Dr DEMO", Service: "Médecine", Status: consultations.ConsultationStatusCompleted, Diagnosis: "DEMO-HOSPITALIZATION-WITHOUT-BED", Observations: "Admission autorisée sans lit"}
	must(db.Where("patient_id=? AND diagnosis=?", admittedPatient.ID, withoutBedConsult.Diagnosis).FirstOrCreate(&withoutBedConsult).Error)
	var admittedRecord medical_records.MedicalRecord
	must(db.Where("patient_id=?", admittedPatient.ID).First(&admittedRecord).Error)
	withoutBed := hospitalizations.Hospitalization{PatientID: admittedPatient.ID, MedicalRecordID: admittedRecord.ID, SourceConsultationID: withoutBedConsult.ID, AdmissionNumber: "HOSP-DEMO-ADMITTED-NO-BED", HospitalizationType: "Médicale", AdmissionReason: "Admission sans lit DEMO", Department: "Médecine", Status: hospitalizations.StatusAdmitted, AdmittedAt: &now}
	must(db.Where("admission_number=?", withoutBed.AdmissionNumber).FirstOrCreate(&withoutBed).Error)
	ensureDemoTimeline(db, withoutBed.PatientID, "hospitalization_admitted", "hospitalization", "Admission sans lit", withoutBed.AdmissionNumber, "hospitalization", withoutBed.ID, actor, now)

	rooms := []hospitalizations.Room{
		{Code: "DEMO-URG-RDC-STD-001", Name: "Urgences DEMO", Department: "Urgences", Floor: "RDC", RoomType: "STANDARD", IsActive: true},
		{Code: "DEMO-MED-R1-STD-001", Name: "Médecine DEMO", Department: "Médecine", Floor: "R+1", RoomType: "STANDARD", IsActive: true},
		{Code: "DEMO-MAT-R2-MAT-001", Name: "Maternité DEMO", Department: "Maternité", Floor: "R+2", RoomType: "MATERNITY", IsActive: true},
		{Code: "DEMO-CHI-R3-ISO-001", Name: "Chirurgie inactive DEMO", Department: "Chirurgie", Floor: "R+3", RoomType: "ISOLATION", IsActive: false},
		{Code: "DEMO-MED-R1-STD-002", Name: "Chambre sans lit DEMO", Department: "Médecine", Floor: "R+1", RoomType: "STANDARD", IsActive: true},
	}
	roomByCode := map[string]hospitalizations.Room{}
	for _, room := range rooms {
		must(db.Where("code=?", room.Code).FirstOrCreate(&room).Error)
		roomByCode[room.Code] = room
	}
	beds := []hospitalizations.Bed{
		{RoomID: roomByCode["DEMO-URG-RDC-STD-001"].ID, Code: "DEMO-URG-RDC-STD-001-L01", Label: "Lit disponible DEMO", BedType: "STANDARD", Status: hospitalizations.BedAvailable, IsActive: true},
		{RoomID: roomByCode["DEMO-URG-RDC-STD-001"].ID, Code: "DEMO-URG-RDC-STD-001-L02", Label: "Lit réservé DEMO", BedType: "STANDARD", Status: hospitalizations.BedReserved, IsActive: true},
		{RoomID: roomByCode["DEMO-MED-R1-STD-001"].ID, Code: "DEMO-MED-R1-STD-001-L01", Label: "Lit occupé DEMO", BedType: "STANDARD", Status: hospitalizations.BedOccupied, IsActive: true},
		{RoomID: roomByCode["DEMO-MAT-R2-MAT-001"].ID, Code: "DEMO-MAT-R2-MAT-001-L01", Label: "Lit hors service DEMO", BedType: "MATERNITY", Status: hospitalizations.BedOutOfService, IsActive: true},
		{RoomID: roomByCode["DEMO-CHI-R3-ISO-001"].ID, Code: "DEMO-CHI-R3-ISO-001-L01", Label: "Lit inactif DEMO", BedType: "ISOLATION", Status: hospitalizations.BedAvailable, IsActive: false},
	}
	bedByCode := map[string]hospitalizations.Bed{}
	for _, bed := range beds {
		must(db.Where("code=?", bed.Code).FirstOrCreate(&bed).Error)
		bedByCode[bed.Code] = bed
	}
	var planned, admitted, discharged hospitalizations.Hospitalization
	must(db.Where("admission_number=?", "HOSP-DEMO-001").First(&planned).Error)
	must(db.Where("admission_number=?", "HOSP-DEMO-002").First(&admitted).Error)
	must(db.Where("admission_number=?", "HOSP-DEMO-003").First(&discharged).Error)
	assignments := []hospitalizations.BedAssignment{
		{HospitalizationID: planned.ID, PatientID: planned.PatientID, BedID: bedByCode["DEMO-URG-RDC-STD-001-L02"].ID, AssignedAt: now.Add(-6 * time.Hour), AssignmentType: hospitalizations.AssignmentReserved},
		{HospitalizationID: admitted.ID, PatientID: admitted.PatientID, BedID: bedByCode["DEMO-MED-R1-STD-001-L01"].ID, AssignedAt: now.Add(-24 * time.Hour), AssignmentType: hospitalizations.AssignmentOccupied},
	}
	released := now.Add(-48 * time.Hour)
	assignments = append(assignments, hospitalizations.BedAssignment{HospitalizationID: discharged.ID, PatientID: discharged.PatientID, BedID: bedByCode["DEMO-URG-RDC-STD-001-L01"].ID, AssignedAt: now.Add(-72 * time.Hour), ReleasedAt: &released, AssignmentType: hospitalizations.AssignmentOccupied})
	for _, assignment := range assignments {
		must(db.Where("hospitalization_id=? AND bed_id=?", assignment.HospitalizationID, assignment.BedID).FirstOrCreate(&assignment).Error)
	}
	// A released historical assignment makes the transfer/release history visible
	// without changing the currently occupied bed.
	transferRelease := now.Add(-25 * time.Hour)
	prior := hospitalizations.BedAssignment{HospitalizationID: admitted.ID, PatientID: admitted.PatientID, BedID: bedByCode["DEMO-URG-RDC-STD-001-L01"].ID, AssignedAt: now.Add(-30 * time.Hour), ReleasedAt: &transferRelease, AssignmentType: hospitalizations.AssignmentOccupied}
	must(db.Where("hospitalization_id=? AND bed_id=?", prior.HospitalizationID, prior.BedID).FirstOrCreate(&prior).Error)
	_ = actor
}

func seedDemoVoucherStates(db *gorm.DB, actor uint, consultByPatient map[string]*consultations.Consultation) {
	var paracetamol, ibuprofen pharmacy.MedicationPresentation
	must(db.Where("code=?", "PARA-500-TAB").First(&paracetamol).Error)
	must(db.Where("code=?", "IBU-400-TAB").First(&ibuprofen).Error)
	ensurePrescription := func(consult *consultations.Consultation, presentation pharmacy.MedicationPresentation, marker string, quantity float64) consultations.ConsultationPrescription {
		item := consultations.ConsultationPrescription{ConsultationID: consult.ID, PresentationID: &presentation.ID, MedicationName: marker, Dosage: presentation.Dosage, Form: presentation.Form, Route: presentation.Route, Quantity: quantity, Instructions: "Fixture bon pharmacie DEMO"}
		must(db.Where("consultation_id=? AND medication_name=?", consult.ID, marker).FirstOrCreate(&item).Error)
		return item
	}
	pendingConsult := consultByPatient["P-DEMO-008"]
	ensurePrescription(pendingConsult, paracetamol, "DEMO-VOUCHER-PENDING-LINE-1", 6)
	ensurePrescription(pendingConsult, ibuprofen, "DEMO-VOUCHER-PENDING-LINE-2", 4)
	must(db.Transaction(func(tx *gorm.DB) error { return pharmacy.MaterializeVoucher(tx, pendingConsult.ID, &actor) }))

	partialConsult := consultByPatient["P-DEMO-009"]
	partialPrescription := ensurePrescription(partialConsult, paracetamol, "DEMO-VOUCHER-PARTIAL", 10)
	must(db.Transaction(func(tx *gorm.DB) error { return pharmacy.MaterializeVoucher(tx, partialConsult.ID, &actor) }))
	patientID := partialConsult.PatientID
	_, err := pharmacy.NewService(pharmacy.NewRepository(db)).CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: paracetamol.ID, PrescriptionID: &partialPrescription.ID, PatientID: &patientID, Quantity: 4, Notes: "Bon partiellement servi DEMO", IdempotencyKey: "DEMO-VOUCHER-PARTIAL-4"}, actor)
	if err != nil {
		log.Fatal(err)
	}
}

func seedDemoAuthorizationStates(db *gorm.DB, patientByCode map[string]*patients.Patient, consultByPatient map[string]*consultations.Consultation, actor uint, now time.Time) {
	specs := []struct{ patient, number, status string }{
		{"P-DEMO-008", "PEC-DEMO-DRAFT", authorization.StatusDraft},
		{"P-DEMO-009", "PEC-DEMO-SUBMITTED", authorization.StatusSubmitted},
		{"P-DEMO-010", "PEC-DEMO-CANCELLED", authorization.StatusCancelled},
	}
	for _, spec := range specs {
		patient := patientByCode[spec.patient]
		consult := consultByPatient[spec.patient]
		var record medical_records.MedicalRecord
		var cov coverage.PatientCoverage
		must(db.Where("patient_id=?", patient.ID).First(&record).Error)
		must(db.Where("patient_id=? AND is_active", patient.ID).Order("is_principal DESC,id").First(&cov).Error)
		amount := 25000.0
		item := authorization.InsuranceAuthorization{AuthorizationNumber: spec.number, PatientID: patient.ID, MedicalRecordID: record.ID, PatientCoverageID: cov.ID, InsuranceCompanyID: cov.CompanyID, GuarantorID: cov.GuarantorID, ReferenceType: "CONSULTATION", ReferenceID: consult.ID, Service: consult.Service, RequestedAmount: &amount, RequestedAt: now, RequestedBy: actor, Status: spec.status, Comment: "Scénario PEC " + spec.status + " DEMO", CreatedBy: actor, UpdatedBy: actor}
		must(db.Where("authorization_number=?", spec.number).FirstOrCreate(&item).Error)
		ensureDemoTimeline(db, patient.ID, "insurance_authorization_"+spec.status, "insurance", "PEC "+spec.status, spec.number, "insurance_authorization", item.ID, actor, now)
	}
}

func seedDemoInactiveStaff(db *gorm.DB, actor uint) {
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	user := auth.User{Name: "Compte inactif DEMO", Email: "demo.inactive@medcore.local", PasswordHash: string(hash), Role: "staff", IsActive: false}
	must(db.Where("email=?", user.Email).FirstOrCreate(&user).Error)
	active := false
	var profile staff.Profile
	db.Where("user_id=?", user.ID).First(&profile)
	_, err = staff.NewService(db).Upsert(profile.ID, staff.UpsertRequest{UserID: user.ID, EmployeeCode: "DEMO-STAFF-INACTIVE", JobTitle: "Compte désactivé", PrimaryDepartment: "Administration", Active: &active, Functions: []string{"CAISSIER"}}, actor)
	if err != nil {
		log.Fatal(err)
	}
	// Upsert synchronizes the profile; the authentication account is explicitly
	// kept inactive as a negative RBAC fixture.
	must(db.Model(&user).Update("is_active", false).Error)
}

func ensureDemoTimeline(db *gorm.DB, patientID uint, eventType, category, title, description, referenceType string, referenceID uint, actor uint, date time.Time) {
	var record medical_records.MedicalRecord
	if err := db.Where("patient_id=?", patientID).First(&record).Error; err != nil {
		log.Fatal(err)
	}
	event := medical_records.MedicalTimelineEvent{MedicalRecordID: record.ID, PatientID: patientID, EventType: eventType, Category: category, Title: title, Description: description, ReferenceType: referenceType, ReferenceID: &referenceID, Severity: "info", EventDate: date, CreatedBy: actor}
	must(db.Where("patient_id=? AND event_type=? AND reference_type=? AND reference_id=?", patientID, eventType, referenceType, referenceID).FirstOrCreate(&event).Error)
}
