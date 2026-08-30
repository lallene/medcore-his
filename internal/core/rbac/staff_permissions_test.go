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

// LOT 23G.1 — scheduling read least-privilege packs (catalog authority).
func TestSchedulingReadLeastPrivilegePacks(t *testing.T) {
	accueil := EffectiveStaffPermissions("staff", []string{"ACCUEIL"}, nil)
	if !has(accueil, "schedule.read.service") {
		t.Fatalf("ACCUEIL missing schedule.read.service: %v", accueil)
	}
	if has(accueil, "schedule.read.all") || has(accueil, "schedule.read.own") || has(accueil, "*") {
		t.Fatalf("ACCUEIL must not gain broader schedule read: %v", accueil)
	}
	if !has(accueil, "queue.checkin") {
		t.Fatalf("ACCUEIL missing queue.checkin: %v", accueil)
	}

	physician := EffectiveStaffPermissions("staff", nil, []string{"MEDECINE_GENERALE"})
	if !has(physician, "schedule.read.own") {
		t.Fatalf("physician missing schedule.read.own: %v", physician)
	}
	if has(physician, "schedule.read.all") || has(physician, "schedule.read.service") || has(physician, "*") {
		t.Fatalf("physician must not gain broader schedule read: %v", physician)
	}
}
