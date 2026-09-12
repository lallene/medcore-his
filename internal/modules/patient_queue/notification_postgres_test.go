package patient_queue

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

func notificationTestDB(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db := queuePostgres(t)
	if err := db.AutoMigrate(&AppointmentNotificationIntent{}, &AppointmentNotificationAttempt{}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	return db, NewService(db)
}

func TestPostgresNotificationIntentIdempotencyAndLifecycle23N(t *testing.T) {
	db, svc := notificationTestDB(t)

	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	_, payload, err := BuildNotificationPayload(1001, start, "Type A", "Urgences", "")
	if err != nil {
		t.Fatal(err)
	}
	in := EnqueueNotificationIntentInput{
		AppointmentID: 1001,
		PatientID:     501,
		Kind:          NotifKindReminderT24H,
		Channel:       NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start),
		SendAfter:     ReminderSendAfterT24H(start),
		PayloadJSON:   payload,
	}

	a, err := svc.EnqueueNotificationIntent(in)
	if err != nil || a == nil || a.ID == 0 {
		t.Fatalf("enqueue: %+v %v", a, err)
	}
	if a.Status != NotifStatusPending {
		t.Fatalf("status=%s", a.Status)
	}
	b, err := svc.EnqueueNotificationIntent(in)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != a.ID {
		t.Fatalf("idempotent enqueue must reuse id %d vs %d", a.ID, b.ID)
	}
	var count int64
	if err := db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=?", 1001).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want 1 row got %d", count)
	}

	// Concurrent duplicate enqueue
	var wg sync.WaitGroup
	ids := make(chan uint, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, e := svc.EnqueueNotificationIntent(in)
			if e != nil {
				t.Errorf("concurrent enqueue: %v", e)
				return
			}
			ids <- row.ID
		}()
	}
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != a.ID {
			t.Fatalf("concurrent idempotency broke: %d != %d", id, a.ID)
		}
	}

	due, err := svc.ListPendingDue(in.SendAfter.Add(time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range due {
		if d.ID == a.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("pending due missing intent")
	}

	proc, err := svc.MarkNotificationProcessing(a.ID)
	if err != nil || proc.Status != NotifStatusProcessing {
		t.Fatalf("processing: %+v %v", proc, err)
	}
	if _, err := svc.MarkNotificationProcessing(a.ID); err == nil {
		t.Fatal("double processing must fail")
	}
	sent, err := svc.MarkNotificationSent(a.ID)
	if err != nil || sent.Status != NotifStatusSent || sent.SentAt == nil {
		t.Fatalf("sent: %+v %v", sent, err)
	}
	if _, err := svc.MarkNotificationSent(a.ID); err == nil {
		t.Fatal("SENT → SENT must fail")
	}

	start2 := start.Add(2 * time.Hour)
	_, payload2, err := BuildNotificationPayload(1002, start2, "Type B", "Med", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 1002,
		PatientID:     502,
		Kind:          NotifKindBooked,
		Channel:       NotifChannelSMS,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start2),
		SendAfter:     time.Now().UTC().Add(-time.Minute),
		PayloadJSON:   payload2,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.CancelPendingForAppointment(1002)
	if err != nil || n != 1 {
		t.Fatalf("cancel pending n=%d err=%v", n, err)
	}
	cur, err := svc.FindNotificationIntent(c.ID)
	if err != nil || cur.Status != NotifStatusCancelled || cur.CancelledAt == nil {
		t.Fatalf("cancelled: %+v %v", cur, err)
	}

	att1, err := svc.RecordNotificationAttempt(c.ID, "log", nil, nil)
	if err != nil || att1.AttemptNo != 1 {
		t.Fatalf("attempt1=%+v %v", att1, err)
	}
	msg := "provider timeout"
	att2, err := svc.RecordNotificationAttempt(c.ID, "log", notifStrPtr("msg-1"), &msg)
	if err != nil || att2.AttemptNo != 2 {
		t.Fatalf("attempt2=%+v %v", att2, err)
	}
	if att2.Error == nil || *att2.Error != msg {
		t.Fatalf("error field=%v", att2.Error)
	}
}

func notifStrPtr(s string) *string { return &s }

func TestPostgresNotificationRejectsUnsafeAndIncompletePayload(t *testing.T) {
	_, svc := notificationTestDB(t)
	base := EnqueueNotificationIntentInput{
		AppointmentID: 1,
		PatientID:     1,
		Kind:          NotifKindBooked,
		Channel:       NotifChannelLog,
		OccurrenceKey: "k1",
		SendAfter:     time.Now().UTC(),
	}

	cases := []struct {
		name    string
		payload string
	}{
		{"reason", `{"appointmentId":1,"scheduledAt":"2026-01-01T10:00:00Z","reason":"douleur"}`},
		{"empty", ``},
		{"emptyObject", `{}`},
		{"zeroAppt", `{"appointmentId":0,"scheduledAt":"2026-01-01T10:00:00Z"}`},
		{"mismatch", `{"appointmentId":99,"scheduledAt":"2026-01-01T10:00:00Z"}`},
		{"missingScheduledAt", `{"appointmentId":1}`},
		{"invalidScheduledAt", `{"appointmentId":1,"scheduledAt":"not-a-time"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.PayloadJSON = tc.payload
			in.OccurrenceKey = "k-" + tc.name
			if _, err := svc.EnqueueNotificationIntent(in); err == nil {
				t.Fatal("expected reject")
			}
		})
	}

	in := base
	in.OccurrenceKey = "k-valid"
	in.PayloadJSON = `{"appointmentId":1,"scheduledAt":"2026-01-01T10:00:00.000000000Z"}`
	if _, err := svc.EnqueueNotificationIntent(in); err != nil {
		t.Fatalf("valid payload: %v", err)
	}
}

func TestPostgresNotificationAttemptFKRejectsOrphan(t *testing.T) {
	db, _ := notificationTestDB(t)
	err := db.Exec(`
		INSERT INTO appointment_notification_attempts (intent_id, attempt_no, provider, created_at)
		VALUES (?, 1, 'log', NOW())
	`, 9_999_999).Error
	if err == nil {
		t.Fatal("expected FK rejection for nonexistent intent")
	}
}

func TestPostgresNotificationAttemptFKMetadataContract(t *testing.T) {
	db, _ := notificationTestDB(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := assertNotificationAttemptIntentFK(db); err != nil {
		t.Fatal(err)
	}

	type fkRow struct {
		Conname       string
		Confupdtype   string
		Confdeltype   string
		Condeferrable bool
		SrcTable      string
		SrcColumn     string
		TgtTable      string
		TgtColumn     string
	}
	var rows []fkRow
	q := `
SELECT c.conname,
       c.confupdtype::text AS confupdtype,
       c.confdeltype::text AS confdeltype,
       c.condeferrable AS condeferrable,
       src.relname AS src_table,
       sa.attname AS src_column,
       tgt.relname AS tgt_table,
       ta.attname AS tgt_column
FROM pg_constraint c
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
	if err := db.Raw(q).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected exact attempt→intent FK metadata")
	}
	for _, r := range rows {
		if r.SrcTable != "appointment_notification_attempts" || r.SrcColumn != "intent_id" {
			t.Fatalf("source mismatch: %+v", r)
		}
		if r.TgtTable != "appointment_notification_intents" || r.TgtColumn != "id" {
			t.Fatalf("target mismatch: %+v", r)
		}
		if r.Confupdtype != "c" {
			t.Fatalf("ON UPDATE must be CASCADE ('c'), got %q (%s)", r.Confupdtype, r.Conname)
		}
		switch r.Confdeltype {
		case "r":
			// RESTRICT always valid under contract.
		case "a":
			if r.Condeferrable {
				t.Fatalf("NO ACTION must be non-deferrable to satisfy contract (%s)", r.Conname)
			}
		default:
			t.Fatalf("ON DELETE must be RESTRICT or non-deferrable NO ACTION, got %q (%s)", r.Confdeltype, r.Conname)
		}
	}

	dropIntentIDFKs := func() {
		t.Helper()
		var names []string
		if err := db.Raw(`
			SELECT c.conname FROM pg_constraint c
			JOIN pg_class src ON src.oid = c.conrelid
			JOIN pg_namespace sn ON sn.oid = src.relnamespace
			JOIN pg_attribute sa ON sa.attrelid = c.conrelid AND sa.attnum = ANY (c.conkey) AND NOT sa.attisdropped
			WHERE c.contype = 'f'
			  AND sn.nspname = current_schema()
			  AND src.relname = 'appointment_notification_attempts'
			  AND sa.attname = 'intent_id'
		`).Scan(&names).Error; err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			if err := db.Exec(`ALTER TABLE appointment_notification_attempts DROP CONSTRAINT ` + quotePGIdent(name)).Error; err != nil {
				t.Fatal(err)
			}
		}
	}

	// Unrelated FK on intent_id must not satisfy the contract helper alone.
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS _notif_fk_decoy (id BIGSERIAL PRIMARY KEY)`).Error; err != nil {
		t.Fatal(err)
	}
	dropIntentIDFKs()
	if err := db.Exec(`
		ALTER TABLE appointment_notification_attempts
		ADD CONSTRAINT fk_appt_notif_attempt_decoy
		FOREIGN KEY (intent_id) REFERENCES _notif_fk_decoy(id)
		ON UPDATE CASCADE ON DELETE RESTRICT
	`).Error; err != nil {
		t.Fatal(err)
	}
	ok, err := notificationAttemptIntentFKMatchesContract(db)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("wrong-target FK must not satisfy attempt→intent contract")
	}
	if err := assertNotificationAttemptIntentFK(db); err == nil {
		t.Fatal("assert must fail for wrong-target FK")
	}
	if err := db.Exec(`ALTER TABLE appointment_notification_attempts DROP CONSTRAINT fk_appt_notif_attempt_decoy`).Error; err != nil {
		t.Fatal(err)
	}

	// Exact target + DEFERRABLE INITIALLY DEFERRED NO ACTION must NOT satisfy the contract.
	dropIntentIDFKs()
	if err := db.Exec(`
		ALTER TABLE appointment_notification_attempts
		ADD CONSTRAINT fk_appt_notif_attempt_deferred
		FOREIGN KEY (intent_id) REFERENCES appointment_notification_intents(id)
		ON UPDATE CASCADE ON DELETE NO ACTION
		DEFERRABLE INITIALLY DEFERRED
	`).Error; err != nil {
		t.Fatal(err)
	}
	ok, err = notificationAttemptIntentFKMatchesContract(db)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("deferrable NO ACTION FK must not satisfy attempt→intent contract")
	}
	if err := assertNotificationAttemptIntentFK(db); err == nil {
		t.Fatal("assert must fail for deferrable NO ACTION FK")
	}
	if err := db.Exec(`ALTER TABLE appointment_notification_attempts DROP CONSTRAINT fk_appt_notif_attempt_deferred`).Error; err != nil {
		t.Fatal(err)
	}

	// Restore contract for schema cleanup safety.
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
}

func quotePGIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func TestPostgresNotificationAttemptNonexistentIntentNotFound(t *testing.T) {
	_, svc := notificationTestDB(t)
	_, err := svc.RecordNotificationAttempt(9_999_998, "log", nil, nil)
	if err == nil {
		t.Fatal("expected not found")
	}
	var appErr *coreerrors.AppError
	if !errors.As(err, &appErr) || appErr.Status != 404 {
		t.Fatalf("want NotFound (404), got %v", err)
	}
}

func TestPostgresNotificationAttemptConcurrency(t *testing.T) {
	db, svc := notificationTestDB(t)

	start := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	_, payload, err := BuildNotificationPayload(2001, start, "Type", "Svc", "")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 2001,
		PatientID:     601,
		Kind:          NotifKindBooked,
		Channel:       NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start),
		SendAfter:     time.Now().UTC().Add(-time.Minute),
		PayloadJSON:   payload,
	})
	if err != nil {
		t.Fatal(err)
	}

	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	nos := make(chan int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			att, e := svc.RecordNotificationAttempt(intent.ID, "log", nil, nil)
			if e != nil {
				errs <- e
				return
			}
			nos <- att.AttemptNo
		}()
	}
	wg.Wait()
	close(errs)
	close(nos)
	for e := range errs {
		t.Fatalf("concurrent attempt: %v", e)
	}
	var nums []int
	for no := range nos {
		nums = append(nums, no)
	}
	if len(nums) != n {
		t.Fatalf("want %d attempt_no values, got %d", n, len(nums))
	}
	sort.Ints(nums)
	seen := map[int]struct{}{}
	for i, no := range nums {
		if no != i+1 {
			t.Fatalf("want contiguous attempt_no=%d got %v", i+1, nums)
		}
		if _, ok := seen[no]; ok {
			t.Fatalf("duplicate attempt_no %d", no)
		}
		seen[no] = struct{}{}
	}
	var count int64
	if err := db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", intent.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(n) {
		t.Fatalf("row count=%d want %d", count, n)
	}
}
