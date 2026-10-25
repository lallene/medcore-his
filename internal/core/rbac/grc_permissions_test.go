package rbac

import "testing"

// grcEvidenceActionPermissions are the GRC-01C permissions.
var grcEvidenceActionPermissions = []struct {
	key       string
	label     string
	sensitive bool
}{
	{"grc.evidence.read", "Lire les preuves GRC", false},
	{"grc.evidence.manage", "Gérer les preuves GRC", true},
	{"grc.action.read", "Lire les actions de remédiation GRC", false},
	{"grc.action.create", "Créer une action de remédiation", false},
	{"grc.action.update", "Modifier une action de remédiation", true},
}

func TestGRCPermissionsCatalog(t *testing.T) {
	tests := []struct {
		key       string
		sensitive bool
	}{
		{key: "grc.dashboard.read"},
		{key: "grc.asset.read"},
		{key: "grc.asset.manage"},
		{key: "grc.risk.read"},
		{key: "grc.risk.create"},
		{key: "grc.risk.update"},
		{key: "grc.risk.approve", sensitive: true},
	}
	for _, p := range grcEvidenceActionPermissions {
		tests = append(tests, struct {
			key       string
			sensitive bool
		}{key: p.key, sensitive: p.sensitive})
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			meta := LookupPermissionMeta(tt.key)

			if meta.Key != tt.key {
				t.Fatalf("LookupPermissionMeta(%q).Key = %q, want %q", tt.key, meta.Key, tt.key)
			}
			if meta.Domain != "GRC" {
				t.Fatalf("LookupPermissionMeta(%q).Domain = %q, want GRC", tt.key, meta.Domain)
			}
			if meta.ScopeHint != "GLOBAL" {
				t.Fatalf("LookupPermissionMeta(%q).ScopeHint = %q, want GLOBAL", tt.key, meta.ScopeHint)
			}
			if meta.Sensitive != tt.sensitive {
				t.Fatalf(
					"LookupPermissionMeta(%q).Sensitive = %v, want %v",
					tt.key,
					meta.Sensitive,
					tt.sensitive,
				)
			}
			if IsSensitive(tt.key) != tt.sensitive {
				t.Fatalf(
					"IsSensitive(%q) = %v, want %v",
					tt.key,
					IsSensitive(tt.key),
					tt.sensitive,
				)
			}
		})
	}
}

func TestGRCPermissionsAreKnown(t *testing.T) {
	known := make(map[string]bool)
	for _, key := range KnownPermissionKeys() {
		known[key] = true
	}

	expected := []string{
		"grc.dashboard.read",
		"grc.asset.read",
		"grc.asset.manage",
		"grc.risk.read",
		"grc.risk.create",
		"grc.risk.update",
		"grc.risk.approve",
	}
	for _, p := range grcEvidenceActionPermissions {
		expected = append(expected, p.key)
	}

	for _, key := range expected {
		if !known[key] {
			t.Errorf("KnownPermissionKeys() missing %q", key)
		}
	}

	if known["grc.admin"] {
		t.Fatal(`KnownPermissionKeys() must not contain broad permission "grc.admin"`)
	}
}

// GRC-01C adds grc.evidence.manage and grc.action.update to the sensitive
// GRC permissions: every other GRC permission must stay non-sensitive.
func TestGRCSensitivePermissionsAreExactlyDeclared(t *testing.T) {
	sensitive := map[string]bool{
		"grc.risk.approve":    true,
		"grc.evidence.manage": true,
		"grc.action.update":   true,
	}

	for _, meta := range AllPermissionMetas() {
		if meta.Domain != "GRC" {
			continue
		}

		wantSensitive := sensitive[meta.Key]
		if meta.Sensitive != wantSensitive {
			t.Errorf(
				"permission %q Sensitive = %v, want %v",
				meta.Key,
				meta.Sensitive,
				wantSensitive,
			)
		}
	}
}

func TestGRCEvidenceActionPermissionsCatalog(t *testing.T) {
	for _, p := range grcEvidenceActionPermissions {
		meta, ok := permissionCatalog[p.key]
		if !ok {
			t.Errorf("permission %q missing from catalog", p.key)
			continue
		}
		if meta.Key != p.key || meta.Label != p.label {
			t.Errorf("meta = %+v, want key %q label %q", meta, p.key, p.label)
		}
	}

	known := map[string]bool{}
	for _, key := range KnownPermissionKeys() {
		known[key] = true
	}
	for _, key := range []string{
		"grc.admin",
		"grc.evidence.create",
		"grc.evidence.update",
		"grc.evidence.delete",
		"grc.evidence.admin",
		"grc.action.delete",
		"grc.action.approve",
		"grc.action.admin",
		"grc.action.manage",
	} {
		if known[key] {
			t.Errorf("KnownPermissionKeys() must not contain %q", key)
		}
	}
}

func TestGRCEvidenceActionPermissionsAreNotAutoAssignedOrImplied(t *testing.T) {
	newKeys := map[string]bool{}
	for _, p := range grcEvidenceActionPermissions {
		newKeys[p.key] = true
	}

	for function, perms := range StaffFunctionPermissions {
		for _, perm := range perms {
			if newKeys[perm] {
				t.Errorf("function %q must not be granted %q by default", function, perm)
			}
		}
	}
	for _, perm := range StaffPhysicianPermissions {
		if newKeys[perm] {
			t.Errorf("physicians must not be granted %q by default", perm)
		}
	}

	for _, p := range grcEvidenceActionPermissions {
		if !HasAnyPermission([]string{"*"}, p.key) {
			t.Errorf("wildcard must grant %q", p.key)
		}
	}

	for granted, required := range map[string]string{
		"grc.evidence.manage": "grc.evidence.read",
		"grc.action.update":   "grc.action.read",
		"grc.action.create":   "grc.action.read",
	} {
		if HasAnyPermission([]string{granted}, required) {
			t.Errorf("%q must not imply %q", granted, required)
		}
	}
	if HasAnyPermission([]string{"grc.action.create"}, "grc.action.update") ||
		HasAnyPermission([]string{"grc.action.update"}, "grc.action.create") {
		t.Error("grc.action.create and grc.action.update must stay independent")
	}
}
