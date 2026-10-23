package complete

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// Check is one validation rule of the generated dataset.
type Check struct {
	Domain   string
	Name     string
	Critical bool
	OK       bool
	Detail   string
}

// Report is the outcome of Validate.
type Report struct {
	Checks []Check
	Counts []Count
}

// Count is one volume line of the final summary.
type Count struct {
	Domain, Label string
	N             int64
}

// Passed is true when every critical check passes.
func (r *Report) Passed() bool {
	for _, c := range r.Checks {
		if c.Critical && !c.OK {
			return false
		}
	}
	return true
}

// DomainVerdicts returns PASS/FAIL per domain (FAIL as soon as one check fails).
func (r *Report) DomainVerdicts() ([]string, map[string]bool) {
	var order []string
	ok := map[string]bool{}
	for _, c := range r.Checks {
		if _, seen := ok[c.Domain]; !seen {
			order = append(order, c.Domain)
			ok[c.Domain] = true
		}
		if !c.OK {
			ok[c.Domain] = false
		}
	}
	return order, ok
}

// Print writes the human-readable validation report.
func (r *Report) Print(logf func(string, ...any)) {
	logf("================ DEMO COMPLETE VALIDATION ================")
	for _, c := range r.Counts {
		logf("  %-18s %-34s %8d", c.Domain, c.Label, c.N)
	}
	logf("----------------------------------------------------------")
	order, ok := r.DomainVerdicts()
	for _, d := range order {
		verdict := "PASS"
		if !ok[d] {
			verdict = "FAIL"
		}
		logf("  %-30s %s", d, verdict)
		for _, c := range r.Checks {
			if c.Domain == d && !c.OK {
				crit := ""
				if c.Critical {
					crit = " [critique]"
				}
				logf("      ✗ %s%s — %s", c.Name, crit, c.Detail)
			}
		}
	}
	overall := "PASS"
	if !r.Passed() {
		overall = "FAIL"
	}
	logf("  OVERALL = %s", overall)
	logf("==========================================================")
}

type validator struct {
	db  *gorm.DB
	cfg Config
	r   *Report
}

func (v *validator) count(domain, label, sql string, args ...any) int64 {
	var n int64
	if err := v.db.Raw(sql, args...).Scan(&n).Error; err != nil {
		v.check(domain, "requête "+label, true, false, err.Error())
		return 0
	}
	v.r.Counts = append(v.r.Counts, Count{domain, label, n})
	return n
}

func (v *validator) check(domain, name string, critical, ok bool, detail string) {
	v.r.Checks = append(v.r.Checks, Check{Domain: domain, Name: name, Critical: critical, OK: ok, Detail: detail})
}

func (v *validator) atLeast(domain, label, sql string, min int64, critical bool, args ...any) int64 {
	n := v.count(domain, label, sql, args...)
	v.check(domain, fmt.Sprintf("%s >= %d", label, min), critical, n >= min, fmt.Sprintf("obtenu %d", n))
	return n
}

// statuses checks that every expected value of a column is represented.
func (v *validator) statuses(domain, table, column string, expected []string, critical bool) {
	var present []string
	if err := v.db.Raw(fmt.Sprintf("SELECT DISTINCT %s FROM %s", quoteIdent(column), quoteIdent(table))).Scan(&present).Error; err != nil {
		v.check(domain, table+"."+column, critical, false, err.Error())
		return
	}
	set := map[string]bool{}
	for _, p := range present {
		set[p] = true
	}
	var missing []string
	for _, e := range expected {
		if !set[e] {
			missing = append(missing, e)
		}
	}
	v.check(domain, fmt.Sprintf("statuts %s.%s", table, column), critical, len(missing) == 0, "manquants: "+strings.Join(missing, ", "))
}

// zero checks that an incoherence query returns 0.
func (v *validator) zero(domain, name, sql string, critical bool, args ...any) {
	var n int64
	if err := v.db.Raw(sql, args...).Scan(&n).Error; err != nil {
		v.check(domain, name, critical, false, err.Error())
		return
	}
	v.check(domain, name, critical, n == 0, fmt.Sprintf("%d ligne(s) incohérente(s)", n))
}

// Validate checks the dataset invariants (volumes, coverage of statuses, coherence).
func Validate(db *gorm.DB, cfg Config) *Report {
	v := &validator{db: db, cfg: cfg, r: &Report{}}
	for _, fn := range validators {
		fn(v)
	}
	return v.r
}

// validators are registered by domain files.
var validators []func(v *validator)

func init() {
	validators = append(validators, validateOrg)
}

func validateOrg(v *validator) {
	d := "RBAC / organisation"
	v.atLeast(d, "comptes demo-complete", "SELECT COUNT(*) FROM users WHERE email LIKE 'dc.%'", 60, true)
	v.atLeast(d, "services avec personnel", `SELECT COUNT(DISTINCT s.id) FROM organization_services s JOIN staff_service_assignments a ON a.service_id = s.id AND a.active`, 15, true)
	for _, fn := range []string{"DIRECTEUR_MEDICAL", "DIRECTEUR_ADMINISTRATIF", "ACCUEIL", "SAGE_FEMME", "INFIRMIER", "AIDE_SOIGNANT", "PHARMACIEN", "COMPTABLE", "CAISSIER", "BIOLOGISTE", "FACTURATION", "RADIOLOGIE", "SUPPORT_AGENT", "SUPPORT_MANAGER"} {
		v.atLeast(d, "fonction "+fn, "SELECT COUNT(*) FROM staff_functions WHERE code = ? AND active", 1, true, fn)
	}
	for _, sp := range []string{"URGENCES", "MEDECINE_GENERALE", "GYNECOLOGIE", "CARDIOLOGIE", "ORL", "DIABETOLOGIE", "NEUROLOGIE", "RHUMATOLOGIE", "CHIRURGIE"} {
		v.atLeast(d, "spécialité "+sp, "SELECT COUNT(*) FROM staff_specialties WHERE code = ? AND active", 1, true, sp)
	}
	for _, c := range []string{"XRAY", "ULTRASOUND", "CT"} {
		v.atLeast(d, "capability "+c, "SELECT COUNT(*) FROM staff_capabilities WHERE code = ? AND active", 1, true, c)
	}
	v.atLeast(d, "override GRANT", "SELECT COUNT(*) FROM staff_permission_overrides WHERE effect = 'GRANT' AND active", 1, true)
	v.atLeast(d, "override DENY", "SELECT COUNT(*) FROM staff_permission_overrides WHERE effect = 'DENY' AND active", 1, true)
	v.atLeast(d, "overlay de matrice", "SELECT COUNT(*) FROM rbac_matrix_overrides WHERE active", 1, true)
	v.atLeast(d, "compte désactivé", "SELECT COUNT(*) FROM users WHERE email LIKE 'dc.%' AND NOT is_active", 1, true)
	v.atLeast(d, "profil inactif / compte actif", "SELECT COUNT(*) FROM users u JOIN staff_profiles p ON p.user_id = u.id WHERE u.is_active AND NOT p.active", 1, true)
	v.atLeast(d, "rôle d'authentification accueil", "SELECT COUNT(*) FROM users WHERE role = 'accueil'", 1, true)
	v.atLeast(d, "compte multi-service", "SELECT COUNT(*) FROM (SELECT profile_id FROM staff_service_assignments WHERE active GROUP BY profile_id HAVING COUNT(*) >= 3) x", 1, true)
	v.atLeast(d, "compte multi-rôle", "SELECT COUNT(*) FROM (SELECT profile_id FROM staff_functions WHERE active GROUP BY profile_id HAVING COUNT(*) >= 2) x", 1, true)
	v.atLeast(d, "compte sans service", "SELECT COUNT(*) FROM staff_profiles p WHERE p.active AND NOT EXISTS (SELECT 1 FROM staff_service_assignments a WHERE a.profile_id = p.id AND a.active)", 1, true)
}
