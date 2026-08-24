package organization

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func organizationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&Department{}, &Service{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDepartmentAndServiceLifecycle(t *testing.T) {
	db := organizationTestDB(t)
	svc := NewService(db)
	active := true
	d, err := svc.SaveDepartment(0, 7, DepartmentRequest{Code: "med", Name: "Médical", Active: &active})
	if err != nil || d.Code != "MED" {
		t.Fatalf("department: %#v %v", d, err)
	}
	created, err := svc.SaveService(0, 7, ServiceRequest{DepartmentID: d.ID, Code: "urg", Name: "Urgences", ServiceType: TypeEmergency, Active: &active, Clinical: true, SupportsConsultation: true})
	if err != nil || created.Code != "URG" {
		t.Fatalf("service: %#v %v", created, err)
	}
	created.Name = "Urgences adultes"
	renamed, err := svc.SaveService(created.ID, 8, ServiceRequest{DepartmentID: d.ID, Code: created.Code, Name: created.Name, ServiceType: created.ServiceType, Active: &active, Clinical: true, SupportsConsultation: true})
	if err != nil || renamed.Name != "Urgences adultes" {
		t.Fatalf("rename: %#v %v", renamed, err)
	}
	if _, err = svc.SaveService(0, 7, ServiceRequest{DepartmentID: d.ID, Code: "URG", Name: "Doublon", ServiceType: TypeClinical}); err == nil {
		t.Fatal("duplicate code accepted")
	}
}

func TestInactiveDepartmentAndServiceSelection(t *testing.T) {
	db := organizationTestDB(t)
	svc := NewService(db)
	inactive := false
	d, err := svc.SaveDepartment(0, 1, DepartmentRequest{Code: "OLD", Name: "Ancien", Active: &inactive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveService(0, 1, ServiceRequest{DepartmentID: d.ID, Code: "X", Name: "X", ServiceType: TypeOther}); err == nil {
		t.Fatal("service under inactive department accepted")
	}
	active := true
	d, _ = svc.SaveDepartment(0, 1, DepartmentRequest{Code: "MED", Name: "Médical", Active: &active})
	s, _ := svc.SaveService(0, 1, ServiceRequest{DepartmentID: d.ID, Code: "GEN", Name: "Médecine", ServiceType: TypeClinical, Active: &active, SupportsConsultation: true})
	s.Active = false
	db.Save(s)
	if _, err = ValidateService(db, s.ID, "consultation"); err == nil {
		t.Fatal("inactive service accepted")
	}
}

func TestLegacyMappingStopsOnUnknownValue(t *testing.T) {
	db := organizationTestDB(t)
	if err := db.Exec(`CREATE TABLE consultations (id integer primary key, service text, service_id integer)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO consultations(id,service) VALUES (1,'Service inventé')`).Error; err != nil {
		t.Fatal(err)
	}
	err := BackfillLegacy(db, 1)
	if err == nil || !strings.Contains(err.Error(), "Service inventé") {
		t.Fatalf("unknown legacy value not reported: %v", err)
	}
}
