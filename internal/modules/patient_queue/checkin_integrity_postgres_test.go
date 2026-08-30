package patient_queue

import (
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func forceTicketFields(t *testing.T, db *gorm.DB, ticketID uint, fields map[string]any) {
	t.Helper()
	if err := db.Model(&Ticket{}).Where("id=?", ticketID).Updates(fields).Error; err != nil {
		t.Fatal(err)
	}
}

func freeApptSlot(t *testing.T, db *gorm.DB, apptID uint, ticketID *uint) {
	t.Helper()
	if ticketID != nil {
		_ = db.Model(&Ticket{}).Where("id=?", *ticketID).Updates(map[string]any{
			"appointment_id": nil, "status": StatusCancelled, "stage": StageCancelled,
		})
	}
	_ = db.Model(&Appointment{}).Where("id=?", apptID).Updates(map[string]any{
		"status": ApptCancelled, "queue_ticket_id": nil,
	})
}

func TestPostgresIdempotentReuseFullLink23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}

	// I1–I4 correct relationship retry
	tk2, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil || !reused || tk2.ID != tk.ID {
		t.Fatalf("I1 reuse err=%v reused=%v ids=%d/%d", err, reused, tk.ID, tk2.ID)
	}
	var tickets, apptHist, qHist int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&tickets)
	db.Model(&AppointmentHistory{}).Where("appointment_id=? AND event_type=?", appt.ID, ApptHistCheckedIn).Count(&apptHist)
	db.Model(&History{}).Where("ticket_id=? AND event_type=?", tk.ID, "CHECK_IN").Count(&qHist)
	if tickets != 1 || apptHist != 1 || qHist != 1 {
		t.Fatalf("I2-I4 tickets=%d apptHist=%d qHist=%d", tickets, apptHist, qHist)
	}

	// I5 missing ticket
	missingID := tk.ID
	if err := db.Exec(`DELETE FROM patient_queue_history WHERE ticket_id=?`, missingID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM patient_queue_tickets WHERE id=?`, missingID).Error; err != nil {
		t.Fatal(err)
	}
	_ = db.Model(&Appointment{}).Where("id=?", appt.ID).Updates(map[string]any{
		"status": ApptCheckedIn, "queue_ticket_id": missingID,
	})
	if _, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("I5 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresIdempotentReuseMismatchAppointment23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	a6 := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk6, _, err := svc.CheckInAppointment(a6.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	other := bookNear(t, svc, admin, 802, prac, at.ID, time.Now().UTC().Add(55*time.Minute).Truncate(time.Minute))
	forceTicketFields(t, db, tk6.ID, map[string]any{"appointment_id": other.ID})
	if _, _, err := svc.CheckInAppointment(a6.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("I6 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresIdempotentReuseMismatchPatient23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	a7 := bookNear(t, svc, admin, 803, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk7, _, err := svc.CheckInAppointment(a7.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	forceTicketFields(t, db, tk7.ID, map[string]any{"patient_id": 9999})
	if _, _, err := svc.CheckInAppointment(a7.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("I7 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresIdempotentReuseMismatchService23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	a8 := bookNear(t, svc, admin, 804, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk8, _, err := svc.CheckInAppointment(a8.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	forceTicketFields(t, db, tk8.ID, map[string]any{"service_id": 11})
	if _, _, err := svc.CheckInAppointment(a8.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("I8 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresIdempotentReuseMismatchPractitioner23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	a9 := bookNear(t, svc, admin, 805, prac, at.ID, time.Now().UTC().Add(20*time.Minute).Truncate(time.Minute))
	tk9, _, err := svc.CheckInAppointment(a9.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	forceTicketFields(t, db, tk9.ID, map[string]any{"expected_doctor_id": 7777})
	if _, _, err := svc.CheckInAppointment(a9.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("I9 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestPostgresIdempotentReuseAfterTakeDoctor23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	migrateClinicalFlowTables(db)
	appt := bookNear(t, svc, admin, 801, prac, at.ID, time.Now().UTC().Add(15*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, admin); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`INSERT INTO vital_signs(id, medical_record_id, patient_id, temperature_c, measured_at)
		VALUES (902,1,801,36.9,NOW()) ON CONFLICT DO NOTHING`)
	if _, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{VitalSignsID: uintPtr(902)}, admin); err != nil {
		t.Fatal(err)
	}
	doc := scopedAccess(800, 10, "queue.doctor.read", "queue.doctor.take")
	taken, err := svc.TakeDoctor(tk.ID, TakeDoctorRequest{CreateConsultation: true}, doc)
	if err != nil {
		t.Fatal(err)
	}
	if taken.DoctorTakenBy == nil || *taken.DoctorTakenBy != 800 {
		t.Fatal("I10 TakeDoctor must set doctor_taken_by")
	}
	tk2, reused, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil || !reused || tk2.ID != tk.ID {
		t.Fatalf("I10 err=%v reused=%v", err, reused)
	}
}

func TestPostgresOrphanUniqueConflict23F(t *testing.T) {
	db, svc, admin, prac, at := checkinSetup(t)
	t.Setenv(EnvEarlyCheckinMinutes, "240")
	now := time.Now().UTC()

	appt := bookNear(t, svc, admin, 801, prac, at.ID, now.Add(20*time.Minute).Truncate(time.Minute))
	orphan := Ticket{
		Reference: "Q-ORPHAN-1", PatientID: appt.PatientID, AppointmentID: &appt.ID, Source: SourceAppointment,
		ServiceID: appt.ServiceID, ExpectedDoctorID: appt.ExpectedDoctorID,
		ArrivedAt: now, CheckedInAt: now,
		Stage: StageWaitingTriage, Status: StatusActive, Priority: PriorityNormal,
		FinanceStatus: FinanceClear, IdentityConfirmed: true, Version: 1, CreatedBy: admin.UserID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatal(err)
	}
	before := mustReload(t, db, appt.ID)
	_, _, err := svc.CheckInAppointment(appt.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if statusOf(err) != 409 {
		t.Fatalf("O1 want 409 got %d (%v)", statusOf(err), err)
	}
	after := mustReload(t, db, appt.ID)
	if after.Status != before.Status || after.QueueTicketID != nil {
		t.Fatalf("O1 appointment mutated: %+v", after)
	}
	var n int64
	db.Model(&Ticket{}).Where("appointment_id=?", appt.ID).Count(&n)
	if n != 1 {
		t.Fatalf("O1 must not create second ticket got %d", n)
	}
	freeApptSlot(t, db, appt.ID, &orphan.ID)

	// O2: completed check-in retry
	appt2 := bookNear(t, svc, admin, 802, prac, at.ID, now.Add(20*time.Minute).Truncate(time.Minute))
	tk, _, err := svc.CheckInAppointment(appt2.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	tk2, reused, err := svc.CheckInAppointment(appt2.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil || !reused || tk2.ID != tk.ID {
		t.Fatalf("O2 err=%v reused=%v", err, reused)
	}
	freeApptSlot(t, db, appt2.ID, &tk.ID)

	// O3: queue_ticket_id=T1 but unique appointment_id owned by T2
	appt3 := bookNear(t, svc, admin, 803, prac, at.ID, now.Add(20*time.Minute).Truncate(time.Minute))
	t1, _, err := svc.CheckInAppointment(appt3.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin)
	if err != nil {
		t.Fatal(err)
	}
	forceTicketFields(t, db, t1.ID, map[string]any{"appointment_id": nil})
	t2row := Ticket{
		Reference: "Q-ORPHAN-T2", PatientID: appt3.PatientID, AppointmentID: &appt3.ID, Source: SourceAppointment,
		ServiceID: appt3.ServiceID, ExpectedDoctorID: appt3.ExpectedDoctorID,
		ArrivedAt: now, CheckedInAt: now,
		Stage: StageWaitingTriage, Status: StatusActive, Priority: PriorityNormal,
		FinanceStatus: FinanceClear, IdentityConfirmed: true, Version: 1, CreatedBy: admin.UserID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&t2row).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CheckInAppointment(appt3.ID, AppointmentCheckInRequest{IdentityConfirmed: true}, admin); statusOf(err) != 409 {
		t.Fatalf("O3 want 409 got %d (%v)", statusOf(err), err)
	}
}

func TestEnsureTicketIndexesDuplicateFailsNoRepair23F(t *testing.T) {
	db := queuePostgres(t)
	_ = db.Exec(`DROP INDEX IF EXISTS ux_pq_tickets_appointment`)
	now := time.Now().UTC()
	apptID := uint(42)
	_ = db.Create(&Appointment{
		ID: apptID, PatientID: 1, ServiceID: 10, ScheduledAt: now, Status: ApptScheduled,
		CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}).Error
	for i, ref := range []string{"Q-DUP-A", "Q-DUP-B"} {
		aid := apptID
		tkt := Ticket{
			Reference: ref, PatientID: 1, AppointmentID: &aid, Source: SourceAppointment,
			ServiceID: 10, ArrivedAt: now, CheckedInAt: now, Stage: StageWaitingTriage, Status: StatusActive,
			Priority: PriorityNormal, FinanceStatus: FinanceClear, IdentityConfirmed: true, Version: 1,
			CreatedBy: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := db.Create(&tkt).Error; err != nil {
			t.Fatalf("seed dup %d: %v", i, err)
		}
	}
	var before int64
	db.Model(&Ticket{}).Where("appointment_id=?", apptID).Count(&before)
	if before != 2 {
		t.Fatalf("need 2 dups got %d", before)
	}
	err := EnsureTicketIndexes(db)
	if err == nil {
		t.Fatal("EnsureTicketIndexes must fail on duplicates")
	}
	if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "ux_pq_tickets_appointment") {
		t.Fatalf("clear error expected, got %v", err)
	}
	var after int64
	db.Model(&Ticket{}).Where("appointment_id=?", apptID).Count(&after)
	if after != 2 {
		t.Fatalf("silent repair deleted rows: before=%d after=%d", before, after)
	}
}

func TestModuleRegisterPanicsOnTicketIndexFailureContract23F(t *testing.T) {
	src, err := os.ReadFile("module.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "EnsureTicketIndexes(app.DB)") {
		t.Fatal("Module.Register must call EnsureTicketIndexes")
	}
	idx := strings.Index(body, "EnsureTicketIndexes(app.DB)")
	window := body[idx:]
	if i := strings.Index(window, "RegisterRoutes"); i > 0 {
		window = window[:i]
	}
	if !strings.Contains(window, "panic(err)") {
		t.Fatal("EnsureTicketIndexes failure must panic in Module.Register (not swallow)")
	}
}

func TestEnsureTicketIndexesInstallsAndVerifies23F(t *testing.T) {
	db := queuePostgres(t)
	_ = db.Exec(`DROP INDEX IF EXISTS ux_pq_tickets_appointment`)
	if err := EnsureTicketIndexes(db); err != nil {
		t.Fatal(err)
	}
	if err := assertTicketAppointmentUniqueIndex(db); err != nil {
		t.Fatal(err)
	}
	if err := EnsureTicketIndexes(db); err != nil {
		t.Fatal(err)
	}
}
