package staff

import "github.com/lallene/medcore-his/backend/internal/core/rbac"

var FunctionLabels = map[string]string{
	"DIRECTEUR_MEDICAL": "Directeur médical", "DIRECTEUR_ADMINISTRATIF": "Directeur administratif",
	"ACCUEIL": "Agent d'accueil",
	"SAGE_FEMME": "Sage-femme", "INFIRMIER": "Infirmier", "AIDE_SOIGNANT": "Aide-soignant",
	"COMPTABLE": "Comptable", "CAISSIER": "Caissier / Caissière", "BIOLOGISTE": "Biologiste",
	"FACTURATION": "Facturation", "RADIOLOGIE": "Radiologie",
	"SUPPORT_AGENT": "Agent support", "SUPPORT_MANAGER": "Responsable support",
}

var SpecialtyLabels = map[string]string{
	"URGENCES": "Urgentiste", "MEDECINE_GENERALE": "Médecin généraliste", "GYNECOLOGIE": "Gynécologue",
	"CARDIOLOGIE": "Cardiologue", "ORL": "ORL", "DIABETOLOGIE": "Diabétologue", "NEUROLOGIE": "Neurologue",
	"RHUMATOLOGIE": "Rhumatologue", "CHIRURGIE": "Chirurgien",
}

var CapabilityLabels = map[string]string{"ULTRASOUND": "Échographie", "XRAY": "Radiographie standard", "CT": "Scanner"}

var FunctionPermissions = rbac.StaffFunctionPermissions

func ValidFunction(code string) bool   { _, ok := FunctionLabels[code]; return ok }
func ValidSpecialty(code string) bool  { _, ok := SpecialtyLabels[code]; return ok }
func ValidCapability(code string) bool { _, ok := CapabilityLabels[code]; return ok }

func MergePermissions(role string, functions, specialties []string) []string {
	return rbac.EffectiveStaffPermissions(role, functions, specialties)
}
