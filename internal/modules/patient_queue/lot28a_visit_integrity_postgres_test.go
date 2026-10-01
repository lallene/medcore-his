package patient_queue

import (
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
)

func TestIsActiveVisitPredicate(t *testing.T) {
	t.Parallel()
	if !IsActiveVisit(Ticket{Status: StatusActive}) {
		t.Fatal("ACTIVE must be active visit")
	}
	for _, st := range []string{StatusCancelled, StatusCompleted, StatusNoShow, StatusOnHold} {
		if IsActiveVisit(Ticket{Status: st}) {
			t.Fatalf("%s must not be active visit", st)
		}
	}
}

func TestClinicalCareStartedPredicate(t *testing.T) {
	t.Parallel()
	doc := uint(9)
	if clinicalCareStarted(Ticket{DoctorTakenBy: &doc, Stage: StageWaitingDoctor}, "") {
		// true — doctor taken
	} else {
		t.Fatal("DoctorTakenBy must mean care started")
	}
	if !clinicalCareStarted(Ticket{Stage: StageDoctorInProgress}, "") {
		t.Fatal("DOCTOR_IN_PROGRESS must mean care started")
	}
	cid := uint(1)
	if !clinicalCareStarted(Ticket{ConsultationID: &cid, Stage: StageWaitingDoctor}, consultations.ConsultationStatusInProgress) {
		t.Fatal("in_progress consultation must mean care started")
	}
	if clinicalCareStarted(Ticket{Stage: StageWaitingTriage}, "") {
		t.Fatal("pre-triage must not mean care started")
	}
	if clinicalCareStarted(Ticket{Stage: StageWaitingDoctor}, "") {
		t.Fatal("waiting doctor without take must not mean care started")
	}
}

func TestPostgresLOT28ACancelReconcilesCheckedInAppointment(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(15*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := svc.Cancel(tk.ID, CancelRequest{Reason: "lot28a-reconcile"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != StatusCancelled {
		t.Fatalf("ticket status=%s", cancelled.Status)
	}
	reloaded := mustReload(t, db, appt.ID)
	if reloaded.Status != ApptScheduled {
		t.Fatalf("appointment want SCHEDULED got %s", reloaded.Status)
	}
	if reloaded.QueueTicketID != nil {
		t.Fatal("queue_ticket_id must be cleared")
	}
	if reloaded.CheckedInAt != nil {
		t.Fatal("checked_in_at must be cleared")
	}
	var hist int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistCheckInReversed).Count(&hist)
	if hist != 1 {
		t.Fatalf("CHECK_IN_REVERSED history want 1 got %d", hist)
	}
	// Re-check-in after cancel must succeed.
	tk2, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil || reused {
		t.Fatalf("re-check-in: err=%v reused=%v", err, reused)
	}
	if tk2.ID == tk.ID {
		t.Fatal("new visit must be a new ticket after cancel")
	}
}

func TestPostgresLOT28ACancelIdempotentAndPreservesTriageVitals(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	nurse := scopedAccess(802, 10, "queue.triage.read", "queue.triage.update", "queue.cancel", "queue.checkin")
	appt := bookNear(t, svc, admin, 802, prac, at.ID, time.Now().UTC().Add(15*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, nurse); err != nil {
		t.Fatal(err)
	}
	// Link a vital without requiring medical_records module tables beyond ticket FK.
	vitalID := uint(9001)
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS vital_signs (
		id bigint PRIMARY KEY, patient_id bigint NOT NULL, consultation_id bigint)`)
	_ = db.Exec(`INSERT INTO vital_signs(id, patient_id) VALUES (?,?) ON CONFLICT DO NOTHING`, vitalID, 802)
	if _, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: &vitalID}, nurse); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Get(tk.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Ticket.VitalSignsID == nil || *detail.Ticket.VitalSignsID != vitalID {
		t.Fatal("vital link missing before cancel")
	}
	c1, err := svc.Cancel(tk.ID, CancelRequest{Reason: "pre-doctor"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := svc.Cancel(tk.ID, CancelRequest{Reason: "again"}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID != c2.ID || c2.Status != StatusCancelled {
		t.Fatalf("idempotent cancel mismatch %+v %+v", c1, c2)
	}
	var hist int64
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "CANCELLED").Count(&hist)
	if hist != 1 {
		t.Fatalf("CANCELLED history want 1 got %d", hist)
	}
	var stillVital uint
	_ = db.Raw(`SELECT id FROM vital_signs WHERE id=?`, vitalID).Scan(&stillVital)
	if stillVital != vitalID {
		t.Fatal("vitals must not be deleted on ticket cancel")
	}
	reloaded, _ := svc.Get(tk.ID, admin)
	if reloaded.Ticket.VitalSignsID == nil || *reloaded.Ticket.VitalSignsID != vitalID {
		t.Fatal("ticket vital_signs_id must be preserved")
	}
}

func TestPostgresLOT28AWalkInFinanceOverrideAudited(t *testing.T) {
	_, svc, admin, _, _ := checkinSetup(t)
	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 803, ServiceID: 10, IdentityConfirmed: true,
		FinanceOverride: true, FinanceOverrideNote: "lot28a-walkin-override", Reason: "r",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	svc.db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "FINANCE_OVERRIDE").Count(&n)
	if n != 1 {
		t.Fatalf("FINANCE_OVERRIDE history want 1 got %d", n)
	}
}

func TestPostgresLOT28AActiveVisitUniqueIndex(t *testing.T) {
	db, svc, admin, _, _ := checkinSetup(t)
	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 804, ServiceID: 10, IdentityConfirmed: true, Reason: "first",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 804, ServiceID: 10, IdentityConfirmed: true, Reason: "second",
	}, admin); statusOf(err) != 409 {
		t.Fatalf("second walk-in want 409 got %d (%v)", statusOf(err), err)
	}
	// Direct insert must be rejected by DB index.
	now := time.Now().UTC()
	dup := Ticket{
		Reference: "Q-DUP-ACTIVE", PatientID: 804, Source: SourceWalkIn, ServiceID: 10,
		ArrivedAt: now, CheckedInAt: now, Stage: StageWaitingTriage, Status: StatusActive,
		Priority: PriorityNormal, FinanceStatus: FinanceClear, CreatedBy: admin.UserID,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	dupErr := db.Create(&dup).Error
	if dupErr == nil {
		t.Fatal("expected unique violation on second ACTIVE ticket")
	}
	if !isActiveVisitUniqueViolation(dupErr) {
		t.Fatalf("want active-visit unique violation, got %v", dupErr)
	}
	// Historical tickets do not block.
	_ = db.Model(&Ticket{}).Where("id=?", tk.ID).Updates(map[string]any{
		"status": StatusCompleted, "stage": StageCompleted, "updated_at": now,
	})
	tk2, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 804, ServiceID: 10, IdentityConfirmed: true, Reason: "after-complete",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if tk2.ID == tk.ID {
		t.Fatal("new ACTIVE visit expected after COMPLETED")
	}
}

func TestPostgresLOT28AConcurrentWalkInOneActive(t *testing.T) {
	_, svc, admin, _, _ := checkinSetup(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	ids := make(chan uint, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
				PatientID: 805, ServiceID: 10, IdentityConfirmed: true, Reason: "race",
			}, admin)
			if err != nil {
				errs <- err
				return
			}
			ids <- tk.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	var okIDs []uint
	for id := range ids {
		okIDs = append(okIDs, id)
	}
	if len(okIDs) != 1 {
		t.Fatalf("want exactly 1 successful walk-in, got %d ids=%v", len(okIDs), okIDs)
	}
	for err := range errs {
		if statusOf(err) != 409 {
			t.Fatalf("losers want 409 got %d (%v)", statusOf(err), err)
		}
	}
	var active int64
	svc.db.Model(&Ticket{}).Where("patient_id=? AND status=?", 805, StatusActive).Count(&active)
	if active != 1 {
		t.Fatalf("ACTIVE tickets=%d", active)
	}
}

func TestPostgresLOT28AConcurrentScheduledCheckInIdempotent(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 806, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	var wg sync.WaitGroup
	type res struct {
		id     uint
		reused bool
		err    error
	}
	out := make(chan res, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
			r := res{reused: reused, err: err}
			if tk != nil {
				r.id = tk.ID
			}
			out <- r
		}()
	}
	wg.Wait()
	close(out)
	ids := map[uint]struct{}{}
	for r := range out {
		if r.err != nil {
			t.Fatalf("scheduled concurrent check-in err: %v", r.err)
		}
		ids[r.id] = struct{}{}
	}
	if len(ids) != 1 {
		t.Fatalf("want one ticket identity, got %d", len(ids))
	}
}

func TestPostgresLOT28AScheduledVsWalkInRace(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(25*time.Minute).Truncate(time.Minute))
	// Use patient 801 — first ensure no leftover ACTIVE from prior tests in same setup.
	_ = svc.db.Model(&Ticket{}).Where("patient_id=? AND status=?", 801, StatusActive).
		Updates(map[string]any{"status": StatusCancelled, "stage": StageCancelled})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	oks := make(chan string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
		if err != nil {
			errs <- err
			return
		}
		oks <- "appt"
	}()
	go func() {
		defer wg.Done()
		_, err := svc.CheckInWalkIn(WalkInCheckInRequest{
			PatientID: 801, ServiceID: 10, IdentityConfirmed: true, Reason: "race-wi",
		}, admin)
		if err != nil {
			errs <- err
			return
		}
		oks <- "walkin"
	}()
	wg.Wait()
	close(errs)
	close(oks)
	var winners []string
	for w := range oks {
		winners = append(winners, w)
	}
	if len(winners) != 1 {
		t.Fatalf("want exactly one winner, got %v", winners)
	}
	for err := range errs {
		if statusOf(err) != 409 {
			t.Fatalf("loser want 409 got %d (%v)", statusOf(err), err)
		}
	}
	var active int64
	svc.db.Model(&Ticket{}).Where("patient_id=? AND status=?", 801, StatusActive).Count(&active)
	if active != 1 {
		t.Fatalf("ACTIVE=%d", active)
	}
}

func TestPostgresLOT28AAppointmentCancelBlockedWithActiveQueue(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 802, prac, at.ID, time.Now().UTC().Add(30*time.Minute).Truncate(time.Minute))
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "should-fail"}, admin); statusOf(err) != 409 {
		t.Fatalf("CancelAppointment with active queue want 409 got %d (%v)", statusOf(err), err)
	}
	if _, err := svc.MarkNoShow(appt.ID, NoShowAppointmentRequest{Reason: "should-fail"}, admin); statusOf(err) != 409 {
		t.Fatalf("MarkNoShow with active queue want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresLOT28ACancelVsTakeDoctorRace(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	migrateClinicalFlowTables(db)
	nurse := scopedAccess(802, 10, "queue.triage.read", "queue.triage.update", "queue.cancel", "queue.checkin")
	doc := scopedAccess(800, 10, "queue.doctor.read", "queue.doctor.take")
	appt := bookNear(t, svc, admin, 803, prac, at.ID, time.Now().UTC().Add(18*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, nurse); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, nurse); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	cancelErr := make(chan error, 1)
	takeErr := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.Cancel(tk.ID, CancelRequest{Reason: "race-cancel"}, admin)
		cancelErr <- err
	}()
	go func() {
		defer wg.Done()
		_, err := svc.TakeDoctor(tk.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
		takeErr <- err
	}()
	wg.Wait()
	ce := <-cancelErr
	te := <-takeErr
	detail, ge := svc.Get(tk.ID, admin)
	if ge != nil {
		t.Fatal(ge)
	}
	switch {
	case ce == nil && te != nil:
		if detail.Ticket.Status != StatusCancelled {
			t.Fatalf("cancel won but ticket=%s", detail.Ticket.Status)
		}
		if detail.Ticket.ConsultationID != nil {
			t.Fatal("cancel winner must not leave new consultation linkage from take")
		}
		reloaded := mustReload(t, db, appt.ID)
		if reloaded.Status == ApptCheckedIn {
			t.Fatal("cancel winner must not leave appointment CHECKED_IN")
		}
	case te == nil && ce != nil:
		if statusOf(ce) != 409 {
			t.Fatalf("take won: cancel want 409 got %d (%v)", statusOf(ce), ce)
		}
		if detail.Ticket.Stage != StageDoctorInProgress || detail.Ticket.Status != StatusActive {
			t.Fatalf("take winner ticket=%s/%s", detail.Ticket.Status, detail.Ticket.Stage)
		}
		if detail.Ticket.ConsultationID == nil {
			t.Fatal("take winner should have consultation")
		}
		var st string
		_ = db.Raw(`SELECT status FROM consultations WHERE id=?`, *detail.Ticket.ConsultationID).Scan(&st)
		if st != consultations.ConsultationStatusInProgress {
			t.Fatalf("consultation=%s", st)
		}
	default:
		t.Fatalf("expected exactly one winner; cancelErr=%v takeErr=%v", ce, te)
	}
}

func TestPostgresLOT28AEnsureActiveIndexDuplicateFails(t *testing.T) {
	db := queuePostgres(t)
	_ = EnsureTicketIndexes(db)
	now := time.Now().UTC()
	mk := func(ref string) {
		t.Helper()
		if err := db.Create(&Ticket{
			Reference: ref, PatientID: 991, Source: SourceWalkIn, ServiceID: 10,
			ArrivedAt: now, CheckedInAt: now, Stage: StageWaitingTriage, Status: StatusActive,
			Priority: PriorityNormal, FinanceStatus: FinanceClear, CreatedBy: 1,
			CreatedAt: now, UpdatedAt: now, Version: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk("Q-ACT-1")
	_ = db.Exec(`DROP INDEX IF EXISTS ux_pq_tickets_patient_active`)
	mk("Q-ACT-2") // second ACTIVE same patient without index
	err := EnsureTicketIndexes(db)
	if err == nil {
		t.Fatal("EnsureTicketIndexes must fail when duplicate ACTIVE patients exist")
	}
}
