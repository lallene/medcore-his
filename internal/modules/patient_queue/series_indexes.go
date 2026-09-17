package patient_queue

import (
	"fmt"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

const appointmentSeriesFK = "fk_pq_appt_series"
const appointmentSeriesPairCheck = "chk_pq_appt_series_pair"

// Canonical CHECK text produced by PostgreSQL for our series pair invariant
// (verified via pg_get_constraintdef). Whitespace is ignored when comparing.
const appointmentSeriesPairCheckCanonicalDef = `CHECK ((((series_id IS NULL) AND (series_occurrence_index IS NULL)) OR ((series_id IS NOT NULL) AND (series_occurrence_index IS NOT NULL) AND (series_occurrence_index > 0))))`

const appointmentSeriesPairCheckDDL = `ALTER TABLE patient_queue_appointments
			ADD CONSTRAINT chk_pq_appt_series_pair
			CHECK (
				(series_id IS NULL AND series_occurrence_index IS NULL)
				OR (series_id IS NOT NULL AND series_occurrence_index IS NOT NULL AND series_occurrence_index > 0)
			)`

// EnsureAppointmentSeriesIndexes installs series integrity constraints (LOT 23O-A).
// Failures must abort startup — unique keys, CHECK pair, and FK underwrite atomic materialization.
func EnsureAppointmentSeriesIndexes(db *gorm.DB) error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_pq_series_patient ON patient_queue_appointment_series (patient_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_series_service ON patient_queue_appointment_series (service_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_series_practitioner ON patient_queue_appointment_series (practitioner_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_series_status ON patient_queue_appointment_series (status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_series_idempotency_caller ON patient_queue_appointment_series (created_by, idempotency_key) WHERE idempotency_key IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_appt_series_occurrence ON patient_queue_appointments (series_id, series_occurrence_index) WHERE series_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_pq_appt_series_id ON patient_queue_appointments (series_id) WHERE series_id IS NOT NULL`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	if err := ensureAppointmentSeriesPairCheck(db); err != nil {
		return err
	}
	if err := ensureAppointmentSeriesFK(db); err != nil {
		return err
	}
	return nil
}

func ensureAppointmentSeriesPairCheck(db *gorm.DB) error {
	ok, err := appointmentSeriesPairCheckMatchesContract(db)
	if err != nil {
		return fmt.Errorf("check series pair constraint contract: %w", err)
	}
	if !ok {
		// Name-only match is insufficient: a weaker/wrong same-name CHECK must be replaced.
		if err := dropAppointmentSeriesPairCheck(db); err != nil {
			return err
		}
		if err := db.Exec(appointmentSeriesPairCheckDDL).Error; err != nil {
			okAfter, checkErr := appointmentSeriesPairCheckMatchesContract(db)
			if checkErr != nil {
				return fmt.Errorf("create %s: %v (recheck: %w)", appointmentSeriesPairCheck, err, checkErr)
			}
			if !okAfter {
				return fmt.Errorf("create %s: %w", appointmentSeriesPairCheck, err)
			}
		}
	}
	return assertAppointmentSeriesPairCheck(db)
}

func dropAppointmentSeriesPairCheck(db *gorm.DB) error {
	sql := `ALTER TABLE patient_queue_appointments DROP CONSTRAINT IF EXISTS ` + quoteIdent(appointmentSeriesPairCheck)
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("drop %s: %w", appointmentSeriesPairCheck, err)
	}
	return nil
}

func assertAppointmentSeriesPairCheck(db *gorm.DB) error {
	ok, err := appointmentSeriesPairCheckMatchesContract(db)
	if err != nil {
		return fmt.Errorf("verify %s: %w", appointmentSeriesPairCheck, err)
	}
	if !ok {
		return fmt.Errorf("verify %s: expected CHECK ((series_id IS NULL AND series_occurrence_index IS NULL) OR (series_id IS NOT NULL AND series_occurrence_index IS NOT NULL AND series_occurrence_index > 0))", appointmentSeriesPairCheck)
	}
	return nil
}

const appointmentSeriesPairCheckDefSQL = `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class rel ON rel.oid = c.conrelid
JOIN pg_namespace nsp ON nsp.oid = rel.relnamespace
WHERE c.contype = 'c'
  AND nsp.nspname = current_schema()
  AND rel.relname = 'patient_queue_appointments'
  AND c.conname = 'chk_pq_appt_series_pair'
`

func appointmentSeriesPairCheckMatchesContract(db *gorm.DB) (bool, error) {
	var def string
	if err := db.Raw(appointmentSeriesPairCheckDefSQL).Scan(&def).Error; err != nil {
		return false, err
	}
	if strings.TrimSpace(def) == "" {
		return false, nil
	}
	return seriesPairCheckDefMatchesContract(def), nil
}

// seriesPairCheckDefMatchesContract compares pg_get_constraintdef output to the
// required pair invariant (whitespace-insensitive). Name existence alone is not enough.
func seriesPairCheckDefMatchesContract(def string) bool {
	return normalizeCheckConstraintDef(def) == normalizeCheckConstraintDef(appointmentSeriesPairCheckCanonicalDef)
}

func normalizeCheckConstraintDef(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(s) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func ensureAppointmentSeriesFK(db *gorm.DB) error {
	ok, err := appointmentSeriesFKMatchesContract(db)
	if err != nil {
		return fmt.Errorf("check appointment→series FK contract: %w", err)
	}
	if !ok {
		// Drop any non-conforming FK on series_id (e.g. GORM AutoMigrate defaults).
		if err := dropNonMatchingAppointmentSeriesFKs(db); err != nil {
			return err
		}
		sql := `ALTER TABLE patient_queue_appointments
			ADD CONSTRAINT fk_pq_appt_series
			FOREIGN KEY (series_id) REFERENCES patient_queue_appointment_series(id)
			ON UPDATE CASCADE ON DELETE RESTRICT`
		if err := db.Exec(sql).Error; err != nil {
			okAfter, checkErr := appointmentSeriesFKMatchesContract(db)
			if checkErr != nil {
				return fmt.Errorf("create %s: %v (recheck: %w)", appointmentSeriesFK, err, checkErr)
			}
			if !okAfter {
				return fmt.Errorf("create %s: %w", appointmentSeriesFK, err)
			}
		}
	}
	return assertAppointmentSeriesFK(db)
}

func dropNonMatchingAppointmentSeriesFKs(db *gorm.DB) error {
	var names []string
	err := db.Raw(`
SELECT c.conname FROM pg_constraint c
JOIN pg_class src ON src.oid = c.conrelid
JOIN pg_namespace sn ON sn.oid = src.relnamespace
JOIN pg_attribute sa ON sa.attrelid = c.conrelid
  AND sa.attnum = c.conkey[1] AND NOT sa.attisdropped
WHERE c.contype = 'f'
  AND sn.nspname = current_schema()
  AND src.relname = 'patient_queue_appointments'
  AND sa.attname = 'series_id'
  AND cardinality(c.conkey) = 1
`).Scan(&names).Error
	if err != nil {
		return fmt.Errorf("list series_id FKs: %w", err)
	}
	for _, name := range names {
		if err := db.Exec(`ALTER TABLE patient_queue_appointments DROP CONSTRAINT IF EXISTS ` + quoteIdent(name)).Error; err != nil {
			return fmt.Errorf("drop FK %s: %w", name, err)
		}
	}
	return nil
}

func quoteIdent(name string) string {
	return `"` + name + `"`
}

const appointmentSeriesFKContractSQL = `
SELECT COUNT(*) FROM pg_constraint c
JOIN pg_class src ON src.oid = c.conrelid
JOIN pg_namespace sn ON sn.oid = src.relnamespace
JOIN pg_class tgt ON tgt.oid = c.confrelid
JOIN pg_namespace tn ON tn.oid = tgt.relnamespace
JOIN pg_attribute sa ON sa.attrelid = c.conrelid
  AND sa.attnum = c.conkey[1] AND NOT sa.attisdropped
JOIN pg_attribute ta ON ta.attrelid = c.confrelid
  AND ta.attnum = c.confkey[1] AND NOT ta.attisdropped
WHERE c.contype = 'f'
  AND sn.nspname = current_schema()
  AND src.relname = 'patient_queue_appointments'
  AND sa.attname = 'series_id'
  AND cardinality(c.conkey) = 1
  AND tn.nspname = current_schema()
  AND tgt.relname = 'patient_queue_appointment_series'
  AND ta.attname = 'id'
  AND cardinality(c.confkey) = 1
  AND c.confupdtype = 'c'
  AND (
    c.confdeltype = 'r'
    OR (c.confdeltype = 'a' AND NOT c.condeferrable)
  )
`

func appointmentSeriesFKMatchesContract(db *gorm.DB) (bool, error) {
	var n int64
	if err := db.Raw(appointmentSeriesFKContractSQL).Scan(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func assertAppointmentSeriesFK(db *gorm.DB) error {
	ok, err := appointmentSeriesFKMatchesContract(db)
	if err != nil {
		return fmt.Errorf("verify appointment→series FK: %w", err)
	}
	if !ok {
		return fmt.Errorf("verify appointment→series FK: expected foreign key patient_queue_appointments(series_id) → patient_queue_appointment_series(id) ON UPDATE CASCADE ON DELETE RESTRICT (or non-deferrable NO ACTION)")
	}
	return nil
}
