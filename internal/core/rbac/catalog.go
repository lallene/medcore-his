package rbac

// PermissionMeta describes a technical permission for the catalog / ACC.
type PermissionMeta struct {
	Key       string
	Label     string
	Domain    string
	ScopeHint string // GLOBAL | SERVICE | OWN
	Sensitive bool
}

var permissionCatalog = map[string]PermissionMeta{}

func init() {
	add := func(key, label, domain, scope string) {
		permissionCatalog[key] = PermissionMeta{
			Key: key, Label: label, Domain: domain, ScopeHint: scope, Sensitive: IsSensitive(key),
		}
	}
	// Patients
	add("patients:read", "Lire les patients", "Patients", "SERVICE")
	add("patients:create", "Créer un patient", "Patients", "SERVICE")
	add("patients:update", "Modifier un patient", "Patients", "SERVICE")
	add("patients:delete", "Supprimer un patient", "Patients", "SERVICE")
	add("patients.360.read", "Voir le Patient 360", "Patients", "SERVICE")
	add("medical_records.read", "Lire le dossier médical", "Patients", "SERVICE")
	add("medical_records.update", "Modifier le dossier médical", "Patients", "SERVICE")
	add("vital_signs.create", "Saisir des constantes", "Patients", "SERVICE")
	// Queue
	add("queue.reception.read", "Voir la file accueil", "Patient Queue", "SERVICE")
	add("queue.checkin", "Enregistrer une arrivée", "Patient Queue", "SERVICE")
	add("queue.cancel", "Annuler un ticket file", "Patient Queue", "SERVICE")
	add("queue.triage.read", "Voir le pré-triage", "Patient Queue", "SERVICE")
	add("queue.triage.update", "Prendre / valider le triage", "Patient Queue", "SERVICE")
	add("queue.doctor.read", "Voir la file médecin", "Patient Queue", "SERVICE")
	add("queue.doctor.take", "Prendre en charge un patient", "Patient Queue", "SERVICE")
	add("queue.read.service", "Lire la file (scope service)", "Patient Queue", "SERVICE")
	add("queue.read.all", "Lire toute la file (global)", "Patient Queue", "GLOBAL")
	add("queue.priority.update", "Modifier la priorité", "Patient Queue", "SERVICE")
	// Medical scheduling LOT 23B
	add("schedule.read.own", "Lire mon planning", "Scheduling", "OWN")
	add("schedule.read.service", "Lire les plannings du service", "Scheduling", "SERVICE")
	add("schedule.read.all", "Lire tous les plannings", "Scheduling", "GLOBAL")
	add("schedule.manage.own", "Gérer mon planning", "Scheduling", "OWN")
	add("schedule.manage.service", "Gérer les plannings du service", "Scheduling", "SERVICE")
	add("schedule.manage.all", "Gérer tous les plannings", "Scheduling", "GLOBAL")
	// Consultations / hosp
	add("consultations.read", "Lire les consultations", "Consultations", "SERVICE")
	add("consultations.create", "Créer une consultation", "Consultations", "SERVICE")
	add("consultations.update", "Modifier une consultation", "Consultations", "SERVICE")
	add("hospitalizations.read", "Lire les hospitalisations", "Hospitalisation", "SERVICE")
	add("hospitalizations.create", "Créer une hospitalisation", "Hospitalisation", "SERVICE")
	add("hospitalizations.update", "Modifier une hospitalisation", "Hospitalisation", "SERVICE")
	add("rooms.read", "Lire les chambres", "Hospitalisation", "SERVICE")
	add("beds.read", "Lire les lits", "Hospitalisation", "SERVICE")
	add("bed_assignments.read", "Lire les affectations de lit", "Hospitalisation", "SERVICE")
	// Lab / imaging / pharmacy
	add("laboratory.read", "Lire le laboratoire", "Laboratoire", "SERVICE")
	add("laboratory.collect", "Collecter un échantillon", "Laboratoire", "SERVICE")
	add("laboratory.process", "Traiter un examen labo", "Laboratoire", "SERVICE")
	add("laboratory.result.write", "Saisir un résultat labo", "Laboratoire", "SERVICE")
	add("laboratory.validate", "Valider un résultat labo", "Laboratoire", "SERVICE")
	add("imaging.read", "Lire l'imagerie", "Imagerie", "SERVICE")
	add("imaging.schedule", "Planifier une imagerie", "Imagerie", "SERVICE")
	add("imaging.perform", "Réaliser une imagerie", "Imagerie", "SERVICE")
	add("imaging.report.write", "Rédiger un compte rendu", "Imagerie", "SERVICE")
	add("imaging.validate", "Valider une imagerie", "Imagerie", "SERVICE")
	add("pharmacy.stock.read", "Lire le stock pharmacie", "Pharmacie", "SERVICE")
	add("pharmacy.dispensation.read", "Lire les délivrances", "Pharmacie", "SERVICE")
	// Billing / cash / insurance
	add("billing.read", "Lire la facturation", "Billing", "SERVICE")
	add("billing.create", "Créer une facture", "Billing", "SERVICE")
	add("billing.issue", "Émettre une facture", "Billing", "SERVICE")
	add("billing.cancel", "Annuler une facture", "Billing", "SERVICE")
	add("billing.tariff.read", "Lire les tarifs", "Billing", "GLOBAL")
	add("cash.register.read", "Lire les caisses", "Cash", "SERVICE")
	add("cash.session.read", "Lire les sessions de caisse", "Cash", "SERVICE")
	add("cash.session.open", "Ouvrir une session de caisse", "Cash", "SERVICE")
	add("cash.session.close", "Clôturer une session de caisse", "Cash", "SERVICE")
	add("cash.payment.read", "Lire les paiements", "Cash", "SERVICE")
	add("cash.payment.create", "Enregistrer un paiement", "Cash", "SERVICE")
	add("cash.receipt.read", "Lire les reçus", "Cash", "SERVICE")
	add("insurance.authorization.read", "Lire les autorisations PEC", "Insurance", "SERVICE")
	add("insurance.authorization.create", "Créer une autorisation PEC", "Insurance", "SERVICE")
	add("receivables.read", "Lire les créances patients", "Receivables", "SERVICE")
	add("insurance_receivables.read", "Lire les créances assureurs", "Receivables", "SERVICE")
	add("insurance_receivables.followup", "Suivi créances assureurs", "Receivables", "SERVICE")
	add("insurance_settlements.read", "Lire les règlements assureurs", "Receivables", "SERVICE")
	add("insurance_settlements.create", "Créer un règlement assureur", "Receivables", "SERVICE")
	add("insurance_settlements.allocate", "Allouer un règlement", "Receivables", "SERVICE")
	add("insurance_batches.read", "Lire les lots assureurs", "Receivables", "SERVICE")
	add("insurance_batches.create", "Créer un lot assureur", "Receivables", "SERVICE")
	add("insurance_batches.submit", "Soumettre un lot assureur", "Receivables", "SERVICE")
	// Staff / org / QA / tickets
	add("staff.read", "Lire le personnel", "Staff", "GLOBAL")
	add("staff.manage", "Gérer le personnel", "Staff", "GLOBAL")
	add("staff.audit.read", "Lire l'audit personnel", "Staff", "GLOBAL")
	add("organization.read", "Lire l'organisation", "Organization", "GLOBAL")
	add("organization.manage", "Gérer l'organisation", "Organization", "GLOBAL")
	add("dashboard.read", "Voir le tableau de bord", "Administration", "GLOBAL")
	add("qa.read", "Lire Automated QA", "QA", "GLOBAL")
	add("qa.audit.read", "Lire l'audit QA", "QA", "GLOBAL")
	add("ticket.create", "Créer un ticket", "Ticketing", "OWN")
	add("ticket.read.own", "Lire ses tickets", "Ticketing", "OWN")
	add("ticket.comment", "Commenter un ticket", "Ticketing", "OWN")
	add("ticket.read.service", "Lire les tickets du service", "Ticketing", "SERVICE")
	add("ticket.read.all", "Lire tous les tickets", "Ticketing", "GLOBAL")
	add("ticket.comment.internal", "Commentaire interne ticket", "Ticketing", "SERVICE")
	add("ticket.assign", "Assigner un ticket", "Ticketing", "SERVICE")
	add("ticket.update", "Mettre à jour un ticket", "Ticketing", "SERVICE")
	add("ticket.resolve", "Résoudre un ticket", "Ticketing", "SERVICE")
	add("ticket.close", "Clôturer un ticket", "Ticketing", "SERVICE")
	add("ticket.reopen", "Réouvrir un ticket", "Ticketing", "SERVICE")
	add("ticket.audit.read", "Lire l'historique ticket", "Ticketing", "SERVICE")
	add("ticket.category.manage", "Gérer les catégories ticket", "Ticketing", "GLOBAL")
	add("ticket.sla.manage", "Gérer les SLA ticket", "Ticketing", "GLOBAL")
	// RBAC admin LOT 21
	add("rbac.read", "Lire le centre d'accès", "RBAC", "GLOBAL")
	add("rbac.user.manage", "Gérer les accès utilisateurs", "RBAC", "GLOBAL")
	add("rbac.override.manage", "Gérer les exceptions GRANT/DENY", "RBAC", "GLOBAL")
	add("rbac.matrix.manage", "Modifier la matrice fonctions×permissions", "RBAC", "GLOBAL")
	add("rbac.audit.read", "Lire l'audit RBAC", "RBAC", "GLOBAL")
	add("*", "Accès total (wildcard)", "Administration", "GLOBAL")
}

// LookupPermissionMeta returns catalog metadata (fallback for unknown keys).
func LookupPermissionMeta(key string) PermissionMeta {
	if m, ok := permissionCatalog[key]; ok {
		return m
	}
	return PermissionMeta{Key: key, Label: key, Domain: "Autre", ScopeHint: "SERVICE", Sensitive: IsSensitive(key)}
}

// AllPermissionMetas returns the full catalog sorted by domain then key.
func AllPermissionMetas() []PermissionMeta {
	out := make([]PermissionMeta, 0, len(permissionCatalog))
	for _, m := range permissionCatalog {
		out = append(out, m)
	}
	return out
}

// KnownPermissionKeys collects every permission appearing in the static matrix + catalog.
func KnownPermissionKeys() []string {
	set := map[string]bool{}
	for k := range permissionCatalog {
		set[k] = true
	}
	for _, list := range StaffFunctionPermissions {
		for _, p := range list {
			set[p] = true
		}
	}
	for _, p := range StaffPhysicianPermissions {
		set[p] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}
