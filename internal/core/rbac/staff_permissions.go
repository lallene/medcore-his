package rbac

var StaffPhysicianPermissions = []string{
	"patients:read", "patients:create", "patients:update", "patients.360.read", "medical_records.read", "medical_records.update",
	"consultations.read", "consultations.create", "consultations.update", "hospitalizations.read", "hospitalizations.create",
	"hospitalizations.discharge", "hospitalizations.cancel",
	"rooms.read", "beds.read", "bed_assignments.read", "laboratory.read", "imaging.read", "pharmacy.stock.read",
	"insurance.authorization.read", "insurance.authorization.create",
	"insurance.authorization.submit", "insurance.authorization.decide", "insurance.authorization.link_act",
	"billing.read",
	"act_catalog.read",
	"performed_acts.read", "performed_acts.create", "performed_acts.void",
	"organization.read",
	"queue.doctor.read", "queue.doctor.take", "queue.read.service", "queue.priority.update",
	"schedule.read.own",
}

var StaffFunctionPermissions = map[string][]string{
	"DIRECTEUR_ADMINISTRATIF": {"dashboard.read", "patients:read", "billing.read", "billing.cancel", "billing.tariff.read", "billing.tariff.manage", "act_catalog.read", "act_catalog.manage", "performed_acts.read", "performed_acts.void", "performed_acts.producer_map.read", "performed_acts.producer_map.manage", "cash.register.read", "cash.register.manage", "cash.session.read", "cash.payment.read", "cash.receipt.read", "receivables.read", "receivables.due_date.manage", "receivables.followup.create", "insurance_receivables.read", "insurance_settlements.read", "insurance_settlements.create", "insurance_settlements.allocate", "insurance_batches.read", "insurance_batches.create", "insurance_batches.submit", "insurance.authorization.read", "insurance.authorization.cancel", "insurance.authorization.decide", "staff.read", "staff.manage", "staff.audit.read", "organization.read", "organization.manage", "qa.read", "qa.audit.read", "ticket.read.all", "ticket.read.service", "ticket.comment.internal", "ticket.assign", "ticket.update", "ticket.resolve", "ticket.close", "ticket.reopen", "ticket.category.manage", "ticket.sla.manage", "ticket.audit.read", "queue.read.all", "queue.reception.read", "queue.triage.read", "queue.doctor.read", "queue.cancel", "queue.priority.update", "schedule.read.all", "schedule.manage.all", "appointment.create.all", "appointment.reschedule.all", "appointment.cancel.all", "appointment.no_show.all", "appointment_type.manage", "rbac.read", "rbac.user.manage", "rbac.override.manage", "rbac.matrix.manage", "rbac.audit.read"},
	"DIRECTEUR_MEDICAL":       {"dashboard.read", "patients:read", "patients.360.read", "medical_records.read", "consultations.read", "hospitalizations.read", "rooms.read", "beds.read", "bed_assignments.read", "laboratory.read", "imaging.read", "pharmacy.stock.read", "insurance.authorization.read", "insurance.authorization.decide", "insurance.authorization.cancel", "billing.read", "performed_acts.read", "performed_acts.void", "performed_acts.producer_map.read", "performed_acts.producer_map.manage", "organization.read", "queue.read.all", "queue.reception.read", "queue.triage.read", "queue.doctor.read", "queue.priority.update", "schedule.read.all", "schedule.manage.service", "appointment.create.service", "appointment.reschedule.service", "appointment.cancel.service", "appointment.no_show.service"},
	"SUPPORT_AGENT":           {"ticket.read.service", "ticket.comment.internal", "ticket.assign", "ticket.update", "ticket.resolve", "ticket.audit.read"},
	"SUPPORT_MANAGER":         {"ticket.read.all", "ticket.read.service", "ticket.comment.internal", "ticket.assign", "ticket.update", "ticket.resolve", "ticket.close", "ticket.reopen", "ticket.category.manage", "ticket.sla.manage", "ticket.audit.read"},
	"SAGE_FEMME":              {"patients:read", "patients.360.read", "medical_records.read", "vital_signs.create", "consultations.read", "hospitalizations.read", "beds.read", "bed_assignments.read", "performed_acts.read"},
	"ACCUEIL": {
		"patients:read", "patients:create", "patients:update",
		"queue.reception.read", "queue.checkin", "queue.cancel",
		"schedule.read.service",
		"appointment.create.service",
		"appointment.cancel.service", "appointment.no_show.service",
	},
	"AIDE_SOIGNANT": {
		"patients:read", "patients.360.read", "medical_records.read", "hospitalizations.read", "beds.read", "bed_assignments.read",
		"queue.triage.read", "queue.triage.update", "vital_signs.create",
	},
	"INFIRMIER": {
		"patients:read", "patients.360.read", "medical_records.read", "vital_signs.create", "consultations.read", "hospitalizations.read", "hospitalizations.update", "beds.read", "bed_assignments.read", "pharmacy.dispensation.read",
		"act_catalog.read", "performed_acts.read", "performed_acts.create",
		"queue.triage.read", "queue.triage.update", "queue.priority.update",
	},
	// LOT28D: operational pharmacy write actor — pharmacy permissions only.
	"PHARMACIEN": {
		"pharmacy.stock.read", "pharmacy.stock.manage",
		"pharmacy.dispensation.read", "pharmacy.dispensation.create",
		"pharmacy.references.manage",
	},
	"BIOLOGISTE":  {"patients:read", "patients.360.read", "laboratory.read", "laboratory.collect", "laboratory.process", "laboratory.result.write", "laboratory.validate", "act_catalog.read", "performed_acts.read", "performed_acts.create"},
	"RADIOLOGIE":  {"patients:read", "patients.360.read", "imaging.read", "imaging.schedule", "imaging.perform", "imaging.report.write", "imaging.validate", "act_catalog.read", "performed_acts.read", "performed_acts.create"},
	"CAISSIER":    {"billing.read", "billing.payment.create", "cash.register.read", "cash.session.read", "cash.session.open", "cash.session.close", "cash.payment.read", "cash.payment.create", "cash.receipt.read"},
	"FACTURATION": {"patients:read", "billing.read", "billing.create", "billing.issue", "billing.cancel", "billing.tariff.read", "billing.tariff.manage", "act_catalog.read", "performed_acts.read", "insurance.authorization.read", "insurance.authorization.decide", "insurance.authorization.cancel", "receivables.read", "insurance_receivables.read", "insurance_batches.read", "insurance_batches.create", "insurance_batches.submit"},
	"COMPTABLE":   {"billing.read", "billing.tariff.read", "act_catalog.read", "performed_acts.read", "cash.register.read", "cash.session.read", "cash.payment.read", "cash.receipt.read", "receivables.read", "receivables.due_date.manage", "receivables.followup.create", "insurance_receivables.read", "insurance_receivables.followup", "insurance_settlements.read", "insurance_settlements.create", "insurance_settlements.allocate", "insurance_batches.read"},
}

// EffectiveStaffPermissions keeps backward-compatible signature (no overlays/overrides).
func EffectiveStaffPermissions(role string, functions, specialties []string) []string {
	return InheritedPermissions(role, functions, specialties, nil)
}
