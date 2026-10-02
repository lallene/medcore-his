package pharmacy

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type scopeConsultation struct {
	ID        uint `gorm:"primaryKey"`
	PatientID uint
	Service   string
	ServiceID *uint
	Status    string
}

func (scopeConsultation) TableName() string { return "consultations" }

type scopePrescription struct {
	ID             uint `gorm:"primaryKey"`
	ConsultationID uint
	PresentationID *uint
	MedicationName string
	Quantity       float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (scopePrescription) TableName() string { return "consultation_prescriptions" }

func pharmacyScopeDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL pharmacie ignorés")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("pharmacy_scope_%d", time.Now().UnixNano())
	if err = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`).Error })
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&auth.User{}, &patients.Patient{}, &scopeConsultation{}, &scopePrescription{},
		&MedicationFamily{}, &Medication{}, &MedicationPresentation{},
		&PharmacyDispensation{},
	); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE IF NOT EXISTS staff_profiles (
			id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL UNIQUE, active BOOLEAN NOT NULL DEFAULT true,
			primary_service_id BIGINT, employee_code TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS staff_service_assignments (
			id BIGSERIAL PRIMARY KEY, profile_id BIGINT NOT NULL, service_id BIGINT NOT NULL,
			active BOOLEAN NOT NULL DEFAULT true, is_primary BOOLEAN NOT NULL DEFAULT false,
			created_by BIGINT NOT NULL DEFAULT 1
		)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func pharmStaffAccess(userID uint, perms ...string) Access {
	a := Access{UserID: userID, Permissions: map[string]bool{}}
	for _, p := range perms {
		a.Permissions[p] = true
	}
	return a
}

func TestPostgresLOT28FPharmacyDispensationStatusServiceScope(t *testing.T) {
	db := pharmacyScopeDB(t)
	const medID, genID uint = 10, 11
	const userA, userB uint = 701, 702
	if err := db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id, employee_code) VALUES
		(1, ?, true, ?, 'MED-A'),
		(2, ?, true, ?, 'GEN-B')`, userA, medID, userB, genID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active, created_by) VALUES
		(1, ?, true, 1),
		(2, ?, true, 1)`, medID, genID).Error; err != nil {
		t.Fatal(err)
	}

	pA := patients.Patient{CodePatient: "PH-F28F-A", NumeroDossier: "PH-DA", Nom: "Alpha"}
	pB := patients.Patient{CodePatient: "PH-F28F-B", NumeroDossier: "PH-DB", Nom: "Beta"}
	if err := db.Create(&pA).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&pB).Error; err != nil {
		t.Fatal(err)
	}
	medSID, genSID := medID, genID
	cMedA := scopeConsultation{PatientID: pA.ID, Service: "Médecine", ServiceID: &medSID, Status: "draft"}
	cMedB := scopeConsultation{PatientID: pB.ID, Service: "Médecine", ServiceID: &medSID, Status: "draft"}
	cGen := scopeConsultation{PatientID: pA.ID, Service: "GEN", ServiceID: &genSID, Status: "draft"}
	for _, c := range []*scopeConsultation{&cMedA, &cMedB, &cGen} {
		if err := db.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	family := MedicationFamily{Code: "ANT", Name: "Antalgiques", IsActive: true}
	db.Create(&family)
	med := Medication{FamilyID: family.ID, Code: "DOL", Name: "DOLIPRANE", IsActive: true}
	db.Create(&med)
	pres := MedicationPresentation{MedicationID: med.ID, Code: "DOL-1", Dosage: "1g", Form: "cp", Route: "orale", Unit: "u", IsActive: true}
	db.Create(&pres)

	mkRx := func(consultID uint) scopePrescription {
		rx := scopePrescription{ConsultationID: consultID, PresentationID: &pres.ID, MedicationName: med.Name, Quantity: 10}
		if err := db.Create(&rx).Error; err != nil {
			t.Fatal(err)
		}
		return rx
	}
	rxMedA := mkRx(cMedA.ID)
	rxMedB := mkRx(cMedB.ID)
	rxGen := mkRx(cGen.ID)

	svc := NewService(NewRepository(db))
	accessA := pharmStaffAccess(userA, "pharmacy.dispensation.read")
	accessB := pharmStaffAccess(userB, "pharmacy.dispensation.read")

	// A — same service
	if _, err := svc.GetPrescriptionDispensationStatus(rxMedA.ID, accessA); err != nil {
		t.Fatalf("A: same-service status: %v", err)
	}
	// B/F103 — same-service cross-patient
	if _, err := svc.GetPrescriptionDispensationStatus(rxMedB.ID, accessA); err != nil {
		t.Fatalf("B/F103: same-service cross-patient: %v", err)
	}
	// C/F101 — cross-service
	_, err := svc.GetPrescriptionDispensationStatus(rxGen.ID, accessA)
	if !errors.Is(err, ErrPrescriptionNotFound) {
		t.Fatalf("C/F101: cross-service want ErrPrescriptionNotFound got %v", err)
	}
	_, err = svc.GetPrescriptionDispensationStatus(rxMedA.ID, accessB)
	if !errors.Is(err, ErrPrescriptionNotFound) {
		t.Fatalf("C: GEN→MED want not found got %v", err)
	}
	// V — nonexistent indistinguishable
	_, err = svc.GetPrescriptionDispensationStatus(999999, accessA)
	if !errors.Is(err, ErrPrescriptionNotFound) {
		t.Fatalf("V: nonexistent want not found got %v", err)
	}
}

// U — missing domain permission rejected at middleware (RBAC still required).
func TestLOT28FPharmacyDispensationStatusRequiresPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(NewService(NewRepository(pharmacyScopeDB(t))))

	deny := gin.New()
	deny.Use(func(c *gin.Context) {
		rbac.SetUser(c, 5, "staff", []string{"pharmacy.stock.read"})
		c.Next()
	})
	RegisterRoutesWithHandler(deny.Group("/api"), h)
	w := httptest.NewRecorder()
	deny.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/pharmacy/prescriptions/1/dispensation-status", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("U: missing pharmacy.dispensation.read want 403 got %d", w.Code)
	}

	allow := gin.New()
	allow.Use(func(c *gin.Context) {
		rbac.SetUser(c, 5, "staff", []string{"pharmacy.dispensation.read", "*"})
		c.Next()
	})
	RegisterRoutesWithHandler(allow.Group("/api"), h)
	w2 := httptest.NewRecorder()
	allow.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/pharmacy/prescriptions/1/dispensation-status", nil))
	if w2.Code == http.StatusForbidden {
		t.Fatal("pharmacy.dispensation.read must authorize dispensation-status")
	}
}
