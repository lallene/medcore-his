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
	if !has(facturation, "act_catalog.read") || has(facturation, "act_catalog.manage") {
		t.Fatalf("facturation act_catalog=%v", facturation)
	}
	if !has(facturation, "performed_acts.read") || has(facturation, "performed_acts.create") || has(facturation, "performed_acts.void") {
		t.Fatalf("facturation performed_acts=%v", facturation)
	}
	comptable := EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
	if !has(comptable, "insurance_settlements.allocate") || has(comptable, "consultations.update") {
		t.Fatalf("comptable=%v", comptable)
	}
	if !has(comptable, "act_catalog.read") {
		t.Fatalf("comptable missing act_catalog.read=%v", comptable)
	}
	if !has(comptable, "performed_acts.read") || has(comptable, "performed_acts.void") {
		t.Fatalf("comptable performed_acts=%v", comptable)
	}
	dirAdmin := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
	if !has(dirAdmin, "act_catalog.manage") {
		t.Fatalf("directeur administratif missing act_catalog.manage=%v", dirAdmin)
	}
	if !has(dirAdmin, "performed_acts.read") || !has(dirAdmin, "performed_acts.void") || has(dirAdmin, "performed_acts.create") {
		t.Fatalf("directeur administratif performed_acts=%v", dirAdmin)
	}
	physician := EffectiveStaffPermissions("staff", nil, []string{"MEDECINE_GENERALE"})
	if !has(physician, "performed_acts.create") || !has(physician, "performed_acts.void") {
		t.Fatalf("physician performed_acts=%v", physician)
	}
	if got := EffectiveStaffPermissions("admin", nil, nil); len(got) != 1 || got[0] != "*" {
		t.Fatalf("admin=%v", got)
	}
}

// LOT 23G.1 / 23I — scheduling read + booking least-privilege packs.
func TestSchedulingReadLeastPrivilegePacks(t *testing.T) {
	accueil := EffectiveStaffPermissions("staff", []string{"ACCUEIL"}, nil)
	if !has(accueil, "schedule.read.service") {
		t.Fatalf("ACCUEIL missing schedule.read.service: %v", accueil)
	}
	if !has(accueil, "appointment.create.service") {
		t.Fatalf("ACCUEIL missing appointment.create.service: %v", accueil)
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
	if has(physician, "appointment.create.service") || has(physician, "appointment.create.all") {
		t.Fatalf("physician must not gain booking create: %v", physician)
	}

	legacy := EffectiveStaffPermissions("accueil", nil, nil)
	if !has(legacy, "schedule.read.service") || !has(legacy, "appointment.create.service") {
		t.Fatalf("legacy role accueil must align scheduling read+create: %v", legacy)
	}

	dirMed := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_MEDICAL"}, nil)
	if !has(dirMed, "appointment.create.service") || has(dirMed, "appointment.create.all") {
		t.Fatalf("DirMed create.service only: %v", dirMed)
	}
	dirAdmin := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
	if !has(dirAdmin, "appointment.create.all") {
		t.Fatalf("DirAdmin missing create.all: %v", dirAdmin)
	}
	if !has(dirAdmin, "appointment_type.manage") {
		t.Fatalf("DirAdmin missing appointment_type.manage: %v", dirAdmin)
	}
	if has(dirMed, "appointment_type.manage") {
		t.Fatalf("DirMed must not gain appointment_type.manage: %v", dirMed)
	}
	if has(accueil, "appointment_type.manage") {
		t.Fatalf("ACCUEIL must not gain appointment_type.manage: %v", accueil)
	}
}
