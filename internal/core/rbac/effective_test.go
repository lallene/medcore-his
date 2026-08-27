package rbac

import (
	"testing"
)

func TestApplyUserOverridesDenyWins(t *testing.T) {
	inh := []string{"billing.read", "patients:read", "queue.doctor.read"}
	out := ApplyUserOverrides(inh, []UserOverride{
		{Permission: "reports.read", Effect: OverrideGrant},
		{Permission: "billing.read", Effect: OverrideDeny},
		{Permission: "billing.read", Effect: OverrideGrant}, // DENY still wins
	})
	set := map[string]bool{}
	for _, p := range out {
		set[p] = true
	}
	if set["billing.read"] {
		t.Fatal("DENY must win over GRANT")
	}
	if !set["reports.read"] || !set["patients:read"] {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestApplyUserOverridesAdminWildcardUntouched(t *testing.T) {
	out := ApplyUserOverrides([]string{"*"}, []UserOverride{{Permission: "staff.manage", Effect: OverrideDeny}})
	if len(out) != 1 || out[0] != "*" {
		t.Fatalf("admin * must ignore overrides: %+v", out)
	}
}

func TestExplainEffectiveSources(t *testing.T) {
	entries := ExplainEffective("staff", []string{"ACCUEIL"}, nil, nil, []UserOverride{
		{Permission: "qa.read", Effect: OverrideGrant},
		{Permission: "queue.cancel", Effect: OverrideDeny},
	})
	by := map[string]EffectiveEntry{}
	for _, e := range entries {
		by[e.Permission] = e
	}
	if !by["queue.reception.read"].Allowed || by["queue.reception.read"].Source != SourceFunction {
		t.Fatalf("reception: %+v", by["queue.reception.read"])
	}
	if !by["qa.read"].Allowed || by["qa.read"].Source != SourceDirectGrant {
		t.Fatalf("grant: %+v", by["qa.read"])
	}
	if by["queue.cancel"].Allowed || by["queue.cancel"].Source != SourceDirectDeny {
		t.Fatalf("deny: %+v", by["queue.cancel"])
	}
}

func TestFunctionMatrixOverlay(t *testing.T) {
	base := FunctionPermissionsResolved("ACCUEIL", nil)
	hasCheckin := false
	for _, p := range base {
		if p == "queue.checkin" {
			hasCheckin = true
		}
	}
	if !hasCheckin {
		t.Fatal("base ACCUEIL should include queue.checkin")
	}
	revoked := FunctionPermissionsResolved("ACCUEIL", []MatrixOverlay{
		{FunctionCode: "ACCUEIL", Permission: "queue.checkin", Effect: OverrideDeny},
		{FunctionCode: "ACCUEIL", Permission: "qa.read", Effect: OverrideGrant},
	})
	set := map[string]bool{}
	for _, p := range revoked {
		set[p] = true
	}
	if set["queue.checkin"] {
		t.Fatal("matrix DENY should revoke queue.checkin")
	}
	if !set["qa.read"] {
		t.Fatal("matrix GRANT should add qa.read")
	}
}

func TestDirecteurHasRBACPerms(t *testing.T) {
	perms := EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
	if !HasAnyPermission(perms, "rbac.read", "rbac.user.manage") {
		t.Fatalf("directeur should have rbac.*: %+v", perms)
	}
}
