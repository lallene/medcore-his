package complete

import (
	"fmt"
	"sort"

	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/access"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

type orgState struct {
	Services map[string]organization.Service // by code
}

func (o *orgState) ServiceID(code string) uint { return o.Services[code].ID }

// Member is one demo-complete account.
type Member struct {
	UserID, ProfileID uint
	Email, Name       string
	Role              string
	Functions         []string
	Specialties       []string
	Capabilities      []string
	Services          []string // primary first
	Perms             []string
	Tags              map[string]bool // scenario tags (multi-service, restricted, ...)
}

func (m *Member) Has(p string) bool {
	for _, x := range m.Perms {
		if x == "*" || x == p {
			return true
		}
	}
	return false
}

// PrimaryService code ("" when none).
func (m *Member) PrimaryService() string {
	if len(m.Services) == 0 {
		return ""
	}
	return m.Services[0]
}

type staffState struct {
	Admin   *Member
	Members []*Member
	byEmail map[string]*Member
}

func (s *staffState) add(m *Member) {
	if s.byEmail == nil {
		s.byEmail = map[string]*Member{}
	}
	s.Members = append(s.Members, m)
	s.byEmail[m.Email] = m
}

// With returns the active members matching a predicate (deterministic order).
func (s *staffState) With(pred func(m *Member) bool) []*Member {
	var out []*Member
	for _, m := range s.Members {
		if !m.Tags["inactive"] && pred(m) {
			out = append(out, m)
		}
	}
	return out
}

func (s *staffState) ByFunction(code string) []*Member {
	return s.With(func(m *Member) bool { return contains(m.Functions, code) && !m.Tags["restricted"] })
}

func (s *staffState) BySpecialty(code string) []*Member {
	return s.With(func(m *Member) bool { return contains(m.Specialties, code) })
}

func (s *staffState) Tagged(tag string) *Member {
	for _, m := range s.Members {
		if m.Tags[tag] {
			return m
		}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------------------

type orgPlanner struct{}

func (orgPlanner) Name() string        { return "Organisation / personnel / RBAC" }
func (orgPlanner) PlanDay(*World, Day) {}

type memberSpec struct {
	key          string // email local part after "dc."
	title        string
	role         string
	functions    []string
	specialties  []string
	capabilities []string
	services     []string
	tags         []string
	active       bool
}

// physician specialties → services (primary, secondary...).
var specialtyServices = map[string][]string{
	"URGENCES":          {"URG"},
	"MEDECINE_GENERALE": {"GEN"},
	"GYNECOLOGIE":       {"GYNE", "MAT"},
	"CARDIOLOGIE":       {"CARD"},
	"ORL":               {"ORL"},
	"DIABETOLOGIE":      {"DIAB"},
	"NEUROLOGIE":        {"NEURO"},
	"RHUMATOLOGIE":      {"RHUM"},
	"CHIRURGIE":         {"CHIR"},
}

func staffRoster() []memberSpec {
	r := []memberSpec{
		// Direction (marker account first: see MarkerEmail).
		{key: "direction.medicale", title: "Directeur médical", role: "staff", functions: []string{"DIRECTEUR_MEDICAL"}, specialties: []string{"MEDECINE_GENERALE"}, services: []string{"ADMIN", "GEN"}, tags: []string{"global-scope"}},
		{key: "direction.administrative", title: "Directrice administrative et financière", role: "staff", functions: []string{"DIRECTEUR_ADMINISTRATIF"}, services: []string{"ADMIN", "FACT", "CASH"}, tags: []string{"refund-approver-2", "global-scope"}},
	}
	// Physicians (9 specialties, several per busy specialty).
	counts := []struct {
		code string
		n    int
	}{{"URGENCES", 4}, {"MEDECINE_GENERALE", 5}, {"GYNECOLOGIE", 2}, {"CARDIOLOGIE", 2}, {"ORL", 1}, {"DIABETOLOGIE", 1}, {"NEUROLOGIE", 1}, {"RHUMATOLOGIE", 1}, {"CHIRURGIE", 2}}
	for _, c := range counts {
		for i := 1; i <= c.n; i++ {
			svcs := append([]string{}, specialtyServices[c.code]...)
			if c.code == "MEDECINE_GENERALE" && i == 2 {
				svcs = []string{"PED", "GEN"} // pediatric consultation is held by GPs
			}
			if c.code == "GYNECOLOGIE" && i == 2 {
				svcs = []string{"MAT", "GYNE"} // prenatal consultations (CPN) are held in maternity
			}
			if c.code == "URGENCES" && (i == 2 || i == 3) {
				svcs = []string{"URG", "SOINS"} // emergency physicians also follow the short-stay ward
			}
			if c.code == "URGENCES" && i == 1 {
				svcs = []string{"URG", "REA"}
			}
			r = append(r, memberSpec{key: fmt.Sprintf("medecin.%s.%d", slug(c.code), i), title: "Médecin " + c.code, role: "staff", specialties: []string{c.code}, services: svcs})
		}
	}
	add := func(key, title string, fn []string, svcs []string, tags ...string) {
		r = append(r, memberSpec{key: key, title: title, role: "staff", functions: fn, services: svcs, tags: tags})
	}
	// Care staff, distributed across services.
	for i, s := range []string{"URG", "URG", "SOINS", "REA", "CHIR", "PED", "MAT", "GEN"} {
		add(fmt.Sprintf("infirmier.%d", i+1), "Infirmier(ère)", []string{"INFIRMIER"}, []string{s})
	}
	add("infirmier.polyvalent", "Infirmier polyvalent", []string{"INFIRMIER"}, []string{"URG", "SOINS", "REA"}, "multi-service")
	add("infirmier.restreint", "Infirmier (override DENY)", []string{"INFIRMIER"}, []string{"SOINS"}, "deny-override")
	for i, s := range []string{"URG", "SOINS", "MAT", "PED"} {
		add(fmt.Sprintf("aide.soignant.%d", i+1), "Aide-soignant(e)", []string{"AIDE_SOIGNANT"}, []string{s})
	}
	add("sage.femme.1", "Sage-femme", []string{"SAGE_FEMME"}, []string{"MAT"})
	add("sage.femme.2", "Sage-femme", []string{"SAGE_FEMME"}, []string{"MAT", "GYNE"})
	// Front office.
	for i, s := range []string{"URG", "GEN", "CARD", "MAT"} {
		add(fmt.Sprintf("accueil.%d", i+1), "Agent d'accueil", []string{"ACCUEIL"}, []string{s})
	}
	add("accueil.facturation", "Accueil (override GRANT billing.read)", []string{"ACCUEIL"}, []string{"GEN"}, "grant-override")
	// Technical platforms.
	add("biologiste.1", "Biologiste", []string{"BIOLOGISTE"}, []string{"LAB"})
	add("biologiste.2", "Technicien de laboratoire", []string{"BIOLOGISTE"}, []string{"LAB"})
	r = append(r,
		memberSpec{key: "radiologue.1", title: "Radiologue (XRAY, ULTRASOUND, CT)", role: "staff", functions: []string{"RADIOLOGIE"}, capabilities: []string{"XRAY", "ULTRASOUND", "CT"}, services: []string{"RAD"}},
		memberSpec{key: "radiologue.2", title: "Manipulateur radio (XRAY)", role: "staff", functions: []string{"RADIOLOGIE"}, capabilities: []string{"XRAY"}, services: []string{"RAD"}, tags: []string{"xray-only"}},
		memberSpec{key: "radiologue.3", title: "Échographiste (ULTRASOUND)", role: "staff", functions: []string{"RADIOLOGIE"}, capabilities: []string{"ULTRASOUND"}, services: []string{"RAD"}, tags: []string{"us-only"}},
		memberSpec{key: "radiologue.sans.capability", title: "Radiologie sans modalité habilitée", role: "staff", functions: []string{"RADIOLOGIE"}, services: []string{"RAD"}, tags: []string{"no-capability"}},
	)
	add("pharmacien.1", "Pharmacien", []string{"PHARMACIEN"}, []string{"PHARM"})
	add("pharmacien.2", "Préparateur en pharmacie", []string{"PHARMACIEN"}, []string{"PHARM"})
	// Finance (refund separation of duties: requester / approver / executor are distinct).
	add("facturation.1", "Agent de facturation (demandeur remboursements)", []string{"FACTURATION"}, []string{"FACT"}, "refund-requester")
	add("facturation.2", "Agent de facturation", []string{"FACTURATION"}, []string{"FACT"})
	add("caisse.1", "Caissière (exécutante remboursements)", []string{"CAISSIER"}, []string{"CASH"}, "refund-executor")
	add("caisse.2", "Caissier", []string{"CAISSIER"}, []string{"CASH"})
	add("caisse.3", "Caissière", []string{"CAISSIER"}, []string{"CASH", "URG"})
	add("facturation.caisse", "Facturation et caisse (multi-rôle)", []string{"FACTURATION", "CAISSIER"}, []string{"FACT", "CASH"}, "multi-role")
	add("comptable.1", "Comptable (approbateur remboursements)", []string{"COMPTABLE"}, []string{"COMPTA"}, "refund-approver")
	add("comptable.2", "Comptable assurances", []string{"COMPTABLE"}, []string{"COMPTA"})
	// Support desk.
	add("support.agent.1", "Agent support", []string{"SUPPORT_AGENT"}, []string{"ADMIN"})
	add("support.agent.2", "Agent support", []string{"SUPPORT_AGENT"}, []string{"URG"})
	add("support.manager", "Responsable support", []string{"SUPPORT_MANAGER"}, []string{"ADMIN"})
	add("support.sans.service", "Agent support sans service", []string{"SUPPORT_AGENT"}, nil, "no-service")
	// Account-state scenarios.
	r = append(r,
		memberSpec{key: "infirmier.parti", title: "Infirmier (compte désactivé)", role: "staff", functions: []string{"INFIRMIER"}, services: []string{"URG"}, tags: []string{"disabled"}},
		memberSpec{key: "profil.inactif", title: "Profil personnel inactif (compte actif)", role: "staff", functions: []string{"CAISSIER"}, services: []string{"CASH"}, tags: []string{"inactive-profile", "inactive"}},
		memberSpec{key: "accueil.historique", title: "Accueil (rôle d'authentification accueil, sans profil)", role: "accueil", tags: []string{"auth-role-accueil", "no-profile"}},
	)
	for i := range r {
		if !hasTag(r[i].tags, "disabled") {
			r[i].active = true
		}
	}
	return r
}

func hasTag(tags []string, t string) bool { return contains(tags, t) }

func (orgPlanner) Setup(w *World) error {
	adminUser, _, err := w.Opts.CreateAccount(w.DB, "Administrateur", "admin@medcore.local", "admin", true, nil)
	if err != nil {
		return wrapf(err, "administrateur")
	}
	services, err := organization.SeedReference(w.DB, adminUser.ID)
	if err != nil {
		return err
	}
	w.Org.Services = services
	w.Staff.Admin = &Member{UserID: adminUser.ID, Email: adminUser.Email, Name: adminUser.Name, Role: "admin", Perms: []string{"*"}, Tags: map[string]bool{"admin": true}}

	g := w.RNG.Stream("staff")
	for _, spec := range staffRoster() {
		m, err := createMember(w, g, spec, adminUser.ID)
		if err != nil {
			return wrapf(err, "compte %s", spec.key)
		}
		w.Staff.add(m)
	}
	if err := rbacScenarios(w, adminUser.ID); err != nil {
		return err
	}
	return refreshPermissions(w)
}

func createMember(w *World, g *RNG, spec memberSpec, actor uint) (*Member, error) {
	id := newIdentity(g, "")
	title := "Dr "
	if len(spec.specialties) == 0 {
		title = ""
	}
	email := StaffEmailPrefix + spec.key + StaffEmailDomain
	m := &Member{Email: email, Name: title + id.Full(), Role: spec.role, Functions: spec.functions, Specialties: spec.specialties, Capabilities: spec.capabilities, Services: spec.services, Tags: map[string]bool{}}
	for _, t := range spec.tags {
		m.Tags[t] = true
	}
	var profileFn func(tx *gorm.DB, u auth.User) error
	if !m.Tags["no-profile"] {
		profileFn = func(tx *gorm.DB, u auth.User) error {
			active := !m.Tags["inactive-profile"]
			req := staff.UpsertRequest{UserID: u.ID, EmployeeCode: "DC-" + slugCode(spec.key), JobTitle: spec.title, Active: &active,
				Functions: spec.functions, Specialties: spec.specialties, Capabilities: spec.capabilities,
				ProfessionalNumber: fmt.Sprintf("ORD-%05d", g.Between(10000, 99999))}
			if len(spec.services) > 0 {
				primary := w.Org.ServiceID(spec.services[0])
				req.PrimaryServiceID = &primary
				for _, s := range spec.services[1:] {
					req.SecondaryServiceIDs = append(req.SecondaryServiceIDs, w.Org.ServiceID(s))
				}
			}
			view, err := staff.NewService(tx).Upsert(0, req, actor)
			if err != nil {
				return err
			}
			m.ProfileID = view.ID
			if m.Tags["inactive-profile"] {
				// Legacy state (active account, inactive staff profile) that the UI cannot
				// produce: written directly on the account created in this transaction to
				// exercise the middleware refusal "Profil personnel inactif".
				// (GORM applies default:true to a false bool on insert: set the profile flag explicitly.)
				if err := tx.Model(&staff.Profile{}).Where("id = ?", view.ID).Update("active", false).Error; err != nil {
					return err
				}
				return tx.Model(&auth.User{}).Where("id = ?", u.ID).Update("is_active", true).Error
			}
			return nil
		}
	}
	u, created, err := w.Opts.CreateAccount(w.DB, m.Name, email, spec.role, spec.active || m.Tags["disabled"], profileFn)
	if err != nil {
		return nil, err
	}
	m.UserID, m.Name = u.ID, u.Name
	if !created && m.ProfileID == 0 && !m.Tags["no-profile"] {
		var p staff.Profile
		if e := w.DB.Where("user_id = ?", u.ID).First(&p).Error; e == nil {
			m.ProfileID = p.ID
		}
	}
	return m, nil
}

func slugCode(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r-32)
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

// rbacScenarios uses the access centre service (audited) for overrides, matrix overlays
// and account deactivation.
func rbacScenarios(w *World, actor uint) error {
	acc := access.NewService(w.DB)
	if m := w.Staff.Tagged("grant-override"); m != nil && m.ProfileID != 0 {
		if _, err := acc.SetOverride(m.ProfileID, actor, access.OverrideRequest{Permission: "billing.read", Effect: "GRANT", Reason: "Consultation des factures à l'accueil (DEMO)"}); err != nil {
			return wrapf(err, "override GRANT")
		}
	}
	if m := w.Staff.Tagged("deny-override"); m != nil && m.ProfileID != 0 {
		if _, err := acc.SetOverride(m.ProfileID, actor, access.OverrideRequest{Permission: "performed_acts.create", Effect: "DENY", Reason: "Saisie d'actes suspendue (DEMO)"}); err != nil {
			return wrapf(err, "override DENY")
		}
		m.Tags["restricted"] = true
	}
	// Matrix overlay: care assistants may read consultations in this establishment.
	if err := acc.ToggleMatrix(actor, access.MatrixToggleRequest{FunctionCode: "AIDE_SOIGNANT", Permission: "consultations.read", Effect: "GRANT", Reason: "Lecture des consultations par les aides-soignants (DEMO)"}); err != nil {
		return wrapf(err, "overlay matrice")
	}
	if m := w.Staff.Tagged("disabled"); m != nil && m.ProfileID != 0 {
		if _, err := acc.SetActive(m.ProfileID, actor, access.ActiveRequest{Active: false, Reason: "Départ de l'établissement (DEMO)"}); err != nil {
			return wrapf(err, "désactivation")
		}
		m.Tags["inactive"] = true
	}
	return nil
}

// refreshPermissions loads the effective permissions exactly as the API computes them.
func refreshPermissions(w *World) error {
	acc := access.NewService(w.DB)
	for _, m := range w.Staff.Members {
		perms, err := acc.ComputeEffectivePermissions(m.UserID)
		if err != nil {
			return err
		}
		sort.Strings(perms)
		m.Perms = perms
	}
	return nil
}
