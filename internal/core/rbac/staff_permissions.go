package rbac

import "sort"

var StaffPhysicianPermissions = []string{
	"patients:read", "patients:create", "patients:update", "patients.360.read", "medical_records.read", "medical_records.update",
	"consultations.read", "consultations.create", "consultations.update", "hospitalizations.read", "hospitalizations.create",
	"rooms.read", "beds.read", "bed_assignments.read", "laboratory.read", "imaging.read", "pharmacy.stock.read",
	"insurance.authorization.read", "insurance.authorization.create", "billing.read",
}

var StaffFunctionPermissions = map[string][]string{
	"DIRECTEUR_MEDICAL":       {"dashboard.read", "patients:read", "patients.360.read", "medical_records.read", "consultations.read", "hospitalizations.read", "rooms.read", "beds.read", "bed_assignments.read", "laboratory.read", "imaging.read", "pharmacy.stock.read", "insurance.authorization.read", "billing.read"},
	"DIRECTEUR_ADMINISTRATIF": {"dashboard.read", "patients:read", "billing.read", "billing.tariff.read", "cash.register.read", "cash.session.read", "cash.payment.read", "cash.receipt.read", "receivables.read", "insurance_receivables.read", "insurance_settlements.read", "insurance_settlements.create", "insurance_settlements.allocate", "insurance_batches.read", "insurance_batches.create", "insurance_batches.submit", "staff.read", "staff.manage", "staff.audit.read"},
	"SAGE_FEMME":              {"patients:read", "patients.360.read", "medical_records.read", "vital_signs.create", "consultations.read", "hospitalizations.read", "beds.read", "bed_assignments.read"},
	"INFIRMIER":               {"patients:read", "patients.360.read", "medical_records.read", "vital_signs.create", "consultations.read", "hospitalizations.read", "hospitalizations.update", "beds.read", "bed_assignments.read", "pharmacy.dispensation.read"},
	"AIDE_SOIGNANT":           {"patients:read", "patients.360.read", "medical_records.read", "hospitalizations.read", "beds.read", "bed_assignments.read"},
	"BIOLOGISTE":              {"patients:read", "patients.360.read", "laboratory.read", "laboratory.collect", "laboratory.process", "laboratory.result.write", "laboratory.validate"},
	"RADIOLOGIE":              {"patients:read", "patients.360.read", "imaging.read", "imaging.schedule", "imaging.perform", "imaging.report.write", "imaging.validate"},
	"CAISSIER":                {"billing.read", "cash.register.read", "cash.session.read", "cash.session.open", "cash.session.close", "cash.payment.read", "cash.payment.create", "cash.receipt.read"},
	"FACTURATION":             {"patients:read", "billing.read", "billing.create", "billing.issue", "billing.tariff.read", "insurance.authorization.read", "receivables.read", "insurance_receivables.read", "insurance_batches.read", "insurance_batches.create", "insurance_batches.submit"},
	"COMPTABLE":               {"billing.read", "billing.tariff.read", "cash.register.read", "cash.session.read", "cash.payment.read", "cash.receipt.read", "receivables.read", "insurance_receivables.read", "insurance_receivables.followup", "insurance_settlements.read", "insurance_settlements.create", "insurance_settlements.allocate", "insurance_batches.read"},
}

func EffectiveStaffPermissions(role string, functions, specialties []string) []string {
	if role == "admin" {
		return []string{"*"}
	}
	set := map[string]bool{}
	if role == "accueil" {
		for _, p := range []string{"patients:read", "patients:create", "patients:update", "hospitalizations.read", "rooms.read", "beds.read", "bed_assignments.read"} {
			set[p] = true
		}
	}
	for _, code := range functions {
		for _, p := range StaffFunctionPermissions[code] {
			set[p] = true
		}
	}
	if len(specialties) > 0 {
		for _, p := range StaffPhysicianPermissions {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
