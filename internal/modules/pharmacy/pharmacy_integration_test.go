package pharmacy

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type testConsultation struct {
	ID         uint `gorm:"primaryKey"`
	PatientID  uint
	DoctorName string
	Service    string
	Status     string
}

func (testConsultation) TableName() string { return "consultations" }

type testPrescription struct {
	ID                                  uint `gorm:"primaryKey"`
	ConsultationID                      uint
	PresentationID                      *uint
	MedicationName, Dosage, Form, Route string
	Quantity                            float64
	Duration, Instructions              string
	CreatedAt, UpdatedAt                time.Time
}

func (testPrescription) TableName() string { return "consultation_prescriptions" }

func pharmacyDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL pharmacie ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("pharmacy_%d", time.Now().UnixNano())
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	err = db.AutoMigrate(&auth.User{}, &patients.Patient{}, &medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{}, &testConsultation{}, &testPrescription{}, &MedicationFamily{}, &Medication{}, &MedicationPresentation{}, &PharmacyStock{}, &PharmacyBatch{}, &StockMovement{}, &PharmacyDispensation{}, &PharmacyDispensationItem{}, &PharmacyVoucher{}, &PharmacyVoucherLine{})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestVoucherMaterializationIsIdempotentAndGroupsConsultationLines(t *testing.T) {
	db := pharmacyDB(t)
	_, prescription, userID := seedPharmacy(t, db)
	var second testPrescription
	db.First(&second, prescription.ID)
	second.ID = 0
	second.Quantity = 5
	db.Create(&second)
	if err := db.Transaction(func(tx *gorm.DB) error { return MaterializeVoucher(tx, prescription.ConsultationID, &userID) }); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return MaterializeVoucher(tx, prescription.ConsultationID, &userID) }); err != nil {
		t.Fatal(err)
	}
	var vouchers, lines int64
	db.Model(&PharmacyVoucher{}).Count(&vouchers)
	db.Model(&PharmacyVoucherLine{}).Count(&lines)
	if vouchers != 1 || lines != 2 {
		t.Fatalf("vouchers=%d lines=%d", vouchers, lines)
	}
	var voucher PharmacyVoucher
	db.Preload("Lines").First(&voucher)
	if voucher.Number != fmt.Sprintf("PHB-%06d", voucher.ID) {
		t.Fatalf("numéro=%s", voucher.Number)
	}
	result, err := NewService(NewRepository(db)).GetVoucher(voucher.ID)
	if err != nil || result.Status != VoucherStatusPending || result.LineCount != 2 {
		t.Fatalf("voucher=%#v err=%v", result, err)
	}
}

func seedPharmacy(t *testing.T, db *gorm.DB) (*Service, ConsultationPrescriptionRef, uint) {
	t.Helper()
	user := auth.User{Name: "Pharmacien", Email: "lot10@test.local", PasswordHash: "x", Role: "admin", IsActive: true}
	db.Create(&user)
	patient := patients.Patient{CodePatient: "LOT10-P", NumeroDossier: "LOT10-D", Nom: "Patient"}
	db.Create(&patient)
	db.Create(&medical_records.MedicalRecord{PatientID: patient.ID, RecordNumber: "LOT10-MR"})
	consultation := testConsultation{PatientID: patient.ID, DoctorName: "Dr Test", Service: "Médecine", Status: "draft"}
	db.Create(&consultation)
	family := MedicationFamily{Code: "ANT", Name: "Antalgiques", IsActive: true}
	db.Create(&family)
	med := Medication{FamilyID: family.ID, Code: "DOL", Name: "DOLIPRANE", GenericName: "Paracétamol", IsActive: true}
	db.Create(&med)
	pres := MedicationPresentation{MedicationID: med.ID, Code: "DOL-1000", Dosage: "1000 mg", Form: "Comprimé", Route: "Orale", Unit: "unité", IsActive: true}
	db.Create(&pres)
	exp1 := time.Now().AddDate(0, 2, 0)
	exp2 := time.Now().AddDate(0, 6, 0)
	db.Create(&PharmacyStock{PresentationID: pres.ID, QuantityAvailable: 100, AlertThreshold: 20, IsStockManaged: true})
	db.Create(&PharmacyBatch{PresentationID: pres.ID, BatchNumber: "FEFO-1", QuantityReceived: 40, QuantityRemaining: 40, ExpirationDate: &exp1, IsActive: true})
	db.Create(&PharmacyBatch{PresentationID: pres.ID, BatchNumber: "FEFO-2", QuantityReceived: 60, QuantityRemaining: 60, ExpirationDate: &exp2, IsActive: true})
	cp := testPrescription{ConsultationID: consultation.ID, PresentationID: &pres.ID, MedicationName: med.Name, Dosage: pres.Dosage, Form: pres.Form, Route: pres.Route, Quantity: 10, Duration: "5 jours", Instructions: "2/j"}
	db.Create(&cp)
	return NewService(NewRepository(db)), ConsultationPrescriptionRef{ID: cp.ID, ConsultationID: consultation.ID, PresentationID: &pres.ID, Quantity: 10}, user.ID
}

func TestPrescriptionDoesNotTouchStockAndDispensationIsFEFOJWTPartialIdempotent(t *testing.T) {
	db := pharmacyDB(t)
	s, p, userID := seedPharmacy(t, db)
	var stock PharmacyStock
	db.Where("presentation_id=?", *p.PresentationID).First(&stock)
	var beforeMovements, beforeDispensations int64
	db.Model(&StockMovement{}).Count(&beforeMovements)
	db.Model(&PharmacyDispensation{}).Count(&beforeDispensations)
	if stock.QuantityAvailable != 100 || beforeMovements != 0 || beforeDispensations != 0 {
		t.Fatal("une prescription a modifié le stock")
	}
	d, err := s.CreateDispensation(CreateDispensationRequest{PresentationID: *p.PresentationID, PrescriptionID: &p.ID, Quantity: 6, IdempotencyKey: "LOT10-IDEM"}, userID)
	if err != nil {
		t.Fatal(err)
	}
	if d.DispensedByID == nil || *d.DispensedByID != userID || len(d.Items) != 1 || d.Items[0].Batch.BatchNumber != "FEFO-1" {
		t.Fatalf("dispensation=%#v", d)
	}
	again, err := s.CreateDispensation(CreateDispensationRequest{PresentationID: *p.PresentationID, PrescriptionID: &p.ID, Quantity: 6, IdempotencyKey: "LOT10-IDEM"}, userID)
	if err != nil || again.ID != d.ID {
		t.Fatal("idempotence absente")
	}
	status, _ := s.GetPrescriptionDispensationStatus(p.ID)
	if status.IsFullyDispensed || status.RemainingQuantity != 4 {
		t.Fatalf("statut partiel=%#v", status)
	}
	db.Where("presentation_id=?", *p.PresentationID).First(&stock)
	if stock.QuantityAvailable != 94 {
		t.Fatalf("stock=%v", stock.QuantityAvailable)
	}
}

func TestConcurrentDispensationNeverMakesStockNegative(t *testing.T) {
	db := pharmacyDB(t)
	s, p, userID := seedPharmacy(t, db)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateDispensation(CreateDispensationRequest{PresentationID: *p.PresentationID, PrescriptionID: &p.ID, Quantity: 80}, userID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for err := range errs {
		if err == nil {
			success++
		}
	}
	var stock PharmacyStock
	db.Where("presentation_id=?", *p.PresentationID).First(&stock)
	if success != 1 || stock.QuantityAvailable != 20 || stock.QuantityAvailable < 0 {
		t.Fatalf("success=%d stock=%v", success, stock.QuantityAvailable)
	}
}

func TestDispensationRequiresInternalPrescription(t *testing.T) {
	db := pharmacyDB(t)
	s, p, userID := seedPharmacy(t, db)
	_, err := s.CreateDispensation(CreateDispensationRequest{PresentationID: *p.PresentationID, Quantity: 1}, userID)
	if !errors.Is(err, ErrPrescriptionRequired) {
		t.Fatalf("erreur=%v", err)
	}
}

func TestAvailabilityUsesOnlyAdmissibleBatches(t *testing.T) {
	db := pharmacyDB(t)
	family := MedicationFamily{Code: "DEMO-FAM", Name: "Famille DEMO", IsActive: true}
	db.Create(&family)
	medication := Medication{FamilyID: family.ID, Code: "DEMO-MED", Name: "COMMERCIAL", GenericName: "DCI", IsActive: true}
	db.Create(&medication)
	now := time.Now()
	expired := now.AddDate(0, 0, -1)
	valid := now.AddDate(0, 2, 0)

	makePresentation := func(code string, active bool, stock float64, batches ...PharmacyBatch) MedicationPresentation {
		presentation := MedicationPresentation{MedicationID: medication.ID, Code: code, Dosage: "10 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé", Packaging: "Boîte DEMO", IsActive: active}
		db.Create(&presentation)
		db.Create(&PharmacyStock{PresentationID: presentation.ID, QuantityAvailable: stock, AlertThreshold: 10, IsStockManaged: true})
		for i := range batches {
			batches[i].PresentationID = presentation.ID
			db.Create(&batches[i])
		}
		return presentation
	}
	withoutBatch := makePresentation("NO-BATCH", true, 1200)
	withExpired := makePresentation("EXPIRED", true, 50, PharmacyBatch{BatchNumber: "EXPIRED", QuantityReceived: 50, QuantityRemaining: 50, ExpirationDate: &expired, IsActive: true})
	withValid := makePresentation("VALID", true, 8, PharmacyBatch{BatchNumber: "VALID", QuantityReceived: 8, QuantityRemaining: 8, ExpirationDate: &valid, IsActive: true})
	inactive := makePresentation("INACTIVE", false, 30, PharmacyBatch{BatchNumber: "INACTIVE", QuantityReceived: 30, QuantityRemaining: 30, ExpirationDate: &valid, IsActive: true})

	items, err := NewService(NewRepository(db)).GetPresentationAvailability()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uint]PresentationAvailabilityResponse{}
	for _, item := range items {
		byID[item.PresentationID] = item
	}
	for _, id := range []uint{withoutBatch.ID, withExpired.ID, inactive.ID} {
		if byID[id].AvailableQuantity != 0 || byID[id].StockStatus != "OUT_OF_STOCK" {
			t.Fatalf("presentation %d ne doit pas être dispensable: %#v", id, byID[id])
		}
	}
	if got := byID[withValid.ID]; got.AvailableQuantity != 8 || got.StockStatus != "LOW_STOCK" || got.CommercialName != "COMMERCIAL" || got.GenericName != "DCI" || got.Packaging != "Boîte DEMO" {
		t.Fatalf("lot valide mal exposé: %#v", got)
	}
}
