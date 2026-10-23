package main

import (
	"errors"
	"log"
	"os"
	"time"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/cmd/seed/complete"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

// runCompleteDemo runs --demo-complete (guard already passed by requireDemoSeed) and
// returns true only when the generated dataset validates (OVERALL = PASS).
func runCompleteDemo(db *gorm.DB, businessTimezone string) bool {
	loc, err := time.LoadLocation(businessTimezone)
	if err != nil {
		log.Fatalf("demo-complete: fuseau métier %q invalide", businessTimezone)
	}
	cfg, err := complete.ParseConfig(complete.RawConfig{
		Anchor:   os.Getenv(complete.EnvAnchorDate),
		Seed:     os.Getenv(complete.EnvSeed),
		Patients: os.Getenv(complete.EnvPatients),
		Days:     os.Getenv(complete.EnvDays),
	}, loc, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("[DEMO-COMPLETE] période %s → %s (%d jours), %d patients, graine %d, fuseau %s",
		cfg.FirstDay().Format("2006-01-02"), cfg.Anchor.Format("2006-01-02"), cfg.Days, cfg.Patients, cfg.Seed, loc)
	rep, err := complete.Run(db, complete.Options{
		Config:        cfg,
		CreateAccount: createDemoAccount,
		ReferenceCatalogs: func(db *gorm.DB) {
			seedConsultationReasonCatalog(db)
			seedMedicalExams(db)
			seedPharmacyCatalog(db)
		},
		ComplexArchetypes: func(db *gorm.DB, items []patients.Patient) { seedClinicalDemo(db, items) },
		Logf:              log.Printf,
	})
	if err != nil && !errors.Is(err, complete.ErrDatasetExists) {
		log.Fatal(err)
	}
	rep.Print(log.Printf)
	return rep.Passed()
}
