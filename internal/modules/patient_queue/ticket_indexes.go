package patient_queue

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const ticketAppointmentUniqueIndex = "ux_pq_tickets_appointment"
const ticketPatientActiveUniqueIndex = "ux_pq_tickets_patient_active"

// EnsureTicketIndexes adds queue-ticket integrity indexes (LOT 23F + LOT28A).
// Partial unique on appointment_id: at most one ticket per appointment; walk-ins keep NULL.
// Partial unique on patient_id WHERE status=ACTIVE: one active visit per patient (LOT28A).
// Does not delete or rewrite historical rows — duplicate non-null appointment_id / ACTIVE
// patient_id groups fail clearly.
func EnsureTicketIndexes(db *gorm.DB) error {
	if err := ensureTicketAppointmentUniqueIndex(db); err != nil {
		return err
	}
	return ensureTicketPatientActiveUniqueIndex(db)
}

func ensureTicketAppointmentUniqueIndex(db *gorm.DB) error {
	var n int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT appointment_id FROM patient_queue_tickets
			WHERE appointment_id IS NOT NULL AND status = 'ACTIVE'
			GROUP BY appointment_id HAVING COUNT(*) > 1
		) d`).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("cannot create %s: %d appointment_id value(s) have duplicate ACTIVE tickets — resolve manually before migrating", ticketAppointmentUniqueIndex, n)
	}
	// LOT28A: narrow to ACTIVE so cancelled visits retain appointment_id for audit
	// while allowing a new check-in on the same appointment after cancel.
	if err := db.Exec(`DROP INDEX IF EXISTS ux_pq_tickets_appointment`).Error; err != nil {
		return fmt.Errorf("drop legacy %s: %w", ticketAppointmentUniqueIndex, err)
	}
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_tickets_appointment ON patient_queue_tickets (appointment_id) WHERE appointment_id IS NOT NULL AND status = 'ACTIVE'`
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("create %s: %w", ticketAppointmentUniqueIndex, err)
	}
	return assertTicketAppointmentUniqueIndex(db)
}

func ensureTicketPatientActiveUniqueIndex(db *gorm.DB) error {
	var n int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT patient_id FROM patient_queue_tickets
			WHERE status = 'ACTIVE'
			GROUP BY patient_id HAVING COUNT(*) > 1
		) d`).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("cannot create %s: %d patient_id value(s) have duplicate ACTIVE tickets — resolve manually before migrating", ticketPatientActiveUniqueIndex, n)
	}
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_tickets_patient_active ON patient_queue_tickets (patient_id) WHERE status = 'ACTIVE'`
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("create %s: %w", ticketPatientActiveUniqueIndex, err)
	}
	return assertTicketPatientActiveUniqueIndex(db)
}

// assertTicketAppointmentUniqueIndex verifies the LOT 23F uniqueness invariant is installed.
func assertTicketAppointmentUniqueIndex(db *gorm.DB) error {
	var def string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = ?`, ticketAppointmentUniqueIndex).Scan(&def).Error; err != nil {
		return fmt.Errorf("verify %s: %w", ticketAppointmentUniqueIndex, err)
	}
	if strings.TrimSpace(def) == "" {
		return fmt.Errorf("verify %s: index missing after create", ticketAppointmentUniqueIndex)
	}
	low := strings.ToLower(def)
	if !strings.Contains(low, "unique") {
		return fmt.Errorf("verify %s: expected UNIQUE index, got %s", ticketAppointmentUniqueIndex, def)
	}
	if !strings.Contains(low, "appointment_id") {
		return fmt.Errorf("verify %s: expected appointment_id column, got %s", ticketAppointmentUniqueIndex, def)
	}
	if !strings.Contains(low, "appointment_id is not null") {
		return fmt.Errorf("verify %s: expected partial predicate appointment_id IS NOT NULL, got %s", ticketAppointmentUniqueIndex, def)
	}
	if !strings.Contains(low, "status") || !strings.Contains(low, "active") {
		return fmt.Errorf("verify %s: expected partial predicate status = 'ACTIVE' (LOT28A), got %s", ticketAppointmentUniqueIndex, def)
	}
	return nil
}

// assertTicketPatientActiveUniqueIndex verifies LOT28A one-ACTIVE-visit-per-patient index.
func assertTicketPatientActiveUniqueIndex(db *gorm.DB) error {
	var def string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE indexname = ?`, ticketPatientActiveUniqueIndex).Scan(&def).Error; err != nil {
		return fmt.Errorf("verify %s: %w", ticketPatientActiveUniqueIndex, err)
	}
	if strings.TrimSpace(def) == "" {
		return fmt.Errorf("verify %s: index missing after create", ticketPatientActiveUniqueIndex)
	}
	low := strings.ToLower(def)
	if !strings.Contains(low, "unique") {
		return fmt.Errorf("verify %s: expected UNIQUE index, got %s", ticketPatientActiveUniqueIndex, def)
	}
	if !strings.Contains(low, "patient_id") {
		return fmt.Errorf("verify %s: expected patient_id column, got %s", ticketPatientActiveUniqueIndex, def)
	}
	if !strings.Contains(low, "status") || !strings.Contains(low, "active") {
		return fmt.Errorf("verify %s: expected partial predicate status = 'ACTIVE', got %s", ticketPatientActiveUniqueIndex, def)
	}
	return nil
}
