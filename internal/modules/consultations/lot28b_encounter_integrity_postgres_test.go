package consultations

import (
	"errors"
	"sync"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func TestPostgresLOT28BCompleteVsCompleteOneWinner(t *testing.T) {
	db := consultationIntegrationDB(t)
	p := patients.Patient{CodePatient: "L28B-C1", NumeroDossier: "L28B-D1", Nom: "Race"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{
		PatientID: p.ID, DoctorName: "Dr", Service: "Med",
		Status: ConsultationStatusInProgress, Version: 1,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil)
	access := unrestrictedAccess(9)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
				ExpectedVersion: 1, Status: ConsultationStatusCompleted,
			}, 9, access)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var okN, conflictN int
	for err := range errs {
		if err == nil {
			okN++
			continue
		}
		if errors.Is(err, ErrConsultationVersionConflict) || errors.Is(err, ErrInvalidTransition) {
			conflictN++
			continue
		}
		t.Fatalf("unexpected err: %v", err)
	}
	if okN != 1 || conflictN != 1 {
		t.Fatalf("want 1 success + 1 conflict, got ok=%d conflict=%d", okN, conflictN)
	}
	var status string
	var version int
	if err := db.Raw(`SELECT status, version FROM consultations WHERE id=?`, c.ID).Row().Scan(&status, &version); err != nil {
		t.Fatal(err)
	}
	if status != ConsultationStatusCompleted {
		t.Fatalf("status=%s", status)
	}
	if version != 2 {
		t.Fatalf("version want 2 got %d", version)
	}
}

func TestPostgresLOT28BUpdateVsCompleteRace(t *testing.T) {
	db := consultationIntegrationDB(t)
	p := patients.Patient{CodePatient: "L28B-C2", NumeroDossier: "L28B-D2", Nom: "Race2"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{
		PatientID: p.ID, DoctorName: "Dr", Service: "Med",
		Status: ConsultationStatusInProgress, Version: 1, Diagnosis: "before",
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil)
	access := unrestrictedAccess(11)
	diag := "after-edit"

	var wg sync.WaitGroup
	type res struct {
		kind string
		err  error
	}
	out := make(chan res, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.UpdateConsultation(c.ID, UpdateConsultationRequest{
			ExpectedVersion: 1, Diagnosis: &diag,
		}, 11, access)
		out <- res{kind: "update", err: err}
	}()
	go func() {
		defer wg.Done()
		_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
			ExpectedVersion: 1, Status: ConsultationStatusCompleted,
		}, 11, access)
		out <- res{kind: "complete", err: err}
	}()
	wg.Wait()
	close(out)

	var winners []string
	for r := range out {
		if r.err == nil {
			winners = append(winners, r.kind)
			continue
		}
		if !errors.Is(r.err, ErrConsultationVersionConflict) &&
			!errors.Is(r.err, ErrConsultationLocked) &&
			!errors.Is(r.err, ErrInvalidTransition) {
			t.Fatalf("%s unexpected: %v", r.kind, r.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("want exactly one winner, got %v", winners)
	}

	var status, diagnosis string
	var version int
	if err := db.Raw(`SELECT status, version, COALESCE(diagnosis,'') FROM consultations WHERE id=?`, c.ID).
		Row().Scan(&status, &version, &diagnosis); err != nil {
		t.Fatal(err)
	}
	if version != 2 {
		t.Fatalf("version want 2 got %d", version)
	}
	switch winners[0] {
	case "complete":
		if status != ConsultationStatusCompleted {
			t.Fatalf("complete won but status=%s", status)
		}
	case "update":
		if status != ConsultationStatusInProgress {
			t.Fatalf("update won but status=%s", status)
		}
		if diagnosis != diag {
			t.Fatalf("diagnosis=%q", diagnosis)
		}
	}
}

func TestPostgresLOT28BCancelVsCompleteRace(t *testing.T) {
	db := consultationIntegrationDB(t)
	p := patients.Patient{CodePatient: "L28B-C3", NumeroDossier: "L28B-D3", Nom: "Race3"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{
		PatientID: p.ID, DoctorName: "Dr", Service: "Med",
		Status: ConsultationStatusInProgress, Version: 1,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	// No ACTIVE queue ticket → cancel allowed.
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS patient_queue_tickets (
		id bigserial PRIMARY KEY, consultation_id bigint, appointment_id bigint, status text)`)

	svc := NewService(NewRepository(db), nil)
	access := unrestrictedAccess(12)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
			ExpectedVersion: 1, Status: ConsultationStatusCompleted,
		}, 12, access)
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
			ExpectedVersion: 1, Status: ConsultationStatusCancelled, CancellationReason: "race",
		}, 12, access)
		errs <- err
	}()
	wg.Wait()
	close(errs)
	var okN int
	for err := range errs {
		if err == nil {
			okN++
			continue
		}
		if !errors.Is(err, ErrConsultationVersionConflict) && !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("unexpected: %v", err)
		}
	}
	if okN != 1 {
		t.Fatalf("want 1 success got %d", okN)
	}
	var status string
	if err := db.Raw(`SELECT status FROM consultations WHERE id=?`, c.ID).Scan(&status).Error; err != nil {
		t.Fatal(err)
	}
	if status != ConsultationStatusCompleted && status != ConsultationStatusCancelled {
		t.Fatalf("terminal status=%s", status)
	}
}

func TestPostgresLOT28BCancelBlockedWhenActiveQueueTicket(t *testing.T) {
	db := consultationIntegrationDB(t)
	p := patients.Patient{CodePatient: "L28B-C4", NumeroDossier: "L28B-D4", Nom: "QLink"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{
		PatientID: p.ID, DoctorName: "Dr", Service: "Med",
		Status: ConsultationStatusInProgress, Version: 1,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`CREATE TABLE IF NOT EXISTS patient_queue_tickets (
		id bigserial PRIMARY KEY, consultation_id bigint, appointment_id bigint, status text)`)
	if err := db.Exec(`INSERT INTO patient_queue_tickets(consultation_id, status) VALUES (?, 'ACTIVE')`, c.ID).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil)
	_, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
		ExpectedVersion: 1, Status: ConsultationStatusCancelled, CancellationReason: "nope",
	}, 1, unrestrictedAccess(1))
	if !errors.Is(err, ErrQueueLinkedCancelBlocked) {
		t.Fatalf("want ErrQueueLinkedCancelBlocked got %v", err)
	}
}

func TestPostgresLOT28BStatusOCCAndVersionBump(t *testing.T) {
	db := consultationIntegrationDB(t)
	p := patients.Patient{CodePatient: "L28B-C5", NumeroDossier: "L28B-D5", Nom: "OCC"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	c := Consultation{
		PatientID: p.ID, DoctorName: "Dr", Service: "Med",
		Status: ConsultationStatusDraft, Version: 1,
	}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(NewRepository(db), nil)
	access := unrestrictedAccess(1)
	out, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
		ExpectedVersion: 1, Status: ConsultationStatusInProgress,
	}, 1, access)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 2 || out.Status != ConsultationStatusInProgress {
		t.Fatalf("got version=%d status=%s", out.Version, out.Status)
	}
	if _, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
		ExpectedVersion: 1, Status: ConsultationStatusCompleted,
	}, 1, access); !errors.Is(err, ErrConsultationVersionConflict) {
		t.Fatalf("stale complete want version conflict got %v", err)
	}
	out2, err := svc.UpdateStatus(c.ID, UpdateConsultationStatusRequest{
		ExpectedVersion: 2, Status: ConsultationStatusCompleted,
	}, 1, access)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Version != 3 || out2.Status != ConsultationStatusCompleted {
		t.Fatalf("got version=%d status=%s", out2.Version, out2.Status)
	}
	// Completed immutable via content update.
	diag := "x"
	if _, err := svc.UpdateConsultation(c.ID, UpdateConsultationRequest{
		ExpectedVersion: 3, Diagnosis: &diag,
	}, 1, access); !errors.Is(err, ErrConsultationLocked) {
		t.Fatalf("want locked got %v", err)
	}
}
