package rbac

import "testing"

func has(xs []string, value string) bool {
	for _, x := range xs {
		if x == value {
			return true
		}
	}
	return false
}
func TestEffectiveStaffPermissionsAreCumulativeAndSeparated(t *testing.T) {
	director := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_MEDICAL"}, []string{"ORL", "MEDECINE_GENERALE"})
	if !has(director, "consultations.create") || !has(director, "laboratory.read") || has(director, "cash.payment.create") {
		t.Fatalf("directeur médical=%v", director)
	}
	multi := EffectiveStaffPermissions("staff", []string{"FACTURATION", "CAISSIER"}, nil)
	if !has(multi, "billing.create") || !has(multi, "cash.payment.create") {
		t.Fatalf("multi=%v", multi)
	}
	facturation := EffectiveStaffPermissions("staff", []string{"FACTURATION"}, nil)
	if has(facturation, "cash.payment.create") {
		t.Fatalf("facturation=%v", facturation)
	}
	comptable := EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
	if !has(comptable, "insurance_settlements.allocate") || has(comptable, "consultations.update") {
		t.Fatalf("comptable=%v", comptable)
	}
	if got := EffectiveStaffPermissions("admin", nil, nil); len(got) != 1 || got[0] != "*" {
		t.Fatalf("admin=%v", got)
	}
}
