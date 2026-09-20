package patient_queue

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

func TestPostgresNotificationLifecycleBookRescheduleCancel23NB(t *testing.T) {
	db, svc, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC) // Monday
	appt := bookLife(t, svc, admin, 901, prac, start, at.ID)

	var booked, reminders int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=? AND channel=?", appt.ID, NotifKindBooked, NotifChannelLog).Count(&booked)
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=? AND channel=?", appt.ID, NotifKindReminderT24H, NotifChannelLog).Count(&reminders)
	if booked != 1 || reminders != 1 {
		t.Fatalf("book intents booked=%d reminders=%d", booked, reminders)
	}
	var rem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=?", appt.ID, NotifKindReminderT24H).First(&rem).Error; err != nil {
		t.Fatal(err)
	}
	if rem.Status != NotifStatusPending || rem.OccurrenceKey != OccurrenceKeyFromScheduledAt(start) {
		t.Fatalf("reminder=%+v", rem)
	}
	lower := strings.ToLower(rem.PayloadJSON)
	for _, bad := range []string{`"reason"`, `"telephone"`, `"email"`} {
		if strings.Contains(lower, bad) {
			t.Fatalf("PHI key %s in payload %s", bad, rem.PayloadJSON)
		}
	}

	// <24h relative to wall clock on fixed slot — only assert when ineligible
	nearStart := time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)
	if !ReminderT24HEligible(nearStart, time.Now().UTC()) {
		nearAppt := bookLife(t, svc, admin, 902, prac, nearStart, at.ID)
		var nr int64
		db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", nearAppt.ID, NotifKindReminderT24H).Count(&nr)
		if nr != 0 {
			t.Fatalf("<24h must not create reminder, got %d", nr)
		}
		var nb int64
		db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", nearAppt.ID, NotifKindBooked).Count(&nb)
		if nb != 1 {
			t.Fatalf("BOOKED still required, got %d", nb)
		}
	}

	cur := mustReload(t, db, appt.ID)
	newStart := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	req := rsReq(cur, newStart)
	req.IdempotencyKey = "rs-notif-1"
	if _, err := svc.RescheduleAppointment(appt.ID, req, admin); err != nil {
		t.Fatal(err)
	}
	var oldRem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=? AND occurrence_key=?", appt.ID, NotifKindReminderT24H, OccurrenceKeyFromScheduledAt(start)).First(&oldRem).Error; err == nil {
		if oldRem.Status != NotifStatusCancelled {
			t.Fatalf("old reminder want CANCELLED got %s", oldRem.Status)
		}
	}
	var newRem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=? AND occurrence_key=?", appt.ID, NotifKindReminderT24H, OccurrenceKeyFromScheduledAt(newStart)).First(&newRem).Error; err != nil {
		t.Fatal(err)
	}
	if newRem.Status != NotifStatusPending {
		t.Fatalf("new reminder status=%s", newRem.Status)
	}
	var resched int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindRescheduled).Count(&resched)
	if resched != 1 {
		t.Fatalf("RESCHEDULED count=%d", resched)
	}

	cur2 := mustReload(t, db, appt.ID)
	req2 := rsReq(cur2, newStart)
	req2.IdempotencyKey = "rs-notif-1"
	if _, err := svc.RescheduleAppointment(appt.ID, req2, admin); err != nil {
		t.Fatal(err)
	}
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindRescheduled).Count(&resched)
	if resched != 1 {
		t.Fatalf("same-instant duplicate RESCHEDULED=%d", resched)
	}

	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "test"}, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.First(&newRem, newRem.ID)
	if newRem.Status != NotifStatusCancelled {
		t.Fatalf("after cancel reminder=%s", newRem.Status)
	}
	var cancelledKind int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindCancelled).Count(&cancelledKind)
	if cancelledKind != 1 {
		t.Fatalf("CANCELLED kind count=%d", cancelledKind)
	}
	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "test"}, admin); err != nil {
		t.Fatal(err)
	}
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindCancelled).Count(&cancelledKind)
	if cancelledKind != 1 {
		t.Fatalf("repeated cancel duplicated CANCELLED=%d", cancelledKind)
	}
}

func TestPostgresNotificationRearmAndWorker23NB(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	payload := mustPayload(t, 3001, start)
	key := OccurrenceKeyFromScheduledAt(start)
	row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3001, PatientID: 1, Kind: NotifKindReminderT24H, Channel: NotifChannelLog,
		OccurrenceKey: key, SendAfter: time.Now().UTC().Add(-time.Minute), PayloadJSON: payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	db.Model(row).Updates(map[string]any{"status": NotifStatusCancelled, "cancelled_at": now})

	rearmed, err := svc.RearmCancelledReminder(3001, key, ReminderSendAfterT24H(start), payload)
	if err != nil || rearmed.Status != NotifStatusPending || rearmed.CancelledAt != nil {
		t.Fatalf("rearm=%+v err=%v", rearmed, err)
	}
	db.Model(rearmed).Updates(map[string]any{"status": NotifStatusSent, "sent_at": now})
	if _, err := svc.RearmCancelledReminder(3001, key, ReminderSendAfterT24H(start), payload); err == nil {
		t.Fatal("SENT must not rearm")
	}

	start2 := start.Add(2 * time.Hour)
	due, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3002, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start2), SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3002, start2),
	})
	if err != nil {
		t.Fatal(err)
	}
	future, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3003, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "future-k", SendAfter: time.Now().UTC().Add(2 * time.Hour),
		PayloadJSON: mustPayload(t, 3003, start2.Add(time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	emailRow, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3004, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelEmail,
		OccurrenceKey: "email-k", SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3004, start2),
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := svc.ClaimDueNotificationIntents(time.Now().UTC(), 50, []string{NotifChannelLog})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[uint]bool{}
	for _, c := range claimed {
		ids[c.ID] = true
		if c.ProcessingStartedAt == nil {
			t.Fatal("claim must set processing_started_at")
		}
		if c.Channel != NotifChannelLog {
			t.Fatalf("claimed non-LOG %s", c.Channel)
		}
	}
	if !ids[due.ID] {
		t.Fatal("due LOG not claimed")
	}
	if ids[future.ID] || ids[emailRow.ID] {
		t.Fatal("future/EMAIL must not be claimed")
	}

	due2, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3005, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "conc-k", SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3005, start2),
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, e := svc.ClaimDueNotificationIntents(time.Now().UTC(), 10, []string{NotifChannelLog})
			if e != nil {
				t.Errorf("claim: %v", e)
				return
			}
			n := 0
			for _, r := range rows {
				if r.ID == due2.ID {
					n++
				}
			}
			results <- n
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for n := range results {
		wins += n
	}
	if wins != 1 {
		t.Fatalf("exactly one worker should claim intent, got %d", wins)
	}

	// Reset due to PENDING for worker delivery (earlier claim left it PROCESSING).
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", due.ID).Updates(map[string]any{
		"status": NotifStatusPending, "send_after": time.Now().UTC().Add(-time.Minute), "processing_started_at": nil,
	})

	w := mustNotificationWorker(t, svc, NotificationWorkerConfig{
		PollInterval: time.Hour,
		BatchSize:    20,
		Adapters:     notificationAdapters(NewLogDeliveryAdapter(NotifChannelLog, nil)),
	})
	if err := w.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := svc.FindNotificationIntent(due.ID)
	if reloaded.Status != NotifStatusSent {
		t.Fatalf("want SENT got %s", reloaded.Status)
	}

	noopIntent, _ := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3006, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "noop-k", SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3006, start2),
	})
	wNoop := mustNotificationWorker(t, svc, NotificationWorkerConfig{
		BatchSize: 10,
		Adapters:  notificationAdapters(NewNoopDeliveryAdapter(NotifChannelLog)),
	})
	claimedNoop, _ := svc.ClaimDueNotificationIntents(time.Now().UTC(), 50, []string{NotifChannelLog})
	for i := range claimedNoop {
		if claimedNoop[i].ID == noopIntent.ID {
			wNoop.processClaimed(context.Background(), &claimedNoop[i], time.Now().UTC())
		}
	}
	got, _ := svc.FindNotificationIntent(noopIntent.ID)
	if got.Status != NotifStatusSkipped {
		t.Fatalf("noop want SKIPPED got %s", got.Status)
	}

	failIntent, _ := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3007, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "fail-k", SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3007, start2),
	})
	wFail := mustNotificationWorker(t, svc, NotificationWorkerConfig{
		Adapters:  notificationAdapters(&failAdapter{}),
		BatchSize: 5,
	})
	for i := 0; i < NotificationMaxAttempts; i++ {
		db.Model(&AppointmentNotificationIntent{}).Where("id=?", failIntent.ID).Updates(map[string]any{
			"status": NotifStatusPending, "send_after": time.Now().UTC().Add(-time.Minute), "processing_started_at": nil,
		})
		rows, _ := svc.ClaimDueNotificationIntents(time.Now().UTC(), 5, []string{NotifChannelLog})
		for j := range rows {
			if rows[j].ID == failIntent.ID {
				wFail.processClaimed(context.Background(), &rows[j], time.Now().UTC())
			}
		}
	}
	final, _ := svc.FindNotificationIntent(failIntent.ID)
	if final.Status != NotifStatusFailed {
		t.Fatalf("max attempts want FAILED got %s", final.Status)
	}

	stale, _ := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3008, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "stale-k", SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3008, start2),
	})
	past := time.Now().UTC().Add(-NotificationStaleProcessing - time.Minute)
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", stale.ID).Updates(map[string]any{
		"status": NotifStatusProcessing, "processing_started_at": past,
	})
	n, err := svc.RecoverStaleProcessingClaims(time.Now().UTC(), 10)
	if err != nil || n < 1 {
		t.Fatalf("stale recover n=%d err=%v", n, err)
	}
	staleRel, _ := svc.FindNotificationIntent(stale.ID)
	if staleRel.Status != NotifStatusPending && staleRel.Status != NotifStatusFailed {
		t.Fatalf("stale recovered status=%s", staleRel.Status)
	}
	var staleAtt int64
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", stale.ID).Count(&staleAtt)
	if staleAtt != 1 {
		t.Fatalf("stale recovery must record exactly 1 attempt, got %d", staleAtt)
	}

	_ = db.Exec(`INSERT INTO patients(id) VALUES (9) ON CONFLICT DO NOTHING`)
	appt := Appointment{
		PatientID: 9, ServiceID: 10, ScheduledAt: start2, Status: ApptCancelled,
		CreatedBy: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&appt).Error; err != nil {
		t.Fatal(err)
	}
	rem, _ := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: appt.ID, PatientID: 9, Kind: NotifKindReminderT24H, Channel: NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start2), SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, appt.ID, start2),
	})
	claimedRem, _ := svc.ClaimDueNotificationIntents(time.Now().UTC(), 10, []string{NotifChannelLog})
	wSkip := mustNotificationWorker(t, svc, NotificationWorkerConfig{
		Adapters: notificationAdapters(NewLogDeliveryAdapter(NotifChannelLog, nil)),
	})
	for i := range claimedRem {
		if claimedRem[i].ID == rem.ID {
			wSkip.processClaimed(context.Background(), &claimedRem[i], time.Now().UTC())
		}
	}
	remGot, _ := svc.FindNotificationIntent(rem.ID)
	if remGot.Status != NotifStatusSkipped {
		t.Fatalf("cancelled appt reminder want SKIPPED got %s", remGot.Status)
	}
}

func TestPostgresNotificationCompletedReminderSkipped23NB(t *testing.T) {
	db, svc := notificationTestDB(t)
	start := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	_ = db.Exec(`INSERT INTO patients(id) VALUES (910) ON CONFLICT DO NOTHING`)
	appt := Appointment{
		PatientID: 910, ServiceID: 10, ScheduledAt: start, Status: ApptCompleted,
		CreatedBy: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&appt).Error; err != nil {
		t.Fatal(err)
	}
	rem, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: appt.ID, PatientID: 910, Kind: NotifKindReminderT24H, Channel: NotifChannelLog,
		OccurrenceKey: OccurrenceKeyFromScheduledAt(start), SendAfter: time.Now().UTC().Add(-time.Minute),
		PayloadJSON: mustPayload(t, appt.ID, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.ClaimDueNotificationIntents(time.Now().UTC(), 10, []string{NotifChannelLog})
	if err != nil {
		t.Fatal(err)
	}
	var found *AppointmentNotificationIntent
	for i := range claimed {
		if claimed[i].ID == rem.ID {
			found = &claimed[i]
			break
		}
	}
	if found == nil || found.Status != NotifStatusProcessing {
		t.Fatalf("want claimed PROCESSING for reminder id=%d got %+v", rem.ID, found)
	}

	spy := &countingAdapter{}
	w := mustNotificationWorker(t, svc, NotificationWorkerConfig{Adapters: notificationAdapters(spy)})
	w.processClaimed(context.Background(), found, time.Now().UTC())

	got, err := svc.FindNotificationIntent(rem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != NotifStatusSkipped {
		t.Fatalf("completed appt reminder want SKIPPED got %s", got.Status)
	}
	if spy.sends != 0 {
		t.Fatalf("adapter Send must not be called, sends=%d", spy.sends)
	}
	var attempts []AppointmentNotificationAttempt
	if err := db.Where("intent_id=?", rem.ID).Order("attempt_no ASC").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("want exactly 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Error == nil || !strings.Contains(*attempts[0].Error, "appointment completed") {
		t.Fatalf("attempt error want contains appointment completed, got %+v", attempts[0].Error)
	}
}

type failAdapter struct{}

func (f *failAdapter) Channel() string      { return NotifChannelLog }
func (f *failAdapter) ProviderName() string { return "fail" }
func (f *failAdapter) Send(context.Context, *AppointmentNotificationIntent, NotificationPayload) (DeliveryResult, error) {
	return DeliveryResult{}, errors.New("adapter boom")
}

// countingAdapter records Send calls so pre-send skips can assert non-delivery.
type countingAdapter struct {
	sends int
}

func (c *countingAdapter) Channel() string      { return NotifChannelLog }
func (c *countingAdapter) ProviderName() string { return "count" }
func (c *countingAdapter) Send(context.Context, *AppointmentNotificationIntent, NotificationPayload) (DeliveryResult, error) {
	c.sends++
	return DeliveryResult{ProviderMessageID: "count"}, nil
}

func mustPayload(t *testing.T, id uint, start time.Time) string {
	t.Helper()
	_, p, err := BuildNotificationPayload(id, start, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func notificationAdapters(ads ...NotificationDeliveryAdapter) map[string]NotificationDeliveryAdapter {
	m := make(map[string]NotificationDeliveryAdapter, len(ads))
	for _, a := range ads {
		m[a.Channel()] = a
	}
	return m
}

func mustNotificationWorker(t *testing.T, svc *Service, cfg NotificationWorkerConfig) *NotificationWorker {
	t.Helper()
	w, err := NewNotificationWorker(svc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestPostgresNotificationCancelSuppressesProcessing23NB(t *testing.T) {
	db, svc, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 903, prac, start, at.ID)
	var rem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=?", appt.ID, NotifKindReminderT24H).First(&rem).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	db.Model(&rem).Updates(map[string]any{"status": NotifStatusProcessing, "processing_started_at": now})
	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "x"}, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.First(&rem, rem.ID)
	if rem.Status != NotifStatusCancelled {
		t.Fatalf("PROCESSING reminder must be suppressed, got %s", rem.Status)
	}
}

func TestPostgresNotificationNoShowSuppresses23NB(t *testing.T) {
	db, svc, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	appt := bookLife(t, svc, admin, 904, prac, start, at.ID)
	past := time.Now().UTC().Add(-2 * time.Hour)
	db.Model(&Appointment{}).Where("id=?", appt.ID).Updates(map[string]any{
		"scheduled_at": past, "scheduled_end_at": past.Add(30 * time.Minute),
	})
	if _, err := svc.MarkNoShow(appt.ID, NoShowAppointmentRequest{Reason: "ns"}, admin); err != nil {
		t.Fatal(err)
	}
	var rem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=?", appt.ID, NotifKindReminderT24H).First(&rem).Error; err != nil {
		t.Fatal(err)
	}
	if rem.Status != NotifStatusCancelled {
		t.Fatalf("no-show must suppress reminder, got %s", rem.Status)
	}
	var cancelledKind int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindCancelled).Count(&cancelledKind)
	if cancelledKind != 0 {
		t.Fatal("no-show must not enqueue CANCELLED lifecycle intent")
	}
}

func TestPostgresNotificationIdempotentBookDoesNotRearmTerminal23NB(t *testing.T) {
	db, svc, admin, prac, _, at := lifeSetup(t)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC) // Monday
	idem := "book-notif-idem-1"
	req := BookAppointmentRequest{
		PatientID: 905, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: start, IdempotencyKey: idem,
	}
	appt, reused, err := svc.BookAppointment(req, admin)
	if err != nil || reused || appt == nil {
		t.Fatalf("book: appt=%v reused=%v err=%v", appt, reused, err)
	}
	var rem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=?", appt.ID, NotifKindReminderT24H).First(&rem).Error; err != nil {
		t.Fatal(err)
	}
	var bookedBefore int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindBooked).Count(&bookedBefore)
	if bookedBefore != 1 {
		t.Fatalf("BOOKED count=%d", bookedBefore)
	}

	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "cancel-idem"}, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.First(&rem, rem.ID)
	if rem.Status != NotifStatusCancelled {
		t.Fatalf("reminder after cancel want CANCELLED got %s", rem.Status)
	}
	cancelled := mustReload(t, db, appt.ID)
	if cancelled.Status != ApptCancelled {
		t.Fatalf("appt status=%s", cancelled.Status)
	}

	again, reused2, err := svc.BookAppointment(req, admin)
	if err != nil {
		t.Fatal(err)
	}
	if !reused2 || again.ID != appt.ID {
		t.Fatalf("replay want reused same id=%d got id=%d reused=%v", appt.ID, again.ID, reused2)
	}
	if again.Status != ApptCancelled {
		t.Fatalf("replay must keep CANCELLED, got %s", again.Status)
	}
	_ = db.First(&rem, rem.ID)
	if rem.Status != NotifStatusCancelled {
		t.Fatalf("reminder must stay CANCELLED after replay, got %s", rem.Status)
	}
	var bookedAfter int64
	db.Model(&AppointmentNotificationIntent{}).Where("appointment_id=? AND kind=?", appt.ID, NotifKindBooked).Count(&bookedAfter)
	if bookedAfter != bookedBefore {
		t.Fatalf("BOOKED intents mutated on terminal replay: before=%d after=%d", bookedBefore, bookedAfter)
	}

	// NO_SHOW terminal: replay must not rearm
	idemNS := "book-notif-idem-ns"
	startNS := time.Date(2026, 11, 2, 11, 0, 0, 0, time.UTC)
	reqNS := BookAppointmentRequest{
		PatientID: 904, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: startNS, IdempotencyKey: idemNS,
	}
	nsAppt, _, err := svc.BookAppointment(reqNS, admin)
	if err != nil {
		t.Fatal(err)
	}
	var nsRem AppointmentNotificationIntent
	if err := db.Where("appointment_id=? AND kind=?", nsAppt.ID, NotifKindReminderT24H).First(&nsRem).Error; err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-2 * time.Hour)
	db.Model(&Appointment{}).Where("id=?", nsAppt.ID).Updates(map[string]any{
		"scheduled_at": past, "scheduled_end_at": past.Add(30 * time.Minute),
	})
	if _, err := svc.MarkNoShow(nsAppt.ID, NoShowAppointmentRequest{Reason: "ns"}, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.First(&nsRem, nsRem.ID)
	if nsRem.Status != NotifStatusCancelled {
		t.Fatalf("no-show reminder want CANCELLED got %s", nsRem.Status)
	}
	// Restore scheduled_at semantics for sameBookingSemantics on replay
	db.Model(&Appointment{}).Where("id=?", nsAppt.ID).Updates(map[string]any{
		"scheduled_at": startNS, "scheduled_end_at": startNS.Add(30 * time.Minute),
	})
	nsAgain, reusedNS, err := svc.BookAppointment(reqNS, admin)
	if err != nil {
		t.Fatal(err)
	}
	if !reusedNS || nsAgain.ID != nsAppt.ID {
		t.Fatalf("no-show replay reuse id=%d reused=%v", nsAgain.ID, reusedNS)
	}
	if nsAgain.Status != ApptNoShow {
		t.Fatalf("replay must keep NO_SHOW, got %s", nsAgain.Status)
	}
	_ = db.First(&nsRem, nsRem.ID)
	if nsRem.Status != NotifStatusCancelled {
		t.Fatalf("no-show reminder must stay CANCELLED after replay, got %s", nsRem.Status)
	}
}

func TestPostgresNotificationAtomicFinalization23NB(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC()
	start := time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)

	mkProcessing := func(apptID uint, key string) *AppointmentNotificationIntent {
		t.Helper()
		row, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
			AppointmentID: apptID, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
			OccurrenceKey: key, SendAfter: asOf.Add(-time.Minute),
			PayloadJSON: mustPayload(t, apptID, start),
		})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		db.Model(row).Updates(map[string]any{
			"status": NotifStatusProcessing, "processing_started_at": now, "updated_at": now,
		})
		return row
	}

	// 1) SENT finalization
	sentRow := mkProcessing(3201, "fin-sent")
	att, err := svc.FinalizeNotificationSent(sentRow.ID, "log", notifStrPtr("msg-ok"))
	if err != nil || att.AttemptNo != 1 {
		t.Fatalf("sent finalize att=%+v err=%v", att, err)
	}
	got, _ := svc.FindNotificationIntent(sentRow.ID)
	if got.Status != NotifStatusSent || got.SentAt == nil || got.ProcessingStartedAt != nil {
		t.Fatalf("SENT state=%+v", got)
	}
	var n int64
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", sentRow.ID).Count(&n)
	if n != 1 {
		t.Fatalf("sent attempts=%d", n)
	}

	// 2) SKIPPED finalization
	skipRow := mkProcessing(3202, "fin-skip")
	msg := "pre-send skip"
	att, err = svc.FinalizeNotificationSkipped(skipRow.ID, "log", &msg)
	if err != nil || att.AttemptNo != 1 {
		t.Fatalf("skip finalize att=%+v err=%v", att, err)
	}
	got, _ = svc.FindNotificationIntent(skipRow.ID)
	if got.Status != NotifStatusSkipped || got.ProcessingStartedAt != nil {
		t.Fatalf("SKIPPED state=%+v", got)
	}
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", skipRow.ID).Count(&n)
	if n != 1 {
		t.Fatalf("skip attempts=%d", n)
	}

	// 3) non-PROCESSING → conflict, no attempt
	pend, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3203, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "fin-pend", SendAfter: asOf.Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3203, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.FinalizeNotificationSent(pend.ID, "log", nil)
	if err == nil {
		t.Fatal("expected conflict on PENDING")
	}
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", pend.ID).Count(&n)
	if n != 0 {
		t.Fatalf("no attempt on conflict, got %d", n)
	}

	// 4) forced transition failure after attempt insert → full rollback
	force := mkProcessing(3204, "fin-force")
	_, err = svc.finalizeProcessingDelivery(force.ID, "log", nil, nil, func(tx *gorm.DB, parent *AppointmentNotificationIntent, _ int, _ time.Time) error {
		return coreerrors.Internal("forced transition failure")
	})
	if err == nil {
		t.Fatal("expected forced failure")
	}
	got, _ = svc.FindNotificationIntent(force.ID)
	if got.Status != NotifStatusProcessing {
		t.Fatalf("must remain PROCESSING, got %s", got.Status)
	}
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", force.ID).Count(&n)
	if n != 0 {
		t.Fatalf("rolled back attempt, got %d", n)
	}

	// 5) failure → PENDING + backoff atomic
	retryRow := mkProcessing(3205, "fin-retry")
	failMsg := "adapter boom"
	att, err = svc.FinalizeNotificationFailure(retryRow.ID, "fail", nil, &failMsg, asOf, 0)
	if err != nil || att.AttemptNo != 1 {
		t.Fatalf("retry finalize att=%+v err=%v", att, err)
	}
	got, _ = svc.FindNotificationIntent(retryRow.ID)
	if got.Status != NotifStatusPending || got.ProcessingStartedAt != nil {
		t.Fatalf("retry PENDING=%+v", got)
	}
	if !got.SendAfter.After(asOf) {
		t.Fatalf("backoff send_after=%s", got.SendAfter)
	}
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", retryRow.ID).Count(&n)
	if n != 1 {
		t.Fatalf("retry attempts=%d", n)
	}

	// 6) max attempts → FAILED atomic
	maxRow := mkProcessing(3206, "fin-max")
	for i := 1; i <= NotificationMaxAttempts-1; i++ {
		if err := db.Create(&AppointmentNotificationAttempt{
			IntentID: maxRow.ID, AttemptNo: i, Provider: "seed", CreatedAt: asOf,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	att, err = svc.FinalizeNotificationFailure(maxRow.ID, "fail", nil, &failMsg, asOf, 0)
	if err != nil || att.AttemptNo != NotificationMaxAttempts {
		t.Fatalf("max finalize att=%+v err=%v", att, err)
	}
	got, _ = svc.FindNotificationIntent(maxRow.ID)
	if got.Status != NotifStatusFailed || got.ProcessingStartedAt != nil {
		t.Fatalf("FAILED=%+v", got)
	}
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", maxRow.ID).Count(&n)
	if n != int64(NotificationMaxAttempts) {
		t.Fatalf("max attempts count=%d", n)
	}
}

func TestPostgresNotificationStaleRecoveryConcurrency23NB(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	if err := EnsureNotificationIndexes(db); err != nil {
		t.Fatal(err)
	}
	asOf := time.Now().UTC().Truncate(time.Microsecond)
	start := time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC)

	// Fresh PROCESSING must not be acquired.
	fresh, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3101, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "fresh-lease", SendAfter: asOf.Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3101, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	freshLease := asOf.Add(-time.Minute) // still within stale TTL
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", fresh.ID).Updates(map[string]any{
		"status": NotifStatusProcessing, "processing_started_at": freshLease, "updated_at": freshLease,
	})

	stale, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3102, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "stale-lease", SendAfter: asOf.Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3102, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	staleLease := asOf.Add(-NotificationStaleProcessing - time.Minute)
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", stale.ID).Updates(map[string]any{
		"status": NotifStatusProcessing, "processing_started_at": staleLease, "updated_at": staleLease,
	})

	// Atomic acquisition refreshes processing_started_at before commit.
	cutoff := asOf.Add(-NotificationStaleProcessing)
	acquired, err := svc.acquireStaleProcessingLeases(asOf, cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(acquired) != 1 || acquired[0] != stale.ID {
		t.Fatalf("acquire want [%d] got %v", stale.ID, acquired)
	}
	var afterAcq AppointmentNotificationIntent
	if err := db.First(&afterAcq, stale.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterAcq.ProcessingStartedAt == nil || !afterAcq.ProcessingStartedAt.Equal(asOf) {
		t.Fatalf("lease refresh want %s got %v", asOf, afterAcq.ProcessingStartedAt)
	}
	if afterAcq.Status != NotifStatusProcessing {
		t.Fatalf("still PROCESSING after acquire, got %s", afterAcq.Status)
	}
	// Non-stale untouched
	var freshRow AppointmentNotificationIntent
	_ = db.First(&freshRow, fresh.ID)
	if freshRow.Status != NotifStatusProcessing || freshRow.ProcessingStartedAt == nil || !freshRow.ProcessingStartedAt.Equal(freshLease) {
		t.Fatalf("fresh PROCESSING must be untouched: %+v", freshRow)
	}
	// Second acquire with same cutoff gets nothing (lease no longer stale).
	again, err := svc.acquireStaleProcessingLeases(asOf, cutoff, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("second acquire must be empty, got %v", again)
	}
	// Roll status back to stale for full Recover + concurrency (re-stale the lease).
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", stale.ID).Updates(map[string]any{
		"status": NotifStatusProcessing, "processing_started_at": staleLease, "updated_at": staleLease,
	})

	var wg sync.WaitGroup
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, e := svc.RecoverStaleProcessingClaims(asOf, 10)
			ch <- result{n: n, err: e}
		}()
	}
	wg.Wait()
	close(ch)
	total := 0
	for r := range ch {
		if r.err != nil {
			t.Fatalf("recover: %v", r.err)
		}
		total += r.n
	}
	if total != 1 {
		t.Fatalf("exactly one recovery consumer, got total n=%d", total)
	}
	var att int64
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", stale.ID).Count(&att)
	if att != 1 {
		t.Fatalf("exactly one recovery attempt, got %d", att)
	}
	got, _ := svc.FindNotificationIntent(stale.ID)
	if got.Status != NotifStatusPending {
		t.Fatalf("want PENDING with backoff, got %s", got.Status)
	}
	if !got.SendAfter.After(asOf) {
		t.Fatalf("backoff send_after should be after asOf, got %s", got.SendAfter)
	}

	// Max-attempt boundary: 4 prior attempts + 1 stale recovery → FAILED
	maxRow, err := svc.EnqueueNotificationIntent(EnqueueNotificationIntentInput{
		AppointmentID: 3103, PatientID: 1, Kind: NotifKindBooked, Channel: NotifChannelLog,
		OccurrenceKey: "stale-max", SendAfter: asOf.Add(-time.Minute),
		PayloadJSON: mustPayload(t, 3103, start),
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= NotificationMaxAttempts-1; i++ {
		if err := db.Create(&AppointmentNotificationAttempt{
			IntentID: maxRow.ID, AttemptNo: i, Provider: "seed", CreatedAt: asOf,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Model(&AppointmentNotificationIntent{}).Where("id=?", maxRow.ID).Updates(map[string]any{
		"status": NotifStatusProcessing, "processing_started_at": staleLease, "updated_at": staleLease,
	})
	n, err := svc.RecoverStaleProcessingClaims(asOf, 10)
	if err != nil || n != 1 {
		t.Fatalf("max recover n=%d err=%v", n, err)
	}
	maxGot, _ := svc.FindNotificationIntent(maxRow.ID)
	if maxGot.Status != NotifStatusFailed {
		t.Fatalf("max attempts want FAILED got %s", maxGot.Status)
	}
	var maxAtt int64
	db.Model(&AppointmentNotificationAttempt{}).Where("intent_id=?", maxRow.ID).Count(&maxAtt)
	if maxAtt != int64(NotificationMaxAttempts) {
		t.Fatalf("attempt count want %d got %d", NotificationMaxAttempts, maxAtt)
	}
}
