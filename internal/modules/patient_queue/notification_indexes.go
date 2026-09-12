package patient_queue

import (
	"fmt"

	"gorm.io/gorm"
)

const notificationAttemptIntentFK = "fk_appt_notif_attempt_intent"

// EnsureNotificationIndexes creates query indexes, unique constraints, and the attempt→intent FK (LOT 23N-A).
// Idempotent. Failures must abort startup/migrate — unique index + FK underwrite idempotency and integrity.
func EnsureNotificationIndexes(db *gorm.DB) error {
	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_appt_notif_intent_due ON appointment_notification_intents (status, send_after)`,
		`CREATE INDEX IF NOT EXISTS idx_appt_notif_intent_appt ON appointment_notification_intents (appointment_id)`,
		`CREATE INDEX IF NOT EXISTS idx_appt_notif_intent_patient ON appointment_notification_intents (patient_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_appt_notif_intent ON appointment_notification_intents (appointment_id, kind, channel, occurrence_key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_appt_notif_attempt ON appointment_notification_attempts (intent_id, attempt_no)`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	if err := ensureNotificationAttemptIntentFK(db); err != nil {
		return err
	}
	return nil
}

// ensureNotificationAttemptIntentFK installs:
//
//	appointment_notification_attempts.intent_id
//	  REFERENCES appointment_notification_intents(id)
//	  ON UPDATE CASCADE ON DELETE RESTRICT
//
// RESTRICT keeps durable attempt history intact and forbids orphan attempts.
// An unrelated FK on intent_id does not count — only exact structural equivalence does
// (any constraint name, including GORM AutoMigrate names).
func ensureNotificationAttemptIntentFK(db *gorm.DB) error {
	ok, err := notificationAttemptIntentFKMatchesContract(db)
	if err != nil {
		return fmt.Errorf("check attempt→intent FK contract: %w", err)
	}
	if !ok {
		sql := `ALTER TABLE appointment_notification_attempts
			ADD CONSTRAINT fk_appt_notif_attempt_intent
			FOREIGN KEY (intent_id) REFERENCES appointment_notification_intents(id)
			ON UPDATE CASCADE ON DELETE RESTRICT`
		if err := db.Exec(sql).Error; err != nil {
			// Named constraint may already exist from a prior partial run, or GORM may have
			// raced; re-check exact contract before failing.
			okAfter, checkErr := notificationAttemptIntentFKMatchesContract(db)
			if checkErr != nil {
				return fmt.Errorf("create %s: %v (recheck: %w)", notificationAttemptIntentFK, err, checkErr)
			}
			if !okAfter {
				return fmt.Errorf("create %s: %w", notificationAttemptIntentFK, err)
			}
		}
	}
	return assertNotificationAttemptIntentFK(db)
}

// notificationAttemptIntentFKContractSQL matches the exact LOT 23N-A FK contract via pg_catalog.
//
// PostgreSQL confupdtype / confdeltype codes:
//
//	'c' = CASCADE, 'r' = RESTRICT, 'a' = NO ACTION, 'n' = SET NULL, 'd' = SET DEFAULT
//
// Declared DDL is ON UPDATE CASCADE + ON DELETE RESTRICT ('c' / 'r').
// ON DELETE NO ACTION ('a') is accepted only when NOT DEFERRABLE (condeferrable = false):
// deferred NO ACTION can postpone checks unlike RESTRICT. CASCADE/SET NULL/SET DEFAULT are not.
const notificationAttemptIntentFKContractSQL = `
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
  AND src.relname = 'appointment_notification_attempts'
  AND sa.attname = 'intent_id'
  AND cardinality(c.conkey) = 1
  AND tn.nspname = current_schema()
  AND tgt.relname = 'appointment_notification_intents'
  AND ta.attname = 'id'
  AND cardinality(c.confkey) = 1
  AND c.confupdtype = 'c'
  AND (
    c.confdeltype = 'r'
    OR (c.confdeltype = 'a' AND NOT c.condeferrable)
  )
`

func notificationAttemptIntentFKMatchesContract(db *gorm.DB) (bool, error) {
	var n int64
	if err := db.Raw(notificationAttemptIntentFKContractSQL).Scan(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func assertNotificationAttemptIntentFK(db *gorm.DB) error {
	ok, err := notificationAttemptIntentFKMatchesContract(db)
	if err != nil {
		return fmt.Errorf("verify attempt→intent FK: %w", err)
	}
	if !ok {
		return fmt.Errorf("verify attempt→intent FK: expected foreign key appointment_notification_attempts(intent_id) → appointment_notification_intents(id) ON UPDATE CASCADE ON DELETE RESTRICT (or non-deferrable NO ACTION)")
	}
	return nil
}
