package patient_queue

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const ticketAppointmentUniqueIndex = "ux_pq_tickets_appointment"

// EnsureTicketIndexes adds queue-ticket integrity indexes (LOT 23F).
// Partial unique on appointment_id: at most one ticket per appointment; walk-ins keep NULL.
// Does not delete or rewrite historical rows — duplicate non-null appointment_id fails clearly.
func EnsureTicketIndexes(db *gorm.DB) error {
	var n int64
	if err := db.Raw(`
		SELECT COUNT(*) FROM (
			SELECT appointment_id FROM patient_queue_tickets
			WHERE appointment_id IS NOT NULL
			GROUP BY appointment_id HAVING COUNT(*) > 1
		) d`).Scan(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("cannot create %s: %d appointment_id value(s) have duplicate tickets — resolve manually before migrating", ticketAppointmentUniqueIndex, n)
	}
	sql := `CREATE UNIQUE INDEX IF NOT EXISTS ux_pq_tickets_appointment ON patient_queue_tickets (appointment_id) WHERE appointment_id IS NOT NULL`
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("create %s: %w", ticketAppointmentUniqueIndex, err)
	}
	return assertTicketAppointmentUniqueIndex(db)
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
	return nil
}
