package organization

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type serviceSeed struct {
	DepartmentCode, Code, Name, Type              string
	Clinical, Consultation, Hospitalization, Beds bool
}

var officialServices = []serviceSeed{
	{"MED", "URG", "Urgences", TypeEmergency, true, true, true, true},
	{"MED", "GEN", "Médecine générale", TypeClinical, true, true, true, true},
	{"MED", "CARD", "Cardiologie", TypeClinical, true, true, true, false},
	{"MED", "ORL", "ORL", TypeClinical, true, true, true, false},
	{"MED", "DIAB", "Diabétologie", TypeClinical, true, true, false, false},
	{"MED", "NEURO", "Neurologie", TypeClinical, true, true, true, false},
	{"MED", "RHUM", "Rhumatologie", TypeClinical, true, true, false, false},
	{"MED", "SOINS", "Soins", TypeClinical, true, false, true, true},
	{"SURG", "CHIR", "Chirurgie", TypeSurgical, true, true, true, true},
	{"FME", "GYNE", "Gynécologie", TypeMaternity, true, true, true, false},
	{"FME", "MAT", "Maternité", TypeMaternity, true, true, true, true},
	{"TECH", "LAB", "Laboratoire", TypeDiagnostic, true, false, false, false},
	{"TECH", "RAD", "Radiologie", TypeDiagnostic, true, false, false, false},
	{"TECH", "PHARM", "Pharmacie", TypePharmacy, false, false, false, false},
	{"ADMIN", "ADMIN", "Administration", TypeAdministrative, false, false, false, false},
	{"ADMIN", "FACT", "Facturation", TypeFinancial, false, false, false, false},
	{"ADMIN", "CASH", "Caisse", TypeFinancial, false, false, false, false},
	{"ADMIN", "COMPTA", "Comptabilité", TypeFinancial, false, false, false, false},
}

var LegacyServiceMapping = map[string]string{
	"Urgences": "URG", "Urgences LOT7": "URG",
	"Médecine": "GEN", "Médecine générale": "GEN", "Médecine LOT7": "GEN", "Consultation externe": "GEN",
	"Cardiologie": "CARD", "ORL": "ORL", "Diabétologie": "DIAB", "Neurologie": "NEURO", "Rhumatologie": "RHUM", "Soins": "SOINS",
	"Chirurgie": "CHIR", "Gynécologie": "GYNE", "Maternité": "MAT", "Laboratoire": "LAB", "Radiologie": "RAD", "Imagerie": "RAD", "Pharmacie": "PHARM",
	"Administration": "ADMIN", "Facturation": "FACT", "Caisse": "CASH", "Comptabilité": "COMPTA", "Recouvrement": "COMPTA", "Accueil": "ADMIN",
}

func SeedReference(db *gorm.DB, actor uint) (map[string]Service, error) {
	departments := []Department{{Code: "MED", Name: "Pôle médical", SortOrder: 10}, {Code: "SURG", Name: "Pôle chirurgical", SortOrder: 20}, {Code: "FME", Name: "Pôle femme / mère / enfant", SortOrder: 30}, {Code: "TECH", Name: "Pôle médico-technique", SortOrder: 40}, {Code: "ADMIN", Name: "Pôle administratif / financier", SortOrder: 50}}
	departmentIDs := map[string]uint{}
	for i := range departments {
		d := departments[i]
		d.Active = true
		d.CreatedBy = actor
		d.UpdatedBy = actor
		if e := db.Where("code=?", d.Code).FirstOrCreate(&d).Error; e != nil {
			return nil, e
		}
		departmentIDs[d.Code] = d.ID
	}
	result := map[string]Service{}
	for i, spec := range officialServices {
		x := Service{DepartmentID: departmentIDs[spec.DepartmentCode], Code: spec.Code, Name: spec.Name, ShortName: spec.Code, ServiceType: spec.Type, Active: true, Clinical: spec.Clinical, SupportsConsultation: spec.Consultation, SupportsHospitalization: spec.Hospitalization, SupportsBeds: spec.Beds, SortOrder: (i + 1) * 10, CreatedBy: actor, UpdatedBy: actor}
		if e := db.Where("code=?", x.Code).Assign(map[string]any{"department_id": x.DepartmentID, "name": x.Name, "short_name": x.ShortName, "service_type": x.ServiceType, "clinical": x.Clinical, "supports_consultation": x.SupportsConsultation, "supports_hospitalization": x.SupportsHospitalization, "supports_beds": x.SupportsBeds, "sort_order": x.SortOrder, "updated_by": actor}).FirstOrCreate(&x).Error; e != nil {
			return nil, e
		}
		result[x.Code] = x
	}
	return result, nil
}

func unknownLegacy(db *gorm.DB, table, column string) ([]string, error) {
	var values []string
	if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, column) {
		return values, nil
	}
	if e := db.Table(table).Distinct(column).Where(column+" IS NOT NULL AND TRIM("+column+")<>''").Pluck(column, &values).Error; e != nil {
		return nil, e
	}
	out := []string{}
	for _, v := range values {
		if _, ok := LegacyServiceMapping[strings.TrimSpace(v)]; !ok && v != "Service DEMO" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out, nil
}

func BackfillLegacy(db *gorm.DB, actor uint) error {
	services, e := SeedReference(db, actor)
	if e != nil {
		return e
	}
	for _, target := range [][2]string{{"consultations", "service"}, {"hospitalizations", "department"}, {"hospitalization_rooms", "department"}, {"staff_profiles", "primary_department"}} {
		unknown, e := unknownLegacy(db, target[0], target[1])
		if e != nil {
			return e
		}
		if len(unknown) > 0 {
			return fmt.Errorf("valeurs service legacy inconnues dans %s.%s: %s", target[0], target[1], strings.Join(unknown, ", "))
		}
	}
	update := func(table, column, idColumn string) error {
		if !db.Migrator().HasColumn(table, idColumn) {
			return nil
		}
		for legacy, code := range LegacyServiceMapping {
			if e := db.Table(table).Where(column+"=? AND "+idColumn+" IS NULL", legacy).Update(idColumn, services[code].ID).Error; e != nil {
				return e
			}
		}
		return nil
	}
	if e = update("consultations", "service", "service_id"); e != nil {
		return e
	}
	if e = update("hospitalizations", "department", "service_id"); e != nil {
		return e
	}
	if e = update("hospitalization_rooms", "department", "service_id"); e != nil {
		return e
	}
	if e = update("staff_profiles", "primary_department", "primary_service_id"); e != nil {
		return e
	}
	if db.Migrator().HasTable("staff_profiles") {
		var profiles []struct {
			ID               uint
			PrimaryServiceID *uint
			EmployeeCode     string
		}
		if e = db.Table("staff_profiles").Select("id,primary_service_id,employee_code").Where("primary_service_id IS NOT NULL").Scan(&profiles).Error; e != nil {
			return e
		}
		for _, p := range profiles {
			x := StaffServiceAssignment{ProfileID: p.ID, ServiceID: *p.PrimaryServiceID, IsPrimary: true, Active: true, CreatedBy: actor, CreatedAt: time.Now()}
			if e = db.Where("profile_id=? AND service_id=?", p.ID, *p.PrimaryServiceID).FirstOrCreate(&x).Error; e != nil {
				return e
			}
			if p.EmployeeCode == "DEMO-DIRECTEUR-MEDICAL" {
				secondary := StaffServiceAssignment{ProfileID: p.ID, ServiceID: services["GEN"].ID, Active: true, CreatedBy: actor, CreatedAt: time.Now()}
				if e = db.Where("profile_id=? AND service_id=?", p.ID, secondary.ServiceID).FirstOrCreate(&secondary).Error; e != nil {
					return e
				}
			}
		}
	}
	if db.Migrator().HasColumn("laboratory_orders", "requesting_service_id") {
		if e = db.Exec("UPDATE laboratory_orders o SET requesting_service_id=c.service_id, executing_service_id=? FROM consultations c WHERE c.id=o.consultation_id AND (o.requesting_service_id IS NULL OR o.executing_service_id IS NULL)", services["LAB"].ID).Error; e != nil {
			return e
		}
	}
	if db.Migrator().HasColumn("imaging_orders", "requesting_service_id") {
		if e = db.Exec("UPDATE imaging_orders o SET requesting_service_id=c.service_id, executing_service_id=? FROM consultations c WHERE c.id=o.consultation_id AND (o.requesting_service_id IS NULL OR o.executing_service_id IS NULL)", services["RAD"].ID).Error; e != nil {
			return e
		}
	}
	if db.Migrator().HasColumn("insurance_authorizations", "service_id") {
		queries := map[string]string{"CONSULTATION": "consultations", "LABORATORY": "laboratory_orders", "IMAGING": "imaging_orders", "HOSPITALIZATION": "hospitalizations"}
		for typ, table := range queries {
			col := "service_id"
			if typ == "LABORATORY" || typ == "IMAGING" {
				col = "requesting_service_id"
			}
			if e = db.Exec(fmt.Sprintf("UPDATE insurance_authorizations a SET service_id=x.%s FROM %s x WHERE a.reference_type=? AND a.reference_id=x.id AND a.service_id IS NULL", col, table), typ).Error; e != nil {
				return e
			}
		}
		for legacy, code := range LegacyServiceMapping {
			if e = db.Table("insurance_authorizations").Where("service=? AND service_id IS NULL", legacy).Update("service_id", services[code].ID).Error; e != nil {
				return e
			}
		}
	}
	if db.Migrator().HasColumn("cash_registers", "service_id") {
		db.Table("cash_registers").Where("code=?", "DEMO-CAISSE-URGENCES").Update("service_id", services["URG"].ID)
		db.Table("cash_registers").Where("code=?", "DEMO-CAISSE-PRINCIPALE").Update("service_id", services["CASH"].ID)
	}
	return nil
}
