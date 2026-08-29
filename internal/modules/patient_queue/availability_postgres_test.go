package patient_queue

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestPostgresAvailabilityEngine23C(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (400,'DrAvailA'),(401,'DrAvailB'),(402,'MgrAvail') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(50,400,true,10),(51,401,true,10),(52,402,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(50,10,true),(51,10,true),(52,10,true) ON CONFLICT DO NOTHING`)

	admin := adminAccess(402)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	// Monday 2026-09-14
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 400, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "12:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	// B. multiple daily windows
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 400, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "14:00", EndTime: "16:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: 401, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "09:00", EndTime: "11:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	dur := 30
	from := day
	to := day.Add(24 * time.Hour)
	pid400 := uint(400)

	// A. practitioner working schedule -> slots
	res, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatalf("A: %v", err)
	}
	if res.Count < 8 { // 08-12 = 8 slots of 30m + afternoon
		t.Fatalf("A slots want >=8 got %d", res.Count)
	}
	morning := 0
	afternoon := 0
	for _, sl := range res.Slots {
		h := sl.StartAt.UTC().Hour()
		if h < 12 {
			morning++
		} else {
			afternoon++
		}
	}
	if morning == 0 || afternoon == 0 {
		t.Fatalf("B multi windows morning=%d afternoon=%d", morning, afternoon)
	}

	// C. appointment removes candidate slot
	doc := uint(400)
	startAppt := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	endAppt := time.Date(2026, 9, 14, 9, 30, 0, 0, time.UTC)
	appt, err := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 1, ServiceID: 10, ExpectedDoctorID: &doc,
		ScheduledAt: startAppt, ScheduledEndAt: &endAppt,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	res2, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range res2.Slots {
		if sl.StartAt.Equal(startAppt) {
			t.Fatal("C scheduled appointment must remove 09:00 slot")
		}
	}

	// D. cancelled does not block
	_ = db.Model(&Appointment{}).Where("id=?", appt.ID).Update("status", ApptCancelled)
	res3, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sl := range res3.Slots {
		if sl.StartAt.Equal(startAppt) {
			found = true
		}
	}
	if !found {
		t.Fatal("D cancelled must restore slot")
	}

	// E. negative exception removes
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 400, ServiceID: 10, Type: ExMeeting,
		StartAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	resE, _ := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	for _, sl := range resE.Slots {
		if !sl.StartAt.Before(time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)) &&
			sl.StartAt.Before(time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC)) {
			t.Fatal("E meeting must remove 10:00–11:00 slots")
		}
	}

	// F. EXTRA_AVAILABILITY Saturday
	sat := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 400, ServiceID: 10, Type: ExExtraAvailability,
		StartAt: time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	resF, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur,
		From: sat, To: sat.Add(24 * time.Hour),
	}, admin)
	if err != nil || resF.Count == 0 {
		t.Fatalf("F EXTRA want slots got %d err=%v", resF.Count, err)
	}

	// G. negative wins over EXTRA
	_, err = svc.CreateScheduleException(CreateScheduleExceptionRequest{
		PractitionerID: 400, ServiceID: 10, Type: ExBlocked,
		StartAt: time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC),
		EndAt:   time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	resG, _ := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur,
		From: sat, To: sat.Add(24 * time.Hour),
	}, admin)
	for _, sl := range resG.Slots {
		if sl.StartAt.Equal(time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)) {
			t.Fatal("G negative must win over EXTRA")
		}
	}

	// H. service-wide multiple practitioners
	resH, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	prac := map[uint]bool{}
	for _, sl := range resH.Slots {
		prac[sl.PractitionerID] = true
		if sl.PractitionerID == 0 {
			t.Fatal("anonymous slot forbidden")
		}
	}
	if !prac[400] || !prac[401] {
		t.Fatalf("H want both practitioners got %v", prac)
	}
	// deterministic sort
	for i := 1; i < len(resH.Slots); i++ {
		a, b := resH.Slots[i-1], resH.Slots[i]
		if a.StartAt.After(b.StartAt) {
			t.Fatal("sort startAt")
		}
		if a.StartAt.Equal(b.StartAt) && a.PractitionerID > b.PractitionerID {
			t.Fatal("sort practitionerId")
		}
	}

	// I. specific practitioner
	resI, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range resI.Slots {
		if sl.PractitionerID != 400 {
			t.Fatal("I leaked other practitioner")
		}
	}

	// J. service-scope IDOR
	other := scopedAccess(210, 11, "schedule.read.service")
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (210,'ScopeOther') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES (60,210,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES (60,11,true) ON CONFLICT DO NOTHING`)
	_, err = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, other)
	if statusOf(err) != 404 && statusOf(err) != 403 {
		t.Fatalf("J IDOR want 403/404 got %d %v", statusOf(err), err)
	}

	// K. practitioner/service mismatch
	_, err = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 11, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("K mismatch want 400 got %d %v", statusOf(err), err)
	}

	// L. service-scoped appointment type mismatch
	atOther, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "SVC11-ONLY", Name: "Svc11", DefaultDurationMinutes: 30, ServiceID: uintPtr(11),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, AppointmentTypeID: &atOther.ID, From: from, To: to,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("L type scope want 400 got %d %v", statusOf(err), err)
	}

	// M. legacy NULL end blocks with fallback
	legacyStart := time.Date(2026, 9, 14, 15, 0, 0, 0, time.UTC)
	leg, err := svc.CreateAppointment(CreateAppointmentRequest{
		PatientID: 2, ServiceID: 10, ExpectedDoctorID: &doc, ScheduledAt: legacyStart,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if leg.ScheduledEndAt != nil {
		t.Fatal("legacy must keep nil end")
	}
	resM, _ := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	for _, sl := range resM.Slots {
		if sl.StartAt.Equal(legacyStart) {
			t.Fatal("M legacy NULL end must block 15:00 (fallback 30m)")
		}
	}

	// N. no persisted slot tables
	var slotTables int64
	db.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('availability_slots','appointment_slots','generated_slots','doctor_slots','calendar_slots')`).Scan(&slotTables)
	if slotTables != 0 {
		t.Fatal("N persisted slot table found")
	}

	// O/P. no mutations
	var apptStatus string
	db.Raw(`SELECT status FROM patient_queue_appointments WHERE id=?`, leg.ID).Scan(&apptStatus)
	beforeSched := int64(0)
	db.Model(&StaffWorkingSchedule{}).Count(&beforeSched)
	_, _ = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	var apptStatus2 string
	db.Raw(`SELECT status FROM patient_queue_appointments WHERE id=?`, leg.ID).Scan(&apptStatus2)
	if apptStatus != apptStatus2 {
		t.Fatal("O appointment mutated")
	}
	afterSched := int64(0)
	db.Model(&StaffWorkingSchedule{}).Count(&afterSched)
	if beforeSched != afterSched {
		t.Fatal("P schedule mutated")
	}
	var exCount int64
	db.Model(&ScheduleException{}).Count(&exCount)
	_, _ = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	var exCount2 int64
	db.Model(&ScheduleException{}).Count(&exCount2)
	if exCount != exCount2 {
		t.Fatal("P exception mutated")
	}

	// Q. first available deterministic earliest
	first, err := svc.FirstAvailable(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, DurationMinutes: &dur, From: from, To: to,
	}, admin)
	if !first.StartAt.Equal(full.Slots[0].StartAt) || first.PractitionerID != full.Slots[0].PractitionerID {
		t.Fatalf("Q first=%v want %v", first, full.Slots[0])
	}

	// R. no availability
	emptyDay := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) // Tuesday, no schedule
	_, err = svc.FirstAvailable(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, DurationMinutes: &dur,
		From: emptyDay, To: emptyDay.Add(24 * time.Hour),
	}, admin)
	if statusOf(err) != 404 {
		t.Fatalf("R want 404 got %d %v", statusOf(err), err)
	}

	// Appointment type duration
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AVAIL-30", Name: "Consult", DefaultDurationMinutes: 30,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	resT, err := svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, AppointmentTypeID: &at.ID, From: from, To: to,
	}, admin)
	if err != nil || resT.Query.DurationMinutes != 30 {
		t.Fatalf("type duration %v err=%v", resT.Query.DurationMinutes, err)
	}
	badDur := 45
	_, err = svc.ComputeAvailability(AvailabilityQuery{
		ServiceID: 10, PractitionerID: &pid400, AppointmentTypeID: &at.ID, DurationMinutes: &badDur, From: from, To: to,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("contradictory duration want 400 got %d", statusOf(err))
	}
}
