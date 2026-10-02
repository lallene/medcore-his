package main

import (
	"os"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Requires a disposable demo DB already migrated+seeded (e.g. medcore_lot12_demo).
// Skips on production-like names and when unset.
func TestPostgresDemoHospitalizationActiveAdmissionInvariant(t *testing.T) {
	dsn := os.Getenv("LOT28D_SEED_PROOF_DATABASE_URL")
	if dsn == "" {
		t.Skip("LOT28D_SEED_PROOF_DATABASE_URL unset")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}

	var withBed, noBed hospitalizations.Hospitalization
	if err := db.Where("admission_number=?", "HOSP-DEMO-002").First(&withBed).Error; err != nil {
		t.Fatalf("HOSP-DEMO-002: %v", err)
	}
	if err := db.Where("admission_number=?", "HOSP-DEMO-ADMITTED-NO-BED").First(&noBed).Error; err != nil {
		t.Fatalf("HOSP-DEMO-ADMITTED-NO-BED: %v", err)
	}
	if withBed.Status != hospitalizations.StatusAdmitted {
		t.Fatalf("with-bed status=%s", withBed.Status)
	}
	if noBed.Status != hospitalizations.StatusAdmitted {
		t.Fatalf("no-bed status=%s", noBed.Status)
	}
	if withBed.PatientID == noBed.PatientID {
		t.Fatalf("both ADMITTED fixtures share patient_id=%d", withBed.PatientID)
	}

	var withBedCode, noBedCode string
	_ = db.Model(&patients.Patient{}).Select("code_patient").Where("id=?", withBed.PatientID).Scan(&withBedCode)
	_ = db.Model(&patients.Patient{}).Select("code_patient").Where("id=?", noBed.PatientID).Scan(&noBedCode)
	if withBedCode != "P-DEMO-006" || noBedCode != "P-DEMO-001" {
		t.Fatalf("unexpected patients with-bed=%s no-bed=%s", withBedCode, noBedCode)
	}

	var activeBed, noBedActive int64
	_ = db.Raw(`SELECT COUNT(*) FROM hospitalization_bed_assignments WHERE hospitalization_id=? AND released_at IS NULL AND deleted_at IS NULL`, withBed.ID).Scan(&activeBed)
	_ = db.Raw(`SELECT COUNT(*) FROM hospitalization_bed_assignments WHERE hospitalization_id=? AND released_at IS NULL AND deleted_at IS NULL`, noBed.ID).Scan(&noBedActive)
	if activeBed != 1 {
		t.Fatalf("HOSP-DEMO-002 active beds=%d", activeBed)
	}
	if noBedActive != 0 {
		t.Fatalf("no-bed fixture active beds=%d", noBedActive)
	}

	var dup int64
	_ = db.Raw(`SELECT COUNT(*) FROM (SELECT patient_id FROM hospitalizations WHERE status='ADMITTED' AND deleted_at IS NULL GROUP BY patient_id HAVING COUNT(*)>1) t`).Scan(&dup)
	if dup != 0 {
		t.Fatalf("patients with >1 ADMITTED=%d", dup)
	}

	// Idempotency: one row per admission_number
	var n2, nNoBed int64
	_ = db.Model(&hospitalizations.Hospitalization{}).Where("admission_number=?", "HOSP-DEMO-002").Count(&n2)
	_ = db.Model(&hospitalizations.Hospitalization{}).Where("admission_number=?", "HOSP-DEMO-ADMITTED-NO-BED").Count(&nNoBed)
	if n2 != 1 || nNoBed != 1 {
		t.Fatalf("row counts HOSP-DEMO-002=%d NO-BED=%d", n2, nNoBed)
	}
}
