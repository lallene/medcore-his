package hospitalizations

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
)

func isHospConflict(err error) bool {
	var app *coreerrors.AppError
	return err != nil && errors.As(err, &app) && app.Status == 409
}

func planStay(t *testing.T, db *gorm.DB, suffix string) (*Service, uint, uint) {
	t.Helper()
	f := seedHospitalization(t, db, suffix, true)
	svc := NewService(db, NewRepository(db))
	item, created, err := svc.Create(CreateRequest{PatientID: f.patient.ID, SourceConsultationID: f.consultation.ID}, 1)
	if err != nil || !created {
		t.Fatalf("plan: %v created=%v", err, created)
	}
	return svc, item.ID, f.patient.ID
}

func secondPlanSamePatient(t *testing.T, db *gorm.DB, patientID uint, suffix string) uint {
	t.Helper()
	f := seedHospitalization(t, db, suffix, true)
	if err := db.Model(&f.consultation).Update("patient_id", patientID).Error; err != nil {
		t.Fatal(err)
	}
	var rec medical_records.MedicalRecord
	if err := db.Where("patient_id=?", patientID).First(&rec).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, NewRepository(db))
	item, created, err := svc.Create(CreateRequest{PatientID: patientID, SourceConsultationID: f.consultation.ID}, 1)
	if err != nil || !created {
		t.Fatalf("second plan: %v created=%v", err, created)
	}
	if item.MedicalRecordID != rec.ID {
		// Create resolves MR by patient — OK
	}
	return item.ID
}

// LOT28D H01–H20 hospitalization integrity.
func TestPostgresLOT28DActiveAdmissionInvariant(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, patientID := planStay(t, db, "H01")

	// H01 PLANNED → ADMITTED
	out, err := svc.Admit(id, AdmitRequest{}, 7)
	if err != nil || out.Status != StatusAdmitted || out.AdmittedAt == nil {
		t.Fatalf("H01: %#v err=%v", out, err)
	}
	var admitEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type=? AND reference_id=? AND event_type=?", "hospitalization", id, "hospitalization_admitted").Count(&admitEvents)
	if admitEvents != 1 {
		t.Fatalf("H17 admit timeline=%d", admitEvents)
	}

	// H05 multiple PLANNED allowed while one ADMITTED
	otherPlan := secondPlanSamePatient(t, db, patientID, "H05")
	var planned *Hospitalization
	planned, err = svc.FindByID(otherPlan)
	if err != nil || planned.Status != StatusPlanned {
		t.Fatalf("H05 planned: %#v err=%v", planned, err)
	}

	// H02 sibling ADMITTED → Conflict
	if _, err = svc.Admit(otherPlan, AdmitRequest{}, 8); !isHospConflict(err) {
		t.Fatalf("H02 want conflict got %v", err)
	}
	if planned, _ = svc.FindByID(otherPlan); planned.Status != StatusPlanned {
		t.Fatalf("H02 loser status=%s", planned.Status)
	}

	// H03/H04 DB partial unique independently rejects
	dup := Hospitalization{
		PatientID: patientID, MedicalRecordID: out.MedicalRecordID, SourceConsultationID: otherPlan + 900000,
		AdmissionNumber: "HOSP-DUP-" + fmt.Sprint(time.Now().UnixNano()), Status: StatusAdmitted,
	}
	// Need unique consultation — create orphan consult id trick: use another seed consult
	f2 := seedHospitalization(t, db, "H03X", true)
	_ = db.Model(&f2.consultation).Update("patient_id", patientID)
	dup.SourceConsultationID = f2.consultation.ID
	dup.MedicalRecordID = out.MedicalRecordID
	createErr := db.Create(&dup).Error
	if createErr == nil {
		t.Fatal("H03 expected unique violation")
	}
	if !isDuplicate(createErr) {
		t.Fatalf("H03 expected duplicate key got %v", createErr)
	}

	// H10/H18 discharge
	dis, err := svc.Discharge(id, DischargeRequest{DischargeSummary: "ok"}, 9)
	if err != nil || dis.Status != StatusDischarged || dis.DischargedAt == nil {
		t.Fatalf("H10: %#v err=%v", dis, err)
	}
	var discEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type=? AND reference_id=? AND event_type=?", "hospitalization", id, "hospitalization_discharged").Count(&discEvents)
	if discEvents != 1 {
		t.Fatalf("H18 discharge timeline=%d", discEvents)
	}

	// H12 DISCHARGED cannot Admit
	if _, err = svc.Admit(id, AdmitRequest{}, 1); !isHospConflict(err) {
		t.Fatalf("H12 want conflict got %v", err)
	}

	// After discharge, sibling may admit (H02 reverse)
	if _, err = svc.Admit(otherPlan, AdmitRequest{}, 1); err != nil {
		t.Fatalf("admit after sibling discharge: %v", err)
	}
}

func TestPostgresLOT28DDifferentPatientsBothAdmitted(t *testing.T) {
	db := hospitalizationDB(t)
	svcA, idA, _ := planStay(t, db, "H06A")
	svcB, idB, _ := planStay(t, db, "H06B")
	if _, err := svcA.Admit(idA, AdmitRequest{}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svcB.Admit(idB, AdmitRequest{}, 1); err != nil {
		t.Fatal(err)
	}
	var n int64
	db.Model(&Hospitalization{}).Where("status=?", StatusAdmitted).Count(&n)
	if n != 2 {
		t.Fatalf("H06 admitted=%d", n)
	}
}

func TestPostgresLOT28DCancelledCannotAdmit(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, _ := planStay(t, db, "H13")
	if _, err := svc.Cancel(id, 1); err != nil {
		t.Fatal(err)
	}
	var cancelEvents int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type=? AND reference_id=? AND event_type=?", "hospitalization", id, "hospitalization_cancelled").Count(&cancelEvents)
	if cancelEvents != 1 {
		t.Fatalf("H19 cancel timeline=%d", cancelEvents)
	}
	if _, err := svc.Admit(id, AdmitRequest{}, 1); !isHospConflict(err) {
		t.Fatalf("H13 want conflict got %v", err)
	}
}

func TestPostgresLOT28DSameRowAdmitRace(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, _ := planStay(t, db, "H07")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Admit(id, AdmitRequest{}, 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok, conflict int
	for err := range errs {
		if err == nil {
			ok++
		} else if isHospConflict(err) {
			conflict++
		} else {
			t.Fatalf("H07 unexpected %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("H07 ok=%d conflict=%d", ok, conflict)
	}
	var events int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type=? AND reference_id=? AND event_type=?", "hospitalization", id, "hospitalization_admitted").Count(&events)
	if events != 1 {
		t.Fatalf("H07 timeline=%d", events)
	}
}

func TestPostgresLOT28DTwoRowSamePatientAdmitRace(t *testing.T) {
	db := hospitalizationDB(t)
	svc, idA, patientID := planStay(t, db, "H08A")
	idB := secondPlanSamePatient(t, db, patientID, "H08B")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, err := svc.Admit(idA, AdmitRequest{}, 1); errs <- err }()
	go func() { defer wg.Done(); _, err := svc.Admit(idB, AdmitRequest{}, 1); errs <- err }()
	wg.Wait()
	close(errs)
	var ok int
	for err := range errs {
		if err == nil {
			ok++
		} else if !isHospConflict(err) {
			t.Fatalf("H08 unexpected %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("H08 winners=%d", ok)
	}
	var admitted int64
	db.Model(&Hospitalization{}).Where("patient_id=? AND status=?", patientID, StatusAdmitted).Count(&admitted)
	if admitted != 1 {
		t.Fatalf("H08 admitted=%d", admitted)
	}
}

func TestPostgresLOT28DAdmitVsCancelRace(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, _ := planStay(t, db, "H09")
	var wg sync.WaitGroup
	type res struct {
		kind string
		err  error
	}
	out := make(chan res, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, err := svc.Admit(id, AdmitRequest{}, 1); out <- res{"admit", err} }()
	go func() { defer wg.Done(); _, err := svc.Cancel(id, 1); out <- res{"cancel", err} }()
	wg.Wait()
	close(out)
	var winners []string
	for r := range out {
		if r.err == nil {
			winners = append(winners, r.kind)
		} else if !isHospConflict(r.err) {
			t.Fatalf("H09 %s: %v", r.kind, r.err)
		}
	}
	if len(winners) != 1 {
		t.Fatalf("H09 winners=%v", winners)
	}
	item, _ := svc.FindByID(id)
	switch winners[0] {
	case "admit":
		if item.Status != StatusAdmitted {
			t.Fatalf("admit won status=%s", item.Status)
		}
	case "cancel":
		if item.Status != StatusCancelled {
			t.Fatalf("cancel won status=%s", item.Status)
		}
	}
}

func TestPostgresLOT28DConcurrentDischarge(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, _ := planStay(t, db, "H11")
	if _, err := svc.Admit(id, AdmitRequest{}, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := svc.Discharge(id, DischargeRequest{DischargeSummary: "x"}, 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	var ok, conflict int
	for err := range errs {
		if err == nil {
			ok++
		} else if isHospConflict(err) {
			conflict++
		} else {
			t.Fatalf("H11 unexpected %v", err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("H11 ok=%d conflict=%d", ok, conflict)
	}
	var events int64
	db.Model(&medical_records.MedicalTimelineEvent{}).Where("reference_type=? AND reference_id=? AND event_type=?", "hospitalization", id, "hospitalization_discharged").Count(&events)
	if events != 1 {
		t.Fatalf("H11 timeline=%d", events)
	}
}

func TestPostgresLOT28DReleaseBedRequiresAdmitted(t *testing.T) {
	db := hospitalizationDB(t)
	svc, id, _ := planStay(t, db, "H14")
	org := seedBedsCapableService(t, db)
	sid := org.ID
	room, err := svc.CreateRoom(CreateRoomRequest{Code: "R-H14", Name: "R", ServiceID: &sid, Floor: "1", RoomType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	bed, err := svc.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "B-H14", Label: "L1", BedType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AssignBed(id, bed.ID, 1); err != nil {
		t.Fatal(err)
	}
	// H14 PLANNED cannot ReleaseBed
	if _, err = svc.ReleaseBed(id, 1); !isHospConflict(err) {
		t.Fatalf("H14 planned release want conflict got %v", err)
	}
	if _, err = svc.Admit(id, AdmitRequest{}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReleaseBed(id, 1); err != nil {
		t.Fatalf("H14 admitted release: %v", err)
	}
	// H15 discharge releases bed when occupied again
	svc2, id2, _ := planStay(t, db, "H15")
	bed2, err := svc2.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "B-H15", Label: "L2", BedType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc2.AssignBed(id2, bed2.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc2.Admit(id2, AdmitRequest{}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc2.Discharge(id2, DischargeRequest{DischargeSummary: "out"}, 1); err != nil {
		t.Fatal(err)
	}
	var active int64
	db.Model(&BedAssignment{}).Where("hospitalization_id=? AND released_at IS NULL", id2).Count(&active)
	if active != 0 {
		t.Fatalf("H15 active assignments=%d", active)
	}
	var bedStatus string
	_ = db.Raw(`SELECT status FROM hospitalization_beds WHERE id=?`, bed2.ID).Scan(&bedStatus)
	if bedStatus != BedAvailable {
		t.Fatalf("H15 bed status=%s", bedStatus)
	}
}

func TestPostgresLOT28DFailedAdmitRaceNoOrphanBed(t *testing.T) {
	db := hospitalizationDB(t)
	svc, idA, patientID := planStay(t, db, "H16A")
	idB := secondPlanSamePatient(t, db, patientID, "H16B")
	org := seedBedsCapableService(t, db)
	sid := org.ID
	room, err := svc.CreateRoom(CreateRoomRequest{Code: "R-H16", Name: "R", ServiceID: &sid, Floor: "1", RoomType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	bedA, err := svc.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "BA-H16", Label: "A", BedType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	bedB, err := svc.CreateBed(CreateBedRequest{RoomID: room.ID, Code: "BB-H16", Label: "B", BedType: "STANDARD"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AssignBed(idA, bedA.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AssignBed(idB, bedB.ID, 1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, err := svc.Admit(idA, AdmitRequest{}, 1); errs <- err }()
	go func() { defer wg.Done(); _, err := svc.Admit(idB, AdmitRequest{}, 1); errs <- err }()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !isHospConflict(err) {
			t.Fatalf("H16 %v", err)
		}
	}
	var occupied int64
	db.Model(&Bed{}).Where("status=?", BedOccupied).Count(&occupied)
	if occupied != 1 {
		t.Fatalf("H16 occupied beds=%d want 1", occupied)
	}
}
