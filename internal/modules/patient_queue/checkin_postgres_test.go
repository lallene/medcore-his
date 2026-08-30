package patient_queue

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

func checkinSetup(t *testing.T) (*gorm.DB, *Service, Access, uint, *AppointmentType) {
	t.Helper()
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")
	t.Setenv(EnvEarlyCheckinMinutes, "60")
	_ = EnsureTicketIndexes(db)

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (800,'DrCI'),(801,'CIActor'),(802,'NurseCI') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(80,800,true,10),(81,801,true,10),(82,802,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(80,10,true),(81,10,true),(82,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(801,'PCI1','CI','One'),(802,'PCI2','CI','Two'),(803,'PCI3','CI','Three'),
		(804,'PCI4','CI','Four'),(805,'PCI5','CI','Five'),(806,'PCI6','CI','Six') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO organization_services(id, name, code) VALUES (10,'MedA','A'),(11,'MedB','B') ON CONFLICT DO NOTHING`)

	admin := adminAccess(801)
	seedPractitionerForService(t, db, 80, 800, 10)
	seedAllDaySchedules(t, db, 800, 10)
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "CI-30", Name: "CheckIn 30", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, admin, 800, at
}

func bookNear(t *testing.T, svc *Service, admin Access, patient, prac uint, atID uint, start time.Time) *Appointment {
	t.Helper()
	appt, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: patient, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &atID, StartAt: start,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	return appt
}

func TestPostgresCheckInHappyPath23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	start := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Minute)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, start)

	tk, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if reused {
		t.Fatal("first check-in must not be reused")
	}
	if tk.Stage != StageWaitingTriage || tk.Status != StatusActive {
		t.Fatalf("ticket stage/status=%s/%s", tk.Stage, tk.Status)
	}
	if tk.PatientID != 801 || tk.ServiceID != 10 {
		t.Fatalf("patient/service=%d/%d", tk.PatientID, tk.ServiceID)
	}
	if tk.ExpectedDoctorID == nil || *tk.ExpectedDoctorID != prac {
		t.Fatalf("expected doctor=%v want %d", tk.ExpectedDoctorID, prac)
	}
	if tk.AppointmentID == nil || *tk.AppointmentID != appt.ID {
		t.Fatal("ticket→appointment link missing")
	}
	if tk.CreatedBy != admin.UserID {
		t.Fatalf("JWT actor want %d got %d", admin.UserID, tk.CreatedBy)
	}

	reloaded := mustReload(t, db, appt.ID)
	if reloaded.Status != ApptCheckedIn {
		t.Fatalf("status=%s", reloaded.Status)
	}
	if reloaded.QueueTicketID == nil || *reloaded.QueueTicketID != tk.ID {
		t.Fatal("appointment→ticket link missing")
	}

	var apptHist, qHist int64
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistCheckedIn).Count(&apptHist)
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "CHECK_IN").Count(&qHist)
	if apptHist != 1 || qHist != 1 {
		t.Fatalf("histories appt=%d queue=%d", apptHist, qHist)
	}

	var ticketCount int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&ticketCount)
	if ticketCount != 1 {
		t.Fatalf("tickets=%d", ticketCount)
	}

	// No consultation created at check-in (only if table exists in this schema).
	var hasConsult bool
	_ = db.Raw(`SELECT to_regclass('consultations') IS NOT NULL`).Scan(&hasConsult)
	if hasConsult {
		var n int64
		_ = db.Raw(`SELECT COUNT(*) FROM consultations WHERE patient_id=?`, 801).Scan(&n)
		if n != 0 {
			t.Fatalf("consultation at check-in=%d", n)
		}
	}
}

func TestPostgresCheckInIdempotent23F(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(15*time.Minute).Truncate(time.Minute))
	tk1, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	tk2, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil || !reused || tk2.ID != tk1.ID {
		t.Fatalf("idempotent reuse err=%v reused=%v ids=%d/%d", err, reused, tk1.ID, tk2.ID)
	}
	var apptHist, qHist, tickets int64
	svc.db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistCheckedIn).Count(&apptHist)
	svc.db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk1.ID, "CHECK_IN").Count(&qHist)
	svc.db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&tickets)
	if apptHist != 1 || qHist != 1 || tickets != 1 {
		t.Fatalf("dup apptHist=%d qHist=%d tickets=%d", apptHist, qHist, tickets)
	}
}

func TestPostgresCheckInIntegrityCheckedInWithoutTicket(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(10*time.Minute).Truncate(time.Minute))
	if err := db.Model(&Appointment{}).Where("id=?", appt.ID).Updates(map[string]any{
		"status": ApptCheckedIn, "queue_ticket_id": nil,
	}).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("integrity want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresCheckInLifecycleRejects23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	for i, st := range []string{ApptCancelled, ApptNoShow, ApptCompleted, ApptInProgress} {
		pid := uint(802 + i)
		_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES (?,?,?,?) ON CONFLICT DO NOTHING`,
			pid, fmt.Sprintf("PCL%d", i), "X", "Y")
		appt := bookNear(t, svc, admin, pid, prac, at.ID, time.Now().UTC().Add(time.Duration(40+i*40)*time.Minute).Truncate(time.Minute))
		if err := db.Model(&Appointment{}).Where("id=?", appt.ID).Update("status", st).Error; err != nil {
			t.Fatal(err)
		}
		_, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
		if statusOf(err) != 409 {
			t.Fatalf("%s want 409 got %d (%v)", st, statusOf(err), err)
		}
	}
}

func TestPostgresCheckInTiming23F(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	t.Setenv(EnvEarlyCheckinMinutes, "60")
	now := time.Now().UTC()

	// Too early (far future)
	far := bookNear(t, svc, admin, 801, prac, at.ID, now.Add(3*time.Hour).Truncate(time.Minute))
	if _, _, err := svc.CheckInAppointment(far.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 400 {
		t.Fatalf("too early want 400 got %d (%v)", statusOf(err), err)
	}

	// Within window
	near := bookNear(t, svc, admin, 802, prac, at.ID, now.Add(30*time.Minute).Truncate(time.Minute))
	if _, _, err := svc.CheckInAppointment(near.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); err != nil {
		t.Fatal(err)
	}

	// Late still SCHEDULED → allowed (book non-overlapping then force past)
	late := bookNear(t, svc, admin, 803, prac, at.ID, now.Add(2*time.Hour).Truncate(time.Minute))
	past := now.Add(-45 * time.Minute)
	end := past.Add(30 * time.Minute)
	if err := svc.db.Model(&Appointment{}).Where("id=?", late.ID).Updates(map[string]any{
		"scheduled_at": past, "scheduled_end_at": end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CheckInAppointment(late.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); err != nil {
		t.Fatalf("late should allow: %v", err)
	}
}

func TestPostgresCheckInFinanceGate23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	_ = db.Exec(`INSERT INTO billing_invoices(patient_id, patient_amount, status, coverage_pending) VALUES (804, 9000, 'ISSUED', false)`)
	fin, err := svc.EvaluateFinance(804)
	if err != nil || fin != FinancePaymentRequired {
		t.Fatalf("finance=%s err=%v", fin, err)
	}
	appt := bookNear(t, svc, admin, 804, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("finance block want 409 got %d", statusOf(err))
	}
	reloaded := mustReload(t, db, appt.ID)
	if reloaded.Status != ApptScheduled || reloaded.QueueTicketID != nil {
		t.Fatalf("finance failure must leave SCHEDULED unlinked: %+v", reloaded)
	}
	var n int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&n)
	if n != 0 {
		t.Fatal("no ticket on finance fail")
	}
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{
		IdentityConfirmed: true, FinanceOverride: true, FinanceOverrideNote: "23F",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if tk.FinanceOverride != true {
		t.Fatal("override flag")
	}
}

func TestPostgresWalkInIndependent23F(t *testing.T) {
	db, svc, admin, _, _ := checkinSetup(t)
	var apptBefore int64
	db.Model(&Appointment{}).Count(&apptBefore)
	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 805, ServiceID: 10, IdentityConfirmed: true, Reason: "walk-23f",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if tk.AppointmentID != nil {
		t.Fatal("walk-in must not create/link appointment")
	}
	if tk.Stage != StageWaitingTriage {
		t.Fatalf("stage=%s", tk.Stage)
	}
	var apptAfter int64
	db.Model(&Appointment{}).Count(&apptAfter)
	if apptAfter != apptBefore {
		t.Fatal("walk-in must not touch appointments")
	}
}

func TestPostgresActiveTicketCollisions23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)

	// A: walk-in active → appointment check-in rejected
	if _, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 801, ServiceID: 10, IdentityConfirmed: true,
	}, admin); err != nil {
		t.Fatal(err)
	}
	apptA := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	if _, _, err := svc.CheckInAppointment(apptA.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("walk-in collision want 409 got %d", statusOf(err))
	}
	// Free practitioner schedule for subsequent books (apptA still SCHEDULED occupying slot).
	if _, err := svc.CancelAppointment(apptA.ID, CancelAppointmentRequest{Reason: "free-slot"}, admin); err != nil {
		t.Fatal(err)
	}

	// B: appointment A active → B rejected (same patient, two SCHEDULED within early window)
	apptB1 := bookNear(t, svc, admin, 802, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	apptB2 := bookNear(t, svc, admin, 802, prac, at.ID, time.Now().UTC().Add(55*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(apptB1.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CheckInAppointment(apptB2.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("cross-appt collision want 409 got %d", statusOf(err))
	}
	// Do not reuse ticket
	var linked Appointment
	_ = db.First(&linked, apptB2.ID)
	if linked.QueueTicketID != nil {
		t.Fatal("must not link B to A's ticket")
	}
	_ = tk

	// D: completed ticket does not block
	if err := db.Model(&Ticket{}).Where("id=?", tk.ID).Updates(map[string]any{
		"status": StatusCompleted, "stage": StageCompleted,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CheckInAppointment(apptB2.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); err != nil {
		t.Fatalf("completed old ticket should not block: %v", err)
	}
}

func TestPostgresCheckInRBACAndIDOR23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (81,11,true) ON CONFLICT DO NOTHING`)
	seedPractitionerForService(t, db, 83, 800, 11)
	seedAllDaySchedules(t, db, 800, 11)

	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))

	noPerm := scopedAccess(801, 10, "appointment.cancel.service")
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, noPerm); statusOf(err) != 403 {
		t.Fatalf("no queue.checkin want 403 got %d", statusOf(err))
	}
	cancelOnly := scopedAccess(801, 10, "appointment.cancel.service", "appointment.reschedule.service")
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, cancelOnly); statusOf(err) != 403 {
		t.Fatalf("cancel/reschedule must not imply check-in want 403 got %d", statusOf(err))
	}

	cross := scopedAccess(8999, 11, "queue.checkin")
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, cross); statusOf(err) != 404 {
		t.Fatalf("cross-service want 404 got %d (%v)", statusOf(err), err)
	}

	ok := scopedAccess(801, 10, "queue.checkin")
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, ok); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresCheckInConcurrencySameAppointment23F(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))

	type result struct {
		tk     *Ticket
		reused bool
		err    error
	}
	ch := make(chan result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
			ch <- result{tk, reused, err}
		}()
	}
	wg.Wait()
	close(ch)
	var ok []*Ticket
	for r := range ch {
		if r.err != nil {
			t.Fatalf("concurrent check-in err: %v", r.err)
		}
		ok = append(ok, r.tk)
	}
	if ok[0].ID != ok[1].ID {
		t.Fatalf("want one ticket got %d and %d", ok[0].ID, ok[1].ID)
	}
	var n, hist int64
	svc.db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&n)
	svc.db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistCheckedIn).Count(&hist)
	if n != 1 || hist != 1 {
		t.Fatalf("tickets=%d hist=%d", n, hist)
	}
}

func TestPostgresCheckInVsCancelConcurrency23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))

	var ciErr, cancelErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, ciErr = svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	}()
	go func() {
		defer wg.Done()
		_, cancelErr = svc.CancelAppointment(appt.ID, CancelAppointmentRequest{Reason: "race"}, admin)
	}()
	wg.Wait()

	final := mustReload(t, db, appt.ID)
	var tickets int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&tickets)

	switch {
	case final.Status == ApptCancelled && tickets == 0 && ciErr != nil && cancelErr == nil:
		// cancel won
	case final.Status == ApptCheckedIn && tickets == 1 && ciErr == nil && cancelErr != nil:
		// check-in won
	default:
		t.Fatalf("incoherent: status=%s tickets=%d ciErr=%v cancelErr=%v", final.Status, tickets, ciErr, cancelErr)
	}
}

func TestPostgresCheckInVsNoShowConcurrency23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	past := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Minute)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(25*time.Minute).Truncate(time.Minute))
	end := past.Add(30 * time.Minute)
	if err := db.Model(&Appointment{}).Where("id=?", appt.ID).Updates(map[string]any{
		"scheduled_at": past, "scheduled_end_at": end,
	}).Error; err != nil {
		t.Fatal(err)
	}

	var ciErr, nsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, ciErr = svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	}()
	go func() {
		defer wg.Done()
		_, nsErr = svc.MarkNoShow(appt.ID, NoShowAppointmentRequest{Reason: "race"}, admin)
	}()
	wg.Wait()

	final := mustReload(t, db, appt.ID)
	var tickets int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&tickets)
	switch {
	case final.Status == ApptNoShow && tickets == 0 && ciErr != nil && nsErr == nil:
	case final.Status == ApptCheckedIn && tickets == 1 && ciErr == nil && nsErr != nil:
	default:
		t.Fatalf("incoherent: status=%s tickets=%d ci=%v ns=%v", final.Status, tickets, ciErr, nsErr)
	}
}

func TestPostgresCheckInVsRescheduleConcurrency23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	start := time.Now().UTC().Add(20 * time.Minute).Truncate(time.Minute)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, start)
	cur := mustReload(t, db, appt.ID)
	newStart := time.Now().UTC().Add(40 * time.Minute).Truncate(time.Minute)

	var ciErr, rsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, ciErr = svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	}()
	go func() {
		defer wg.Done()
		_, rsErr = svc.RescheduleAppointment(appt.ID, rsReq(cur, newStart), admin)
	}()
	wg.Wait()

	final := mustReload(t, db, appt.ID)
	var tickets int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&tickets)

	// Valid: check-in wins (CHECKED_IN + ticket, reschedule rejected)
	// or reschedule wins then check-in may succeed/fail depending on timing of new slot
	if final.Status == ApptCheckedIn {
		if tickets != 1 || ciErr != nil {
			t.Fatalf("check-in win incoherent tickets=%d ci=%v rs=%v", tickets, ciErr, rsErr)
		}
		return
	}
	if final.Status == ApptScheduled {
		if tickets != 0 {
			t.Fatal("scheduled must not have ticket after reschedule-win path without check-in")
		}
		// If check-in also succeeded somehow — impossible with SCHEDULED
		if ciErr == nil {
			t.Fatal("SCHEDULED with successful check-in")
		}
		return
	}
	t.Fatalf("unexpected status=%s tickets=%d ci=%v rs=%v", final.Status, tickets, ciErr, rsErr)
}

func TestPostgresSamePatientConcurrentCheckIn23F(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	a1 := bookNear(t, svc, admin, 806, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	a2 := bookNear(t, svc, admin, 806, prac, at.ID, time.Now().UTC().Add(50*time.Minute).Truncate(time.Minute))

	type result struct {
		err error
		id  uint
	}
	ch := make(chan result, 2)
	var wg sync.WaitGroup
	for _, id := range []uint{a1.ID, a2.ID} {
		wg.Add(1)
		go func(aid uint) {
			defer wg.Done()
			tk, _, err := svc.CheckInAppointment(aid, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
			r := result{err: err}
			if tk != nil {
				r.id = tk.ID
			}
			ch <- r
		}(id)
	}
	wg.Wait()
	close(ch)
	ok, fail := 0, 0
	for r := range ch {
		if r.err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("want 1 success 1 fail got ok=%d fail=%d", ok, fail)
	}
	var active int64
	svc.db.Model(&Ticket{}).Where("patient_id=? AND status=?", 806, StatusActive).Count(&active)
	if active != 1 {
		t.Fatalf("active tickets=%d", active)
	}
}

func TestPostgresCheckInVsWalkInConcurrency23F(t *testing.T) {
	_, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))

	var ciErr, wiErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, ciErr = svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	}()
	go func() {
		defer wg.Done()
		_, wiErr = svc.CheckInWalkIn(WalkInCheckInRequest{
			PatientID: 801, ServiceID: 10, IdentityConfirmed: true,
		}, admin)
	}()
	wg.Wait()

	var active int64
	svc.db.Model(&Ticket{}).Where("patient_id=? AND status=?", 801, StatusActive).Count(&active)
	if active != 1 {
		t.Fatalf("active=%d ci=%v wi=%v", active, ciErr, wiErr)
	}
	if (ciErr == nil) == (wiErr == nil) {
		t.Fatalf("exactly one path should succeed ci=%v wi=%v", ciErr, wiErr)
	}
}

func TestPostgresAppointmentToClinicalFlow23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	migrateClinicalFlowTables(db)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(15*time.Minute).Truncate(time.Minute))

	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Stage != StageWaitingTriage {
		t.Fatalf("stage=%s", tk.Stage)
	}
	if _, err := svc.TakeTriage(tk.ID, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, temperature_c, measured_at)
		VALUES (901,1,801,36.8,NOW()) ON CONFLICT DO NOTHING`)
	if _, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(901)}, admin); err != nil {
		t.Fatal(err)
	}
	doc := scopedAccess(800, 10, "queue.doctor.read", "queue.doctor.take")
	taken, err := svc.TakeDoctor(tk.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if taken.ConsultationID == nil {
		t.Fatal("consultation expected after TakeDoctor")
	}
	reloaded := mustReload(t, db, appt.ID)
	if reloaded.Status != ApptInProgress {
		t.Fatalf("appointment after TakeDoctor want IN_PROGRESS got %s", reloaded.Status)
	}
	completed, err := svc.Complete(taken.ID, CompleteRequest{Disposition: "DISCHARGED"}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Stage != StageCompleted {
		t.Fatalf("ticket=%s", completed.Stage)
	}
	final := mustReload(t, db, appt.ID)
	if final.Status != ApptCompleted {
		t.Fatalf("appointment want COMPLETED got %s", final.Status)
	}
}

func TestPostgresTicketAppointmentUniqueIndex23F(t *testing.T) {
	db, _, admin, prac, at := checkinSetup(t)
	svc := NewService(db)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	dup := Ticket{
		Reference: "Q-DUP-1", PatientID: 801, AppointmentID: &appt.ID, Source: SourceAppointment,
		ServiceID: 10, ArrivedAt: time.Now().UTC(), CheckedInAt: time.Now().UTC(),
		Stage: StageWaitingTriage, Status: StatusActive, Priority: PriorityNormal,
		FinanceStatus: FinanceClear, IdentityConfirmed: true, Version: 1, CreatedBy: admin.UserID,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := db.Create(&dup).Error; err == nil {
		t.Fatal("unique appointment_id must reject second ticket")
	}
	_ = tk
}
