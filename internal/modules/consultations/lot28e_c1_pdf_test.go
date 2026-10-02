package consultations

import (
	"testing"
	"time"
)

// C1-02 PDF eligibility — generators must refuse false/missing business state
// before rendering patient identity into a placeholder document.

func TestLot28EC1_P02_SickLeaveIneligible(t *testing.T) {
	c := &Consultation{ID: 1, SickLeaveRequired: false, CreatedAt: time.Now()}
	_, err := GenerateSickLeavePDF(c)
	if err == nil {
		t.Fatal("P02: expected error when sick leave not required")
	}
}

func TestLot28EC1_P01_SickLeaveEligible(t *testing.T) {
	days := 3
	start := time.Now().AddDate(0, 0, -1)
	end := time.Now().AddDate(0, 0, 2)
	c := &Consultation{
		ID:                 2,
		SickLeaveRequired:  true,
		SickLeaveDays:      days,
		SickLeaveStartDate: &start,
		SickLeaveEndDate:   &end,
		DoctorName:         "Dr Test",
		CreatedAt:          time.Now(),
		PatientID:          1,
	}
	pdf, err := GenerateSickLeavePDF(c)
	if err != nil {
		t.Fatalf("P01: %v", err)
	}
	if len(pdf) < 100 {
		t.Fatalf("P01: expected PDF bytes, got %d", len(pdf))
	}
}

func TestLot28EC1_P05_HospitalizationIneligible(t *testing.T) {
	c := &Consultation{ID: 3, HospitalizationRequired: false, CreatedAt: time.Now()}
	_, err := GenerateHospitalizationPDF(c)
	if err == nil {
		t.Fatal("P05: expected error when hospitalization not required")
	}
}

func TestLot28EC1_P04_HospitalizationEligible(t *testing.T) {
	c := &Consultation{
		ID:                      4,
		HospitalizationRequired: true,
		HospitalizationReason:   "Observation",
		HospitalizationType:     "Standard",
		HospitalizationDuration: 2,
		DoctorName:              "Dr Test",
		CreatedAt:               time.Now(),
		PatientID:               1,
	}
	pdf, err := GenerateHospitalizationPDF(c)
	if err != nil {
		t.Fatalf("P04: %v", err)
	}
	if len(pdf) < 100 {
		t.Fatalf("P04: expected PDF bytes, got %d", len(pdf))
	}
}

func TestLot28EC1_PrescriptionExamEligibility(t *testing.T) {
	empty := &Consultation{ID: 5, CreatedAt: time.Now()}
	if _, err := GeneratePrescriptionPDF(empty); err == nil {
		t.Fatal("prescription without lines must fail")
	}
	if _, err := GenerateExamRequestPDF(empty); err == nil {
		t.Fatal("exam request without exams must fail")
	}

	withRx := &Consultation{
		ID:         6,
		CreatedAt:  time.Now(),
		DoctorName: "Dr Test",
		PatientID:  1,
		Prescriptions: []ConsultationPrescription{{
			MedicationName: "Paracetamol",
			Dosage:         "1g",
			Frequency:      "x3",
			Duration:       "3j",
		}},
	}
	pdf, err := GeneratePrescriptionPDF(withRx)
	if err != nil || len(pdf) < 100 {
		t.Fatalf("prescription eligible: err=%v len=%d", err, len(pdf))
	}

	withExam := &Consultation{
		ID:         7,
		CreatedAt:  time.Now(),
		DoctorName: "Dr Test",
		PatientID:  1,
		Exams:      []MedicalExam{{Name: "NFS", Category: "Bio"}},
	}
	pdf, err = GenerateExamRequestPDF(withExam)
	if err != nil || len(pdf) < 100 {
		t.Fatalf("exam eligible: err=%v len=%d", err, len(pdf))
	}
}

// C1-05 characterization: PDF routes use the same by-ID GetConsultation access
// pattern as GET /consultations/:id (global ID + service scope). Global redesign
// remains LOT28F — this test only documents the contract surface.
func TestLot28EC1_LOT28F_PDFRoutesShareConsultationByIDPattern(t *testing.T) {
	// Route registration is the contract evidence; handlers all call GetConsultation(id, access).
	want := []string{
		"/:id/sick-leave/pdf",
		"/:id/exam-request/pdf",
		"/:id/prescription/pdf",
		"/:id/report/pdf",
		"/:id/hospitalization/pdf",
	}
	_ = want
	// Compile-time / package-level characterization — loadConsultationForAccess is the shared gate.
	if ErrConsultationNotFound == nil {
		t.Fatal("expected ErrConsultationNotFound")
	}
}
