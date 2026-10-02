package patient_queue

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A01/A12: patients.360.read may read a minimal active-care indicator without queue detail.
func TestLot28E_A12_ActiveTicketMinimalFor360Only(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:lot28e_queue_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Ticket{}); err != nil {
		t.Fatal(err)
	}
	_ = db.Exec(`CREATE TABLE organization_services (id INTEGER PRIMARY KEY, name TEXT)`)
	_ = db.Exec(`INSERT INTO organization_services (id, name) VALUES (1, 'Urgences')`)

	now := time.Now().UTC()
	consultID := uint(99)
	vitalID := uint(55)
	doctor := uint(7)
	tk := Ticket{
		Reference: "T-28E-MIN", PatientID: 11, ServiceID: 1, Source: SourceWalkIn,
		ArrivedAt: now, CheckedInAt: now, Stage: StageWaitingDoctor, Status: StatusActive,
		Priority: "NORMAL", FinanceStatus: "CLEAR", Version: 1, CreatedBy: 1,
		CreatedAt: now, UpdatedAt: now,
		ConsultationID: &consultID, VitalSignsID: &vitalID, DoctorTakenBy: &doctor,
	}
	if err := db.Create(&tk).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewService(db)
	a360 := Access{UserID: 3, Permissions: map[string]bool{"patients.360.read": true}}
	dto, err := svc.GetActiveTicketForPatient(11, a360)
	if err != nil {
		t.Fatalf("360 indicator: %v", err)
	}
	if dto.Reference != "T-28E-MIN" || dto.Stage != StageWaitingDoctor {
		t.Fatalf("minimal fields missing: %+v", dto)
	}
	if dto.ServiceName != "Urgences" {
		t.Fatalf("serviceName=%q", dto.ServiceName)
	}
	if dto.ConsultationID != nil {
		t.Fatal("360-only must not expose consultationId")
	}
	if dto.VitalSigns != nil || dto.VitalSignsID != nil {
		t.Fatal("360-only must not expose vitals")
	}
	if dto.DoctorTakenBy != nil || dto.DoctorTakenByName != "" {
		t.Fatal("360-only must not expose doctor identity")
	}
	if dto.Reason != "" || dto.PatientPhone != "" {
		t.Fatal("360-only must not expose reason/phone")
	}

	// Queue reader still gets enriched ticket (A01 queue path).
	aQueue := Access{UserID: 3, Permissions: map[string]bool{"queue.read.all": true}}
	full, err := svc.GetActiveTicketForPatient(11, aQueue)
	if err != nil {
		t.Fatalf("queue reader: %v", err)
	}
	if full.ConsultationID == nil || *full.ConsultationID != consultID {
		t.Fatalf("queue reader should see consultationId: %+v", full)
	}
}

func TestLot28E_A01_360AloneAuthorizedForMinimal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:lot28e_qauth_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Ticket{}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	_, err = svc.GetActiveTicketForPatient(1, Access{UserID: 1, Permissions: map[string]bool{"patients:read": true}})
	if err == nil {
		t.Fatal("patients:read alone must not authorize active-ticket")
	}
}
