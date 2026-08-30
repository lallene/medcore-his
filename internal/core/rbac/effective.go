package rbac

import "sort"

// OverrideEffect for direct user permission overrides.
const (
	OverrideGrant = "GRANT"
	OverrideDeny  = "DENY"
)

// Source kinds for effective permission explanations.
const (
	SourceWildcard     = "WILDCARD"
	SourceFunction     = "FUNCTION"
	SourceSpecialty    = "SPECIALTY"
	SourceBase         = "BASE"
	SourceDirectGrant  = "DIRECT_GRANT"
	SourceDirectDeny   = "DIRECT_DENY"
	SourceMatrixAdd    = "MATRIX_ADD"
	SourceMatrixRevoke = "MATRIX_REVOKE"
)

// UserOverride is a direct GRANT/DENY on a user.
type UserOverride struct {
	Permission string
	Effect     string // GRANT | DENY
}

// MatrixOverlay adjusts the hardcoded function→permission matrix at runtime.
type MatrixOverlay struct {
	FunctionCode string
	Permission   string
	Effect       string // GRANT (add) | DENY (revoke from function base)
}

// EffectiveEntry explains one permission decision.
type EffectiveEntry struct {
	Permission string `json:"permission"`
	Allowed    bool   `json:"allowed"`
	Source     string `json:"source"`
	SourceName string `json:"sourceName,omitempty"`
	Domain     string `json:"domain,omitempty"`
	Label      string `json:"label,omitempty"`
	Sensitive  bool   `json:"sensitive"`
	ScopeHint  string `json:"scopeHint,omitempty"` // GLOBAL | SERVICE | OWN
}

// SensitivePermissions require reinforced UI confirmation.
var SensitivePermissions = map[string]bool{
	"*":                    true,
	"rbac.read":            true,
	"rbac.user.manage":     true,
	"rbac.override.manage": true,
	"rbac.matrix.manage":   true,
	"rbac.audit.read":      true,
	"staff.manage":         true,
	"staff.audit.read":     true,
	"billing.cancel":       true,
	"cash.payment.cancel":  true,
	"queue.read.all":       true,
	"ticket.read.all":      true,
	"organization.manage":  true,
}

// IsSensitive reports whether a permission is high-risk.
func IsSensitive(p string) bool { return SensitivePermissions[p] || p == "*" }

// FunctionPermissionsResolved returns permissions for one function after matrix overlays.
func FunctionPermissionsResolved(code string, overlays []MatrixOverlay) []string {
	set := map[string]bool{}
	for _, p := range StaffFunctionPermissions[code] {
		set[p] = true
	}
	for _, o := range overlays {
		if o.FunctionCode != code {
			continue
		}
		switch o.Effect {
		case OverrideGrant:
			set[o.Permission] = true
		case OverrideDeny:
			delete(set, o.Permission)
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// InheritedPermissions computes role/function/specialty inheritance (no user overrides).
// Admin short-circuits to *.
func InheritedPermissions(role string, functions, specialties []string, overlays []MatrixOverlay) []string {
	if role == "admin" {
		return []string{"*"}
	}
	set := map[string]bool{}
	set["organization.read"] = true
	for _, p := range []string{"ticket.create", "ticket.read.own", "ticket.comment"} {
		set[p] = true
	}
	if role == "accueil" {
		for _, p := range []string{
			"patients:read", "patients:create", "patients:update", "hospitalizations.read", "rooms.read", "beds.read", "bed_assignments.read",
			"queue.reception.read", "queue.checkin", "queue.cancel",
			"schedule.read.service",
			"appointment.create.service",
			"appointment.cancel.service", "appointment.no_show.service",
		} {
			set[p] = true
		}
	}
	for _, code := range functions {
		for _, p := range FunctionPermissionsResolved(code, overlays) {
			set[p] = true
		}
	}
	if len(specialties) > 0 {
		for _, p := range StaffPhysicianPermissions {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ApplyUserOverrides merges DENY > GRANT > inherited.
// Priority documented in RBAC_ADMIN.md. Admin wildcard is not reduced by overrides.
func ApplyUserOverrides(inherited []string, overrides []UserOverride) []string {
	for _, p := range inherited {
		if p == "*" {
			return []string{"*"}
		}
	}
	set := map[string]bool{}
	for _, p := range inherited {
		set[p] = true
	}
	for _, o := range overrides {
		if o.Permission == "" {
			continue
		}
		switch o.Effect {
		case OverrideGrant:
			set[o.Permission] = true
		case OverrideDeny:
			delete(set, o.Permission)
		}
	}
	// Second pass: DENY always wins even if GRANT listed after in slice
	for _, o := range overrides {
		if o.Effect == OverrideDeny {
			delete(set, o.Permission)
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ExplainEffective builds per-permission source explanations for ACC.
func ExplainEffective(role string, functions, specialties []string, overlays []MatrixOverlay, overrides []UserOverride) []EffectiveEntry {
	if role == "admin" {
		return []EffectiveEntry{{
			Permission: "*", Allowed: true, Source: SourceWildcard, SourceName: "admin",
			Label: "Accès total", Domain: "Administration", Sensitive: true, ScopeHint: "GLOBAL",
		}}
	}

	// Build inheritance provenance
	type prov struct {
		source, name string
	}
	inheritedFrom := map[string]prov{}
	for _, p := range []string{"organization.read", "ticket.create", "ticket.read.own", "ticket.comment"} {
		inheritedFrom[p] = prov{SourceBase, "base"}
	}
	if role == "accueil" {
		for _, p := range []string{
			"patients:read", "patients:create", "patients:update", "hospitalizations.read", "rooms.read", "beds.read", "bed_assignments.read",
			"queue.reception.read", "queue.checkin", "queue.cancel",
			"schedule.read.service",
			"appointment.create.service",
			"appointment.cancel.service", "appointment.no_show.service",
		} {
			inheritedFrom[p] = prov{SourceBase, "role:accueil"}
		}
	}
	for _, code := range functions {
		for _, p := range FunctionPermissionsResolved(code, overlays) {
			if _, ok := inheritedFrom[p]; !ok {
				inheritedFrom[p] = prov{SourceFunction, code}
			}
		}
	}
	if len(specialties) > 0 {
		label := specialties[0]
		if len(specialties) > 1 {
			label = "SPECIALTY"
		}
		for _, p := range StaffPhysicianPermissions {
			if _, ok := inheritedFrom[p]; !ok {
				inheritedFrom[p] = prov{SourceSpecialty, label}
			}
		}
	}

	denied := map[string]bool{}
	granted := map[string]bool{}
	for _, o := range overrides {
		if o.Effect == OverrideDeny {
			denied[o.Permission] = true
		}
		if o.Effect == OverrideGrant {
			granted[o.Permission] = true
		}
	}

	keys := map[string]bool{}
	for p := range inheritedFrom {
		keys[p] = true
	}
	for p := range granted {
		keys[p] = true
	}
	for p := range denied {
		keys[p] = true
	}

	out := make([]EffectiveEntry, 0, len(keys))
	for p := range keys {
		meta := LookupPermissionMeta(p)
		e := EffectiveEntry{
			Permission: p,
			Domain:     meta.Domain,
			Label:      meta.Label,
			Sensitive:  IsSensitive(p),
			ScopeHint:  meta.ScopeHint,
		}
		if denied[p] {
			e.Allowed = false
			e.Source = SourceDirectDeny
			e.SourceName = "override"
			out = append(out, e)
			continue
		}
		if _, ok := inheritedFrom[p]; ok {
			e.Allowed = true
			e.Source = inheritedFrom[p].source
			e.SourceName = inheritedFrom[p].name
			out = append(out, e)
			continue
		}
		if granted[p] {
			e.Allowed = true
			e.Source = SourceDirectGrant
			e.SourceName = "override"
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain == out[j].Domain {
			return out[i].Permission < out[j].Permission
		}
		return out[i].Domain < out[j].Domain
	})
	return out
}

// EffectiveStaffPermissionsFull applies matrix overlays then user overrides.
func EffectiveStaffPermissionsFull(role string, functions, specialties []string, overlays []MatrixOverlay, overrides []UserOverride) []string {
	inh := InheritedPermissions(role, functions, specialties, overlays)
	return ApplyUserOverrides(inh, overrides)
}

// CanAdministerRBAC is true if the actor can manage access control.
func CanAdministerRBAC(perms []string) bool {
	for _, p := range perms {
		if p == "*" || p == "rbac.user.manage" || p == "staff.manage" {
			return true
		}
	}
	return false
}

// HasAnyPermission checks exact or wildcard.
func HasAnyPermission(perms []string, required ...string) bool {
	set := map[string]bool{}
	for _, p := range perms {
		if p == "*" {
			return true
		}
		set[p] = true
	}
	for _, r := range required {
		if set[r] {
			return true
		}
	}
	return false
}
