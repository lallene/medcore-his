package rbac

import "testing"

// LOT29B B17: route-referenced permissions must exist in the catalog with SERVICE scope.
func TestLOT29B_B17_RoutePermissionCatalogKeys(t *testing.T) {
	t.Parallel()
	required := map[string]string{
		"cash.register.manage":        "SERVICE",
		"receivables.due_date.manage": "SERVICE",
		"receivables.followup.create": "SERVICE",
	}
	for key, wantScope := range required {
		meta, ok := permissionCatalog[key]
		if !ok {
			t.Fatalf("missing catalog key %s", key)
		}
		if meta.ScopeHint != wantScope {
			t.Fatalf("%s ScopeHint=%q want %q", key, meta.ScopeHint, wantScope)
		}
	}
}
