package patient_queue

import "gorm.io/gorm"

// EnsureScheduleIndexes adds lookup indexes for working schedules and exceptions.
func EnsureScheduleIndexes(db *gorm.DB) error {
	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_pq_sched_prac_svc_day ON patient_queue_staff_working_schedules (practitioner_id, service_id, weekday)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_sched_active_valid ON patient_queue_staff_working_schedules (active, valid_from, valid_until)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_ex_prac_svc_range ON patient_queue_schedule_exceptions (practitioner_id, service_id, start_at, end_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_ex_active ON patient_queue_schedule_exceptions (active, cancelled_at)`,
		`CREATE INDEX IF NOT EXISTS idx_pq_sched_audit_entity ON patient_queue_schedule_audit (entity_type, entity_id)`,
	}
	for _, sql := range statements {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	return nil
}
