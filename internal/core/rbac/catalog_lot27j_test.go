package rbac

import "testing"

func TestLOT27JPermissionCatalogContainsLifecycleKeys(t *testing.T) {
	t.Parallel()
	required := []string{
		"insurance.authorization.submit",
		"insurance.authorization.decide",
		"insurance.authorization.cancel",
		"insurance.authorization.link_act",
		"billing.payment.create",
		"billing.tariff.manage",
		"billing.cancel",
	}
	for _, key := range required {
		if _, ok := permissionCatalog[key]; !ok {
			t.Fatalf("missing catalog key %s", key)
		}
	}
}
