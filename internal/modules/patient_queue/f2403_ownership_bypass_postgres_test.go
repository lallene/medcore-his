package patient_queue

import "testing"

// LOT 24B F24-03 — queue.read.all must NOT bypass mutation ownership.
//
// Production defect (do not fix in this RED phase):
//   CompleteTriage          — service.go treats queue.read.all like "*" for TriageTakenBy
//   assertDoctorCanComplete — service.go returns nil when queue.read.all for DoctorTakenBy
//
// Locked contract: only "*" may bypass TriageTakenBy / DoctorTakenBy ownership.
// queue.read.all is a READ permission and must not authorize these mutations.

func TestPostgresF2403_ReadAllDoesNotBypassTriageCompleteOwnership(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	owner := scopedAccess(101, 10, "queue.triage.update")
	intruder := Access{
		UserID: 24031,
		Permissions: map[string]bool{
			"queue.triage.update": true,
			"queue.read.all":      true,
		},
	}
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24031,'F2403 Intruder Triage') ON CONFLICT DO NOTHING`)

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "f2403-triage-deny",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := svc.TakeTriage(tk.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if taken.TriageTakenBy == nil || *taken.TriageTakenBy != owner.UserID {
		t.Fatalf("setup TriageTakenBy=%v want %d", taken.TriageTakenBy, owner.UserID)
	}

	_, err = svc.CompleteTriage(taken.ID, CompleteTriageRequest{}, intruder)
	if err == nil {
		t.Fatal("CompleteTriage: actor with queue.triage.update+queue.read.all but not TriageTakenBy must be rejected; queue.read.all must not bypass ownership")
	}
	if statusOf(err) != 403 {
		t.Fatalf("CompleteTriage ownership mismatch want 403, got %d (%v)", statusOf(err), err)
	}

	var stage string
	if e := db.Raw(`SELECT stage FROM patient_queue_tickets WHERE id=?`, taken.ID).Scan(&stage).Error; e != nil {
		t.Fatal(e)
	}
	if stage != StageTriageInProgress {
		t.Fatalf("ticket stage must remain TRIAGE_IN_PROGRESS after denied CompleteTriage, got %s", stage)
	}
}

func TestPostgresF2403_ReadAllDoesNotBypassDoctorCompleteOwnership(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	docOwner := scopedAccess(102, 10, "queue.doctor.take", "queue.doctor.read")
	intruder := Access{
		UserID: 24032,
		Permissions: map[string]bool{
			"queue.doctor.take": true,
			"queue.read.all":    true,
		},
	}
	_ = db.Exec(`INSERT INTO users(id, name) VALUES (24032,'F2403 Intruder Doctor') ON CONFLICT DO NOTHING`)

	taken := f2403DoctorInProgressTicket(t, svc, admin, docOwner)

	_, err := svc.Complete(taken.ID, CompleteRequest{Disposition: "DISCHARGED"}, intruder)
	if err == nil {
		t.Fatal("Complete: actor with queue.doctor.take+queue.read.all but not DoctorTakenBy must be rejected; queue.read.all must not bypass ownership")
	}
	if statusOf(err) != 403 {
		t.Fatalf("Complete ownership mismatch want 403, got %d (%v)", statusOf(err), err)
	}

	var stage string
	if e := db.Raw(`SELECT stage FROM patient_queue_tickets WHERE id=?`, taken.ID).Scan(&stage).Error; e != nil {
		t.Fatal(e)
	}
	if stage != StageDoctorInProgress {
		t.Fatalf("ticket stage must remain DOCTOR_IN_PROGRESS after denied Complete, got %s", stage)
	}
}

func TestPostgresF2403_StarStillBypassesTriageAndDoctorOwnership(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	star := adminAccess(100)
	triageOwner := scopedAccess(101, 10, "queue.triage.update")
	docOwner := scopedAccess(102, 10, "queue.doctor.take", "queue.doctor.read")

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "f2403-star-triage",
	}, star)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, triageOwner); err != nil {
		t.Fatal(err)
	}
	done, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, star)
	if err != nil {
		t.Fatalf("* must still bypass TriageTakenBy for CompleteTriage: %v", err)
	}
	if done.Stage != StageWaitingDoctor {
		t.Fatalf("after * CompleteTriage stage=%s want WAITING_DOCTOR", done.Stage)
	}

	taken, err := svc.TakeDoctor(done.ID, TakeDoctorRequest{}, docOwner)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := svc.Complete(taken.ID, CompleteRequest{Disposition: "DISCHARGED"}, star)
	if err != nil {
		t.Fatalf("* must still bypass DoctorTakenBy for Complete: %v", err)
	}
	if completed.Stage != StageCompleted || completed.Status != StatusCompleted {
		t.Fatalf("after * Complete stage/status=%s/%s", completed.Stage, completed.Status)
	}
}

func TestPostgresF2403_OwnerStillCompletesTriageAndDoctor(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	admin := adminAccess(100)
	triageOwner := scopedAccess(101, 10, "queue.triage.update")
	docOwner := scopedAccess(102, 10, "queue.doctor.take", "queue.doctor.read")

	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "f2403-owner",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, triageOwner); err != nil {
		t.Fatal(err)
	}
	waiting, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, triageOwner)
	if err != nil {
		t.Fatalf("TriageTakenBy owner must CompleteTriage: %v", err)
	}
	if waiting.Stage != StageWaitingDoctor {
		t.Fatalf("owner CompleteTriage stage=%s want WAITING_DOCTOR", waiting.Stage)
	}

	taken, err := svc.TakeDoctor(waiting.ID, TakeDoctorRequest{}, docOwner)
	if err != nil {
		t.Fatal(err)
	}
	if taken.DoctorTakenBy == nil || *taken.DoctorTakenBy != docOwner.UserID {
		t.Fatalf("setup DoctorTakenBy=%v want %d", taken.DoctorTakenBy, docOwner.UserID)
	}
	completed, err := svc.Complete(taken.ID, CompleteRequest{Disposition: "DISCHARGED"}, docOwner)
	if err != nil {
		t.Fatalf("DoctorTakenBy owner must Complete: %v", err)
	}
	if completed.Stage != StageCompleted {
		t.Fatalf("owner Complete stage=%s want COMPLETED", completed.Stage)
	}
}

// f2403DoctorInProgressTicket advances a walk-in to DOCTOR_IN_PROGRESS owned by docOwner.
func f2403DoctorInProgressTicket(t *testing.T, svc *Service, admin, docOwner Access) *Ticket {
	t.Helper()
	tk, err := svc.CheckInWalkIn(WalkInCheckInRequest{
		PatientID: 1, ServiceID: 10, IdentityConfirmed: true, Reason: "f2403-doctor-deny",
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TakeTriage(tk.ID, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteTriage(tk.ID, CompleteTriageRequest{}, admin); err != nil {
		t.Fatal(err)
	}
	taken, err := svc.TakeDoctor(tk.ID, TakeDoctorRequest{}, docOwner)
	if err != nil {
		t.Fatal(err)
	}
	if taken.DoctorTakenBy == nil || *taken.DoctorTakenBy != docOwner.UserID {
		t.Fatalf("setup DoctorTakenBy=%v want %d", taken.DoctorTakenBy, docOwner.UserID)
	}
	if taken.Stage != StageDoctorInProgress {
		t.Fatalf("setup stage=%s want DOCTOR_IN_PROGRESS", taken.Stage)
	}
	return taken
}
