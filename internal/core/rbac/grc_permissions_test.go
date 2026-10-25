package rbac

import "testing"

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

	for _, key := range expected {
		if !known[key] {
			t.Errorf("KnownPermissionKeys() missing %q", key)
		}
	}

	if known["grc.admin"] {
		t.Fatal(`KnownPermissionKeys() must not contain broad permission "grc.admin"`)
	}
}

func TestGRCRiskApproveIsTheOnlySensitiveGRCPermissionInInitialLot(t *testing.T) {
	for _, meta := range AllPermissionMetas() {
		if meta.Domain != "GRC" {
			continue
		}

		wantSensitive := meta.Key == "grc.risk.approve"
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
