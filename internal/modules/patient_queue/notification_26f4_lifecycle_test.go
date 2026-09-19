package patient_queue

import (
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func emailLifecycleService(t *testing.T) (*gorm.DB, *Service, Access, uint, *AppointmentType) {
	t.Helper()
	db, base, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	svc := base.WithNotificationLifecycleConfig(NotificationLifecycleConfig{EmailEnabled: true})
	return db, svc, admin, prac, at
}

func countIntents(t *testing.T, db *gorm.DB, appointmentID uint, kind, channel string) int64 {
	t.Helper()
	var n int64
	q := db.Model(&AppointmentNotificationIntent{}).Where("appointment_id = ? AND kind = ?", appointmentID, kind)
	if channel != "" {
		q = q.Where("channel = ?", channel)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func mustIntent(t *testing.T, db *gorm.DB, appointmentID uint, kind, channel string) AppointmentNotificationIntent {
	t.Helper()
	var row AppointmentNotificationIntent
	if err := db.Where("appointment_id = ? AND kind = ? AND channel = ?", appointmentID, kind, channel).
		First(&row).Error; err != nil {
		t.Fatalf("intent %s/%s: %v", kind, channel, err)
	}
	return row
}

func assertPairEqual(t *testing.T, logRow, emailRow AppointmentNotificationIntent) {
	t.Helper()
	if logRow.AppointmentID != emailRow.AppointmentID {
		t.Fatalf("appointment_id mismatch")
	}
	if logRow.OccurrenceKey != emailRow.OccurrenceKey {
		t.Fatalf("occurrence_key LOG=%s EMAIL=%s", logRow.OccurrenceKey, emailRow.OccurrenceKey)
	}
	if logRow.PayloadJSON != emailRow.PayloadJSON {
		t.Fatalf("payload mismatch LOG=%s EMAIL=%s", logRow.PayloadJSON, emailRow.PayloadJSON)
	}
	if !logRow.SendAfter.Equal(emailRow.SendAfter) {
		t.Fatalf("send_after LOG=%s EMAIL=%s", logRow.SendAfter, emailRow.SendAfter)
	}
}

func TestNotificationLifecycleChannelsDefaultAndEmail(t *testing.T) {
	t.Parallel()
	svc := NewService(nil)
	got := svc.NotificationLifecycleChannels()
	if len(got) != 1 || got[0] != NotifChannelLog {
		t.Fatalf("default channels=%v", got)
	}
	got[0] = "MUTATED"
	if svc.NotificationLifecycleChannels()[0] != NotifChannelLog {
		t.Fatal("SupportedChannels must return defensive copy")
	}
	email := svc.WithNotificationLifecycleConfig(NotificationLifecycleConfig{EmailEnabled: true})
	ch := email.NotificationLifecycleChannels()
	if len(ch) != 2 || ch[0] != NotifChannelLog || ch[1] != NotifChannelEmail {
		t.Fatalf("email-enabled channels=%v", ch)
	}
	for _, c := range ch {
		if c == NotifChannelSMS {
			t.Fatal("SMS must never be enabled")
		}
	}
	// Original unchanged
	if len(svc.NotificationLifecycleChannels()) != 1 {
		t.Fatal("WithNotificationLifecycleConfig must not mutate original")
	}
}

func TestPostgresDefaultLogOnlyNoEmail26F4(t *testing.T) {
	db, svc, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 4, 5, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac, start, at.ID)
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelLog) != 1 {
		t.Fatal("expected LOG BOOKED")
	}
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelEmail) != 0 {
		t.Fatal("default must not create EMAIL BOOKED")
	}
	if countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelLog) != 1 {
		t.Fatal("expected LOG REMINDER")
	}
	if countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelEmail) != 0 {
		t.Fatal("default must not create EMAIL REMINDER")
	}
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelSMS) != 0 {
		t.Fatal("SMS must be absent")
	}
}

func TestPostgresEmailEnabledBook26F4(t *testing.T) {
	db, svc, admin, prac, at := emailLifecycleService(t)
	start := time.Date(2027, 5, 3, 10, 0, 0, 0, time.UTC) // far future → reminder eligible
	appt := bookLife(t, svc, admin, 901, prac, start, at.ID)

	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelLog) != 1 ||
		countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelEmail) != 1 {
		t.Fatal("want BOOKED LOG+EMAIL")
	}
	if countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelLog) != 1 ||
		countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelEmail) != 1 {
		t.Fatal("want REMINDER LOG+EMAIL")
	}
	bookLog := mustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelLog)
	bookEmail := mustIntent(t, db, appt.ID, NotifKindBooked, NotifChannelEmail)
	assertPairEqual(t, bookLog, bookEmail)
	remLog := mustIntent(t, db, appt.ID, NotifKindReminderT24H, NotifChannelLog)
	remEmail := mustIntent(t, db, appt.ID, NotifKindReminderT24H, NotifChannelEmail)
	assertPairEqual(t, remLog, remEmail)
	if remLog.OccurrenceKey != OccurrenceKeyFromScheduledAt(start) {
		t.Fatalf("reminder key=%s", remLog.OccurrenceKey)
	}
	var sms int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND channel=?", appt.ID, NotifChannelSMS).Count(&sms)
	if sms != 0 {
		t.Fatalf("SMS intents=%d", sms)
	}

	// Dedup: re-running lifecycle enqueue must not duplicate same-channel rows.
	if err := svc.applyBookNotificationIntentsTx(svc.db, *appt, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelLog) != 1 ||
		countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelEmail) != 1 {
		t.Fatal("dedup: still one BOOKED per channel")
	}
	if countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelLog) != 1 ||
		countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelEmail) != 1 {
		t.Fatal("dedup: still one REMINDER per channel")
	}

	// <24h: BOOKED both channels, no reminders
	nearStart := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	if ReminderT24HEligible(nearStart, time.Now().UTC()) {
		t.Skip("nearStart unexpectedly eligible relative to wall clock")
	}
	near := bookLife(t, svc, admin, 902, prac, nearStart, at.ID)
	if countIntents(t, db, near.ID, NotifKindBooked, NotifChannelLog) != 1 ||
		countIntents(t, db, near.ID, NotifKindBooked, NotifChannelEmail) != 1 {
		t.Fatal("<24h BOOKED LOG+EMAIL required")
	}
	if countIntents(t, db, near.ID, NotifKindReminderT24H, "") != 0 {
		t.Fatal("<24h must not create reminders")
	}
}

func TestPostgresEmailEnabledBookEmptyPatientEmail26F4(t *testing.T) {
	db, svc, admin, prac, at := emailLifecycleService(t)
	// Ephemeral pq schema may omit patients.email; add empty column for recipient-independence proof.
	if err := db.Exec(`ALTER TABLE patients ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT ''`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE patients SET email = '' WHERE id = 905`).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2027, 6, 7, 9, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 905, prac, start, at.ID)
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelEmail) != 1 {
		t.Fatal("empty Patient.Email must still enqueue EMAIL BOOKED")
	}
	if countIntents(t, db, appt.ID, NotifKindReminderT24H, NotifChannelEmail) != 1 {
		t.Fatal("empty Patient.Email must still enqueue EMAIL REMINDER")
	}
}

func TestPostgresEmailEnabledReschedule26F4(t *testing.T) {
	db, svc, admin, prac, at := emailLifecycleService(t)
	start := time.Date(2027, 7, 5, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac, start, at.ID)
	oldKey := OccurrenceKeyFromScheduledAt(start)

	cur := mustReload(t, db, appt.ID)
	newStart := time.Date(2027, 7, 5, 14, 0, 0, 0, time.UTC)
	req := rsReq(cur, newStart)
	req.IdempotencyKey = "rs-26f4-1"
	if _, err := svc.RescheduleAppointment(appt.ID, req, admin); err != nil {
		t.Fatal(err)
	}
	newKey := OccurrenceKeyFromScheduledAt(newStart)
	if oldKey == newKey {
		t.Fatal("occurrence keys must differ after reschedule")
	}

	for _, ch := range []string{NotifChannelLog, NotifChannelEmail} {
		var oldRem AppointmentNotificationIntent
		if err := db.Where("appointment_id=? AND kind=? AND channel=? AND occurrence_key=?",
			appt.ID, NotifKindReminderT24H, ch, oldKey).First(&oldRem).Error; err != nil {
			t.Fatalf("old rem %s: %v", ch, err)
		}
		if oldRem.Status != NotifStatusCancelled {
			t.Fatalf("old rem %s status=%s", ch, oldRem.Status)
		}
	}

	resLog := mustIntent(t, db, appt.ID, NotifKindRescheduled, NotifChannelLog)
	resEmail := mustIntent(t, db, appt.ID, NotifKindRescheduled, NotifChannelEmail)
	assertPairEqual(t, resLog, resEmail)
	if resLog.OccurrenceKey != newKey {
		t.Fatalf("RESCHEDULED key=%s want %s", resLog.OccurrenceKey, newKey)
	}

	remLogNew := AppointmentNotificationIntent{}
	remEmailNew := AppointmentNotificationIntent{}
	if err := db.Where("appointment_id=? AND kind=? AND channel=? AND occurrence_key=?",
		appt.ID, NotifKindReminderT24H, NotifChannelLog, newKey).First(&remLogNew).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("appointment_id=? AND kind=? AND channel=? AND occurrence_key=?",
		appt.ID, NotifKindReminderT24H, NotifChannelEmail, newKey).First(&remEmailNew).Error; err != nil {
		t.Fatal(err)
	}
	if remLogNew.Status != NotifStatusPending || remEmailNew.Status != NotifStatusPending {
		t.Fatalf("new reminders status LOG=%s EMAIL=%s", remLogNew.Status, remEmailNew.Status)
	}
	assertPairEqual(t, remLogNew, remEmailNew)

	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelLog) != 1 {
		t.Fatal("historical BOOKED LOG must remain")
	}
	var sms int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND channel=?", appt.ID, NotifChannelSMS).Count(&sms)
	if sms != 0 {
		t.Fatal("SMS must be absent")
	}
}

func TestPostgresEmailEnabledCancel26F4(t *testing.T) {
	db, svc, admin, prac, at := emailLifecycleService(t)
	start := time.Date(2027, 8, 2, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 901, prac, start, at.ID)

	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "26f4", IdempotencyKey: "c-26f4"}, admin); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{NotifChannelLog, NotifChannelEmail} {
		rem := mustIntent(t, db, appt.ID, NotifKindReminderT24H, ch)
		if rem.Status != NotifStatusCancelled {
			t.Fatalf("reminder %s status=%s", ch, rem.Status)
		}
		canc := mustIntent(t, db, appt.ID, NotifKindCancelled, ch)
		if canc.Status != NotifStatusPending {
			t.Fatalf("CANCELLED %s status=%s", ch, canc.Status)
		}
	}
	cancLog := mustIntent(t, db, appt.ID, NotifKindCancelled, NotifChannelLog)
	cancEmail := mustIntent(t, db, appt.ID, NotifKindCancelled, NotifChannelEmail)
	assertPairEqual(t, cancLog, cancEmail)
	if countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelLog) != 1 ||
		countIntents(t, db, appt.ID, NotifKindBooked, NotifChannelEmail) != 1 {
		t.Fatal("historical BOOKED must remain")
	}
}

func TestPostgresNoShowSuppressesAllChannels26F4(t *testing.T) {
	db, emailSvc, admin, prac, at := emailLifecycleService(t)
	far := time.Date(2027, 9, 6, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, emailSvc, admin, 903, prac, far, at.ID)
	past := time.Now().UTC().Add(-2 * time.Hour)
	db.Model(&Appointment{}).Where("id=?", appt.ID).Updates(map[string]any{
		"scheduled_at": past, "scheduled_end_at": past.Add(30 * time.Minute),
	})

	// Disable EMAIL on a separate handle — suppress must still clear EMAIL reminders.
	logOnly := NewService(db)
	if _, err := logOnly.MarkNoShow(appt.ID, NoShowAppointmentRequest{Reason: "ns-26f4"}, admin); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{NotifChannelLog, NotifChannelEmail} {
		rem := mustIntent(t, db, appt.ID, NotifKindReminderT24H, ch)
		if rem.Status != NotifStatusCancelled {
			t.Fatalf("no-show must suppress %s reminder, got %s", ch, rem.Status)
		}
	}
	if countIntents(t, db, appt.ID, NotifKindCancelled, "") != 0 {
		t.Fatal("no-show must not create CANCELLED lifecycle intents")
	}
}

func TestPostgresRearmPerChannelIsolation26F4(t *testing.T) {
	db, svc, _, _, _ := emailLifecycleService(t)
	start := time.Date(2027, 10, 4, 10, 0, 0, 0, time.UTC)
	key := OccurrenceKeyFromScheduledAt(start)
	_, payload, err := BuildNotificationPayload(5001, start, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sendAfter := ReminderSendAfterT24H(start)
	for _, ch := range []string{NotifChannelLog, NotifChannelEmail} {
		row, e := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: 5001, PatientID: 1, Kind: NotifKindReminderT24H, Channel: ch,
			OccurrenceKey: key, SendAfter: sendAfter, PayloadJSON: payload,
		})
		if e != nil {
			t.Fatal(e)
		}
		now := time.Now().UTC()
		db.Model(row).Updates(map[string]any{
			"status": NotifStatusCancelled, "cancelled_at": now, "processing_started_at": nil,
		})
	}

	appt := Appointment{ID: 5001, PatientID: 1, ScheduledAt: start, Status: ApptScheduled}
	if err := svc.ensureReminderT24HForChannelsTx(svc.db, appt, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []string{NotifChannelLog, NotifChannelEmail} {
		var row AppointmentNotificationIntent
		if err := db.Where("appointment_id=? AND kind=? AND channel=? AND occurrence_key=?",
			5001, NotifKindReminderT24H, ch, key).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Status != NotifStatusPending {
			t.Fatalf("%s status=%s want PENDING", ch, row.Status)
		}
	}
	if countIntents(t, db, 5001, NotifKindReminderT24H, NotifChannelLog) != 1 ||
		countIntents(t, db, 5001, NotifKindReminderT24H, NotifChannelEmail) != 1 {
		t.Fatal("rearm must not duplicate rows")
	}

	// Terminal SENT must not rearm (LOG public API)
	logRow := mustIntent(t, db, 5001, NotifKindReminderT24H, NotifChannelLog)
	db.Model(&logRow).Updates(map[string]any{"status": NotifStatusSent, "sent_at": time.Now().UTC()})
	if _, err := svc.RearmCancelledReminder(5001, key, sendAfter, payload); err == nil {
		t.Fatal("SENT must not rearm via public LOG API")
	}
}

func TestPostgresMultiChannelEnqueueRollback26F4(t *testing.T) {
	db, svc, _, _, _ := emailLifecycleService(t)
	start := time.Date(2027, 11, 1, 10, 0, 0, 0, time.UTC)
	now := time.Now().UTC()
	end := start.Add(30 * time.Minute)
	appt := Appointment{
		ID: 0, PatientID: 1, ServiceID: 10, ScheduledAt: start, ScheduledEndAt: &end,
		Status: ApptScheduled, CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&appt).Error; e != nil {
			return e
		}
		if e := svc.applyBookNotificationIntentsTx(tx, appt, now); e != nil {
			return e
		}
		var n int64
		if e := tx.Model(&AppointmentNotificationIntent{}).Where("appointment_id=?", appt.ID).Count(&n).Error; e != nil {
			return e
		}
		if n < 2 {
			t.Fatalf("expected multi-channel intents inside TX, got %d", n)
		}
		return errors.New("force rollback after LOG+EMAIL enqueue")
	})
	if err == nil {
		t.Fatal("expected forced rollback error")
	}
	var apptN, intentN int64
	db.Model(&Appointment{}).Where("id=?", appt.ID).Count(&apptN)
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=?", appt.ID).Count(&intentN)
	if appt.ID != 0 && apptN != 0 {
		t.Fatalf("appointment must roll back, count=%d id=%d", apptN, appt.ID)
	}
	if intentN != 0 {
		t.Fatalf("intents must roll back, count=%d", intentN)
	}
}
