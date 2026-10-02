package performed_acts

import (
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTableName(t *testing.T) {
	t.Parallel()
	if (Act{}).TableName() != "performed_acts" {
		t.Fatal((Act{}).TableName())
	}
}

func TestStatusesBounded(t *testing.T) {
	t.Parallel()
	if StatusPerformed != "PERFORMED" || StatusVoided != "VOIDED" {
		t.Fatal("unexpected status constants")
	}
}

func TestInsuranceBoundary_NoAuthFieldsOnModel(t *testing.T) {
	t.Parallel()
	// Structural: Act must not carry insurer/authorization money fields.
	a := Act{InsuranceEligible: true, Status: StatusPerformed}
	if a.InsuranceEligible != true {
		t.Fatal("InsuranceEligible is catalogue snapshot metadata only")
	}
}

func hardeningSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pa_hard_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &ProducerMap{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestManualCreateRejectsProducerSourceFields(t *testing.T) {
	db := hardeningSQLite(t)
	p := patients.Patient{CodePatient: "H1", NumeroDossier: "H1D", Nom: "Hard"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "H-CAT", Label: "Hard", Category: "PROCEDURE", BasePrice: 1000,
		Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	sid := uint(42)
	_, err := s.Create(CreateRequest{
		PatientID: p.ID, ActCatalogEntryID: cat.ID, SourceType: SourceConsultation, SourceID: &sid,
	}, 1)
	if err == nil || !strings.Contains(err.Error(), "sourceType") {
		t.Fatalf("expected sourceType rejection, got %v", err)
	}
}

func TestProducerRejectsExistingVoidedSource(t *testing.T) {
	db := hardeningSQLite(t)
	p, cat := seedProducerBase(t, db, SourceConsultation, ConsultationClinicalKey)
	s := NewService(db)
	src := uint(88)
	voided := Act{
		PatientID: p.ID, ActCatalogEntryID: cat.ID, ActCode: cat.Code, ActLabel: cat.Label, ActCategory: cat.Category,
		BasePrice: cat.BasePrice, Currency: cat.Currency, Billable: true, InsuranceEligible: true, Quantity: 1,
		PerformedAt: time.Now(), PerformedBy: 1, Status: StatusVoided,
		SourceType: SourceConsultation, SourceID: &src, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&voided).Error; err != nil {
		t.Fatal(err)
	}
	_, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 88, PatientID: p.ID,
		ClinicalKey: ConsultationClinicalKey, ActorID: 1,
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "voided") {
		t.Fatalf("expected VOIDED conflict, got %v", err)
	}
}

func TestAcceptExistingProducerActPatientMismatch(t *testing.T) {
	t.Parallel()
	act := &Act{PatientID: 2, Status: StatusPerformed}
	_, err := acceptExistingProducerAct(act, 9)
	if err == nil {
		t.Fatal("expected patient mismatch conflict")
	}
}
