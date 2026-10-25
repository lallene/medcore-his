package rbac

import (
	"os"
	"regexp"
	"testing"
)

var grcCompliancePermissions = []struct {
	key   string
	label string
}{
	{"grc.framework.read", "Lire les référentiels de conformité"},
	{"grc.framework.manage", "Gérer les référentiels de conformité"},
	{"grc.requirement.read", "Lire les exigences de conformité"},
	{"grc.requirement.manage", "Gérer les exigences de conformité"},
	{"grc.control.read", "Lire les contrôles GRC"},
	{"grc.control.manage", "Gérer les contrôles GRC"},
	{"grc.control.implementation.read", "Lire les implémentations de contrôles"},
	{"grc.control.implementation.manage", "Gérer les implémentations de contrôles"},
}

func TestGRCCompliancePermissionsCatalog(t *testing.T) {
	for _, tt := range grcCompliancePermissions {
		t.Run(tt.key, func(t *testing.T) {
			meta, ok := permissionCatalog[tt.key]
			if !ok {
				t.Fatalf("permission %q missing from catalog", tt.key)
			}
			if meta.Key != tt.key || meta.Label != tt.label {
				t.Fatalf("meta = %+v, want key %q label %q", meta, tt.key, tt.label)
			}
			if meta.Domain != "GRC" {
				t.Fatalf("Domain = %q, want GRC", meta.Domain)
			}
			if meta.ScopeHint != "GLOBAL" {
				t.Fatalf("ScopeHint = %q, want GLOBAL", meta.ScopeHint)
			}
			if meta.Sensitive || IsSensitive(tt.key) {
				t.Fatalf("permission %q must not be sensitive", tt.key)
			}
		})
	}

	if !LookupPermissionMeta("grc.risk.approve").Sensitive || !IsSensitive("grc.risk.approve") {
		t.Fatal(`"grc.risk.approve" must remain sensitive`)
	}
}

func TestGRCCompliancePermissionsDeclaredExactlyOnce(t *testing.T) {
	for _, tt := range grcCompliancePermissions {
		count := 0
		for _, meta := range AllPermissionMetas() {
			if meta.Key == tt.key {
				count++
			}
		}
		if count != 1 {
			t.Errorf("AllPermissionMetas() contains %q %d times, want 1", tt.key, count)
		}
	}

	// The catalog is a map, so a repeated add() would silently overwrite an
	// entry: check the declarations themselves for duplicates.
	source, err := os.ReadFile("catalog.go")
	if err != nil {
		t.Fatal(err)
	}

	declared := map[string]int{}
	for _, match := range regexp.MustCompile(`\badd\("([^"]+)"`).FindAllStringSubmatch(string(source), -1) {
		declared[match[1]]++
	}
	if len(declared) == 0 {
		t.Fatal("no permission declaration found in catalog.go")
	}
	for key, count := range declared {
		if count != 1 {
			t.Errorf("catalog.go declares %q %d times, want 1", key, count)
		}
	}
	for _, tt := range grcCompliancePermissions {
		if declared[tt.key] != 1 {
			t.Errorf("catalog.go declares %q %d times, want 1", tt.key, declared[tt.key])
		}
	}
}

func TestGRCCatalogHasNoBroadOrRelationPermissions(t *testing.T) {
	known := map[string]bool{}
	for _, key := range KnownPermissionKeys() {
		known[key] = true
	}

	for _, key := range []string{
		"grc.admin",
		"grc.requirement.control.manage",
		"grc.control.risk.manage",
		"grc.link.manage",
	} {
		if known[key] {
			t.Errorf("KnownPermissionKeys() must not contain %q", key)
		}
	}

	want := map[string]bool{
		"grc.dashboard.read": true,
		"grc.asset.read":     true,
		"grc.asset.manage":   true,
		"grc.risk.read":      true,
		"grc.risk.create":    true,
		"grc.risk.update":    true,
		"grc.risk.approve":   true,
	}
	for _, tt := range grcCompliancePermissions {
		want[tt.key] = true
	}
	for _, p := range grcEvidenceActionPermissions {
		want[p.key] = true
	}

	got := 0
	for _, meta := range AllPermissionMetas() {
		if meta.Domain != "GRC" {
			continue
		}
		got++
		if !want[meta.Key] {
			t.Errorf("unexpected GRC permission %q", meta.Key)
		}
	}
	if got != len(want) {
		t.Errorf("GRC catalog has %d permissions, want %d", got, len(want))
	}
}

func TestGRCCompliancePermissionsAreNotAutoAssigned(t *testing.T) {
	newKeys := map[string]bool{}
	for _, tt := range grcCompliancePermissions {
		newKeys[tt.key] = true
	}

	for function, perms := range StaffFunctionPermissions {
		for _, p := range perms {
			if newKeys[p] {
				t.Errorf("function %q must not be granted %q by default", function, p)
			}
		}
	}
	for _, p := range StaffPhysicianPermissions {
		if newKeys[p] {
			t.Errorf("physicians must not be granted %q by default", p)
		}
	}
}

func TestGRCCompliancePermissionsEvaluation(t *testing.T) {
	for _, tt := range grcCompliancePermissions {
		if !HasAnyPermission([]string{"*"}, tt.key) {
			t.Errorf("wildcard must grant %q", tt.key)
		}
	}

	manageDoesNotImplyRead := map[string]string{
		"grc.framework.manage":              "grc.framework.read",
		"grc.requirement.manage":            "grc.requirement.read",
		"grc.control.manage":                "grc.control.read",
		"grc.control.implementation.manage": "grc.control.implementation.read",
	}
	for manage, read := range manageDoesNotImplyRead {
		if HasAnyPermission([]string{manage}, read) {
			t.Errorf("%q must not imply %q", manage, read)
		}
	}

	out := ApplyUserOverrides([]string{"grc.control.manage"}, []UserOverride{
		{Permission: "grc.control.manage", Effect: OverrideDeny},
		{Permission: "grc.control.manage", Effect: OverrideGrant},
	})
	for _, p := range out {
		if p == "grc.control.manage" {
			t.Fatal("DENY must win over GRANT for grc.control.manage")
		}
	}
}
