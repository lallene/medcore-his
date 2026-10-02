package rbac

import "testing"

func TestPharmacienLeastPrivilegePack(t *testing.T) {
	perms := EffectiveStaffPermissions("staff", []string{"PHARMACIEN"}, nil)
	want := []string{
		"pharmacy.stock.read", "pharmacy.stock.manage",
		"pharmacy.dispensation.read", "pharmacy.dispensation.create",
		"pharmacy.references.manage",
	}
	for _, p := range want {
		if !has(perms, p) {
			t.Fatalf("PHARMACIEN missing %s: %v", p, perms)
		}
	}
	forbidden := []string{
		"billing.create", "billing.*", "insurance.authorization.create",
		"hospitalizations.create", "consultations.create", "performed_acts.create",
		"patients:create", "*",
	}
	for _, p := range forbidden {
		if has(perms, p) {
			t.Fatalf("PHARMACIEN must not have %s: %v", p, perms)
		}
	}
}

func TestInfirmierNotPromotedToPharmacyWrite(t *testing.T) {
	perms := EffectiveStaffPermissions("staff", []string{"INFIRMIER"}, nil)
	if !has(perms, "pharmacy.dispensation.read") {
		t.Fatal("INFIRMIER should retain dispensation.read")
	}
	for _, p := range []string{"pharmacy.dispensation.create", "pharmacy.stock.manage", "pharmacy.references.manage"} {
		if has(perms, p) {
			t.Fatalf("INFIRMIER must not gain %s", p)
		}
	}
}

func TestPhysicianKeepsStockReadOnly(t *testing.T) {
	perms := EffectiveStaffPermissions("staff", nil, []string{"MEDECINE_GENERALE"})
	if !has(perms, "pharmacy.stock.read") {
		t.Fatal("physician missing pharmacy.stock.read")
	}
	if has(perms, "pharmacy.dispensation.create") || has(perms, "pharmacy.stock.manage") {
		t.Fatalf("physician must not gain pharmacy write: %v", perms)
	}
}

func TestPharmacyRoutePermissionsInCatalog(t *testing.T) {
	for _, code := range []string{
		"pharmacy.stock.read", "pharmacy.stock.manage",
		"pharmacy.dispensation.read", "pharmacy.dispensation.create",
		"pharmacy.references.manage",
	} {
		if _, ok := permissionCatalog[code]; !ok {
			t.Fatalf("catalog missing %s", code)
		}
	}
	if _, ok := permissionCatalog["pharmacy.dispense"]; ok {
		t.Fatal("dead pharmacy.dispense must not be in catalog")
	}
}
