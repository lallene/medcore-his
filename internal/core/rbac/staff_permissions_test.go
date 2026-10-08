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
	if !has(facturation, "billing.cancel") || !has(facturation, "billing.tariff.manage") {
		t.Fatalf("facturation billing manage/cancel=%v", facturation)
	}
	if !has(facturation, "insurance.authorization.decide") || !has(facturation, "insurance.authorization.cancel") {
		t.Fatalf("facturation insurance decide/cancel=%v", facturation)
	}
	caissier := EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
	if !has(caissier, "billing.payment.create") {
		t.Fatalf("caissier missing billing.payment.create=%v", caissier)
	}
	comptable := EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
	if !has(comptable, "insurance_settlements.allocate") || has(comptable, "consultations.update") {
		t.Fatalf("comptable=%v", comptable)
	}
	if !has(comptable, "billing.credit_note.create") || !has(comptable, "billing.credit_note.read") {
		t.Fatalf("comptable credit_note=%v", comptable)
	}
	if has(EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.credit_note.create") {
		t.Fatal("caissier must not create credit notes")
	}
	if has(EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.credit.apply") {
		t.Fatal("caissier must not apply customer credit")
	}
	if !has(comptable, "billing.credit.apply") || !has(facturation, "billing.credit.apply") {
		t.Fatal("comptable/facturation must apply customer credit")
	}
	if !has(comptable, "billing.statement.read") || !has(facturation, "billing.statement.read") {
		t.Fatal("comptable/facturation must read financial statement")
	}
	if has(EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil), "billing.statement.read") {
		t.Fatal("caissier must not read full financial statement")
	}
	if has(EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil), "billing.statement.read") {
		t.Fatal("clinical must not read financial statement")
	}
	if !has(caissier, "billing.refund.request") || has(caissier, "billing.refund.approve") {
		t.Fatal("caissier may request refund but must not approve")
	}
	if !has(caissier, "billing.refund.execute") || has(comptable, "billing.refund.execute") {
		t.Fatal("caissier executes refund; comptable approves only (SoD)")
	}
	if !has(comptable, "billing.refund.approve") || !has(facturation, "billing.refund.request") {
		t.Fatal("comptable approve / facturation request refund")
	}
	if has(facturation, "billing.refund.approve") {
		t.Fatal("facturation must not approve refund")
	}
	if !has(facturation, "billing.refund.execute") {
		t.Fatal("facturation may execute/record external refund")
	}
	if has(EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil), "billing.refund.request") {
		t.Fatal("clinical must not request refund via broad financial permission")
	}
	if !has(caissier, "cash.movement.read") || has(caissier, "cash.movement.create") {
		t.Fatalf("caissier cash.movement=%v", caissier)
	}
	if !has(comptable, "cash.movement.read") || has(comptable, "cash.movement.create") {
		t.Fatalf("comptable cash.movement=%v", comptable)
	}
	if !has(comptable, "act_catalog.read") {
		t.Fatalf("comptable missing act_catalog.read=%v", comptable)
	}
	if !has(comptable, "performed_acts.read") || has(comptable, "performed_acts.void") {
		t.Fatalf("comptable performed_acts=%v", comptable)
	}
	dirAdmin := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
	if !has(dirAdmin, "cash.movement.create") || !has(dirAdmin, "cash.movement.read") {
		t.Fatalf("directeur administratif cash.movement=%v", dirAdmin)
	}
	if !has(dirAdmin, "act_catalog.manage") {
		t.Fatalf("directeur administratif missing act_catalog.manage=%v", dirAdmin)
	}
	if !has(dirAdmin, "performed_acts.read") || !has(dirAdmin, "performed_acts.void") || has(dirAdmin, "performed_acts.create") {
		t.Fatalf("directeur administratif performed_acts=%v", dirAdmin)
	}
	if !has(dirAdmin, "performed_acts.producer_map.read") || !has(dirAdmin, "performed_acts.producer_map.manage") {
		t.Fatalf("directeur administratif producer_map=%v", dirAdmin)
	}
	dirMed := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_MEDICAL"}, nil)
	if !has(dirMed, "performed_acts.producer_map.read") || !has(dirMed, "performed_acts.producer_map.manage") {
		t.Fatalf("directeur médical producer_map=%v", dirMed)
	}
	physician := EffectiveStaffPermissions("staff", nil, []string{"MEDECINE_GENERALE"})
	if !has(physician, "performed_acts.create") || !has(physician, "performed_acts.void") {
		t.Fatalf("physician performed_acts=%v", physician)
	}
	if !has(physician, "act_catalog.read") || has(physician, "act_catalog.manage") {
		t.Fatalf("physician act_catalog=%v", physician)
	}
	if !has(physician, "insurance.authorization.submit") || !has(physician, "insurance.authorization.decide") {
		t.Fatalf("physician insurance lifecycle=%v", physician)
	}
	if !has(physician, "insurance.authorization.link_act") {
		t.Fatalf("physician missing link_act=%v", physician)
	}
	if has(physician, "performed_acts.producer_map.manage") || has(physician, "performed_acts.producer_map.read") {
		t.Fatalf("physician must not manage producer maps=%v", physician)
	}
	if got := EffectiveStaffPermissions("admin", nil, nil); len(got) != 1 || got[0] != "*" {
		t.Fatalf("admin=%v", got)
	}
}

// LOT27I-A — performed_acts.create requires act_catalog.read (catalog selection dependency).
func TestPerformedActsCreateImpliesActCatalogRead(t *testing.T) {
	type caseSpec struct {
		name        string
		functions   []string
		specialties []string
	}
	cases := []caseSpec{
		{name: "physician", specialties: []string{"MEDECINE_GENERALE"}},
		{name: "INFIRMIER", functions: []string{"INFIRMIER"}},
		{name: "BIOLOGISTE", functions: []string{"BIOLOGISTE"}},
		{name: "RADIOLOGIE", functions: []string{"RADIOLOGIE"}},
	}
	for _, tc := range cases {
		perms := EffectiveStaffPermissions("staff", tc.functions, tc.specialties)
		if !has(perms, "performed_acts.create") {
			t.Fatalf("%s missing performed_acts.create: %v", tc.name, perms)
		}
		if !has(perms, "act_catalog.read") {
			t.Fatalf("%s has performed_acts.create without act_catalog.read: %v", tc.name, perms)
		}
		if has(perms, "act_catalog.manage") {
			t.Fatalf("%s must not gain act_catalog.manage from create dependency: %v", tc.name, perms)
		}
	}

	// Exhaustive: every built-in function pack that grants create also grants catalog read
	// and does not imply catalog manage via this dependency.
	for code, pack := range StaffFunctionPermissions {
		if !has(pack, "performed_acts.create") {
			continue
		}
		if !has(pack, "act_catalog.read") {
			t.Fatalf("function %s has performed_acts.create without act_catalog.read: %v", code, pack)
		}
		if has(pack, "act_catalog.manage") {
			t.Fatalf("function %s must not gain act_catalog.manage from create dependency: %v", code, pack)
		}
	}
	if !has(StaffPhysicianPermissions, "performed_acts.create") {
		t.Fatal("StaffPhysicianPermissions missing performed_acts.create")
	}
	if !has(StaffPhysicianPermissions, "act_catalog.read") {
		t.Fatal("StaffPhysicianPermissions missing act_catalog.read")
	}
	if has(StaffPhysicianPermissions, "act_catalog.manage") {
		t.Fatal("StaffPhysicianPermissions must not include act_catalog.manage")
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
