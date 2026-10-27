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

// grcAssessmentAuditFindingPermissions are the GRC-01D.06 permissions; the
// sensitive ones guard the dedicated transition routes.
var grcAssessmentAuditFindingPermissions = []struct {
	key       string
	label     string
	sensitive bool
}{
	{"grc.assessment.read", "Lire les évaluations de contrôles", false},
	{"grc.assessment.create", "Créer une évaluation de contrôle", false},
	{"grc.assessment.update", "Modifier une évaluation de contrôle", false},
	{"grc.assessment.complete", "Clôturer une évaluation de contrôle", true},
	{"grc.audit.read", "Lire les audits GRC", false},
	{"grc.audit.create", "Créer un audit GRC", false},
	{"grc.audit.update", "Modifier un audit GRC", false},
	{"grc.audit.complete", "Clôturer un audit GRC", true},
	{"grc.finding.read", "Lire les constats GRC", false},
	{"grc.finding.create", "Créer un constat", false},
	{"grc.finding.update", "Modifier un constat", false},
	{"grc.finding.close", "Clôturer un constat", true},
	{"grc.finding.accept", "Accepter un constat sans correction", true},
}

// grcIncidentPermissions are the GRC-10 permissions. None is granted to a
// function or a physician. Close is sensitive and cannot be reached by update.
var grcIncidentPermissions = []struct {
	key       string
	label     string
	sensitive bool
}{
	{"grc.incident.read", "Lire les incidents de sécurité", false},
	{"grc.incident.create", "Créer un incident de sécurité", false},
	{"grc.incident.update", "Modifier un incident de sécurité", false},
	{"grc.incident.close", "Clôturer un incident de sécurité", true},
}

// grcPolicyPermissions are the GRC-11 permissions. None is granted to a
// function or a physician. Approve, publish and retire are sensitive.
var grcPolicyPermissions = []struct {
	key       string
	label     string
	sensitive bool
}{
	{"grc.policy.read", "Lire les politiques de gouvernance", false},
	{"grc.policy.create", "Créer une politique de gouvernance", false},
	{"grc.policy.update", "Modifier une politique de gouvernance", false},
	{"grc.policy.approve", "Approuver une politique de gouvernance", true},
	{"grc.policy.publish", "Publier une politique de gouvernance", true},
	{"grc.policy.retire", "Retirer une politique de gouvernance", true},
}

func grcExtendedPermissions() []struct {
	key       string
	label     string
	sensitive bool
} {
	out := append([]struct {
		key       string
		label     string
		sensitive bool
	}{}, grcEvidenceActionPermissions...)
	out = append(out, grcAssessmentAuditFindingPermissions...)
	out = append(out, grcIncidentPermissions...)
	out = append(out, grcPolicyPermissions...)
	return append(out, grcReportingPermissions...)
}

// grcReportingPermissions is the GRC-12 read permission. It is not sensitive
// and it is not granted to a function or a physician.
var grcReportingPermissions = []struct {
	key       string
	label     string
	sensitive bool
}{
	{"grc.reporting.read", "Lire le reporting GRC", false},
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
	for _, p := range grcExtendedPermissions() {
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
	for _, p := range grcExtendedPermissions() {
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

// GRC-01C adds grc.evidence.manage and grc.action.update, GRC-01D.06 the
// four transition permissions: every other GRC permission stays non-sensitive.
func TestGRCSensitivePermissionsAreExactlyDeclared(t *testing.T) {
	sensitive := map[string]bool{
		"grc.risk.approve":        true,
		"grc.evidence.manage":     true,
		"grc.action.update":       true,
		"grc.assessment.complete": true,
		"grc.audit.complete":      true,
		"grc.finding.close":       true,
		"grc.finding.accept":      true,
		"grc.incident.close":      true,
		"grc.policy.approve":      true,
		"grc.policy.publish":      true,
		"grc.policy.retire":       true,
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

func TestGRCAssessmentAuditFindingPermissions(t *testing.T) {
	for _, p := range grcAssessmentAuditFindingPermissions {
		meta, ok := permissionCatalog[p.key]
		if !ok {
			t.Errorf("permission %q missing from catalog", p.key)
			continue
		}
		if meta.Label != p.label || meta.Domain != "GRC" || meta.ScopeHint != "GLOBAL" || meta.Sensitive != p.sensitive {
			t.Errorf("meta = %+v, want label %q GRC GLOBAL sensitive=%v", meta, p.label, p.sensitive)
		}
	}

	grc := 0
	for _, meta := range AllPermissionMetas() {
		if meta.Domain == "GRC" {
			grc++
		}
	}
	if grc != 44 {
		t.Errorf("GRC catalog has %d permissions, want 44 (43 before GRC-12 + 1 reporting permission)", grc)
	}

	known := map[string]bool{}
	for _, key := range KnownPermissionKeys() {
		known[key] = true
	}
	for _, key := range []string{
		"grc.admin",
		"grc.assessment.manage", "grc.assessment.delete", "grc.assessment.evidence.manage",
		"grc.audit.manage", "grc.audit.delete", "grc.audit.close",
		"grc.finding.manage", "grc.finding.delete", "grc.finding.resolve",
	} {
		if known[key] {
			t.Errorf("KnownPermissionKeys() must not contain %q", key)
		}
	}
}

// Separation of duties: no 01D.06 permission implies another one, and none
// is granted to a function or physician by default.
func TestGRCAssessmentAuditFindingSeparationOfDuties(t *testing.T) {
	for _, granted := range grcAssessmentAuditFindingPermissions {
		for _, required := range grcAssessmentAuditFindingPermissions {
			if granted.key != required.key && HasAnyPermission([]string{granted.key}, required.key) {
				t.Errorf("%q must not imply %q", granted.key, required.key)
			}
		}
		if !HasAnyPermission([]string{"*"}, granted.key) {
			t.Errorf("wildcard must grant %q", granted.key)
		}
	}

	keys := map[string]bool{}
	for _, p := range grcAssessmentAuditFindingPermissions {
		keys[p.key] = true
	}
	for function, perms := range StaffFunctionPermissions {
		for _, perm := range perms {
			if keys[perm] {
				t.Errorf("function %q must not be granted %q by default", function, perm)
			}
		}
	}
	for _, perm := range StaffPhysicianPermissions {
		if keys[perm] {
			t.Errorf("physicians must not be granted %q by default", perm)
		}
	}
}

func TestGRCIncidentPermissionsAreSeparated(t *testing.T) {
	for _, granted := range grcIncidentPermissions {
		meta, ok := permissionCatalog[granted.key]
		if !ok {
			t.Errorf("permission %q missing from catalog", granted.key)
			continue
		}
		if meta.Label != granted.label || meta.Domain != "GRC" || meta.ScopeHint != "GLOBAL" || meta.Sensitive != granted.sensitive {
			t.Errorf("meta = %+v, want label %q GRC GLOBAL sensitive=%v", meta, granted.label, granted.sensitive)
		}
		for _, required := range grcIncidentPermissions {
			if granted.key != required.key && HasAnyPermission([]string{granted.key}, required.key) {
				t.Errorf("%q must not imply %q", granted.key, required.key)
			}
		}
		if !HasAnyPermission([]string{"*"}, granted.key) {
			t.Errorf("wildcard must grant %q", granted.key)
		}
	}

	keys := map[string]bool{}
	for _, p := range grcIncidentPermissions {
		keys[p.key] = true
	}
	for function, perms := range StaffFunctionPermissions {
		for _, perm := range perms {
			if keys[perm] {
				t.Errorf("function %q must not be granted %q by default", function, perm)
			}
		}
	}
	for _, perm := range StaffPhysicianPermissions {
		if keys[perm] {
			t.Errorf("physicians must not be granted %q by default", perm)
		}
	}
}

func TestGRCPolicyPermissionsAreSeparated(t *testing.T) {
	for _, granted := range grcPolicyPermissions {
		meta, ok := permissionCatalog[granted.key]
		if !ok {
			t.Errorf("permission %q missing from catalog", granted.key)
			continue
		}
		if meta.Label != granted.label || meta.Domain != "GRC" || meta.ScopeHint != "GLOBAL" || meta.Sensitive != granted.sensitive {
			t.Errorf("meta = %+v, want label %q GRC GLOBAL sensitive=%v", meta, granted.label, granted.sensitive)
		}
		for _, required := range grcPolicyPermissions {
			if granted.key != required.key && HasAnyPermission([]string{granted.key}, required.key) {
				t.Errorf("%q must not imply %q", granted.key, required.key)
			}
		}
		if !HasAnyPermission([]string{"*"}, granted.key) {
			t.Errorf("wildcard must grant %q", granted.key)
		}
	}

	keys := map[string]bool{}
	for _, p := range grcPolicyPermissions {
		keys[p.key] = true
	}
	for function, perms := range StaffFunctionPermissions {
		for _, perm := range perms {
			if keys[perm] {
				t.Errorf("function %q must not be granted %q by default", function, perm)
			}
		}
	}
	for _, perm := range StaffPhysicianPermissions {
		if keys[perm] {
			t.Errorf("physicians must not be granted %q by default", perm)
		}
	}
}

func TestGRCReportingPermissionIsReadOnly(t *testing.T) {
	for _, granted := range grcReportingPermissions {
		meta, ok := permissionCatalog[granted.key]
		if !ok {
			t.Fatalf("permission %q missing from catalog", granted.key)
		}
		if meta.Label != granted.label || meta.Domain != "GRC" || meta.ScopeHint != "GLOBAL" || meta.Sensitive {
			t.Fatalf("meta = %+v", meta)
		}
		if !HasAnyPermission([]string{"*"}, granted.key) {
			t.Fatalf("wildcard must grant %q", granted.key)
		}
		if HasAnyPermission([]string{"grc.dashboard.read"}, granted.key) || HasAnyPermission([]string{granted.key}, "grc.dashboard.read") {
			t.Fatal("reporting and dashboard permissions must stay independent")
		}
	}
	for function, perms := range StaffFunctionPermissions {
		for _, perm := range perms {
			if perm == "grc.reporting.read" {
				t.Errorf("function %q must not be granted grc.reporting.read", function)
			}
		}
	}
	for _, perm := range StaffPhysicianPermissions {
		if perm == "grc.reporting.read" {
			t.Fatal("physicians must not be granted grc.reporting.read")
		}
	}
}
