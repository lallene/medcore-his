package complete

import (
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
)

func init() {
	validators = append(validators, validatePeriod, validatePatients, validateAgenda, validateClinical, validateTechnical,
		validatePharmacy, validateActs, validateBilling, validateCash, validateInsurance, validateSupport,
		validateNotifications, validateDocuments, validatePagination, validateIntegrity)
}

func (v *validator) expected(base int) int64 { return int64(float64(base) * v.cfg.Scale()) }

func validatePeriod(v *validator) {
	d := "Période / volumétrie temporelle"
	now := v.cfg.SimulationNow()
	days := v.count(d, "jours distincts avec consultations", "SELECT COUNT(DISTINCT (created_at AT TIME ZONE ?)::date) FROM consultations", v.cfg.Location.String())
	v.check(d, fmt.Sprintf("activité sur >= 85%% des %d jours", v.cfg.Days), true, days >= int64(float64(v.cfg.Days)*0.85), fmt.Sprintf("%d jours", days))
	v.atLeast(d, "activité dès la première semaine", "SELECT COUNT(*) FROM consultations WHERE created_at < ?", 1, true, v.cfg.FirstDay().AddDate(0, 0, 7))
	v.atLeast(d, "activité le jour d'ancrage", "SELECT COUNT(*) FROM patient_queue_tickets WHERE created_at >= ?", 1, true, v.cfg.Anchor)
	v.atLeast(d, "mois couverts", "SELECT COUNT(DISTINCT date_trunc('month', created_at)) FROM consultations", int64(v.cfg.Days/31), true)
	v.zero(d, "aucune ligne datée après le snapshot", `SELECT (SELECT COUNT(*) FROM consultations WHERE created_at > ?) + (SELECT COUNT(*) FROM billing_invoices WHERE created_at > ?)
		+ (SELECT COUNT(*) FROM patient_queue_tickets WHERE created_at > ?) + (SELECT COUNT(*) FROM billing_payments WHERE paid_at > ?) + (SELECT COUNT(*) FROM laboratory_orders WHERE created_at > ?)`, true, now, now, now, now, now)
	v.zero(d, "aucune ligne antérieure à la période", "SELECT COUNT(*) FROM consultations WHERE created_at < ?", true, v.cfg.FirstDay().AddDate(0, 0, -1))
}

func validatePatients(v *validator) {
	d := "Patients / dossiers médicaux"
	now := v.cfg.SimulationNow()
	v.atLeast(d, "patients", "SELECT COUNT(*) FROM patients", int64(v.cfg.Patients), true)
	v.atLeast(d, "dossiers médicaux", "SELECT COUNT(*) FROM medical_records", int64(v.cfg.Patients), true)
	v.atLeast(d, "enfants (< 15 ans)", "SELECT COUNT(*) FROM patients WHERE date_naissance > ?::timestamptz - interval '15 years'", v.expected(150), true, now)
	v.atLeast(d, "personnes âgées (>= 65 ans)", "SELECT COUNT(*) FROM patients WHERE date_naissance < ?::timestamptz - interval '65 years'", v.expected(80), true, now)
	v.atLeast(d, "homonymes (nom + prénoms)", "SELECT COUNT(*) FROM (SELECT nom, prenoms FROM patients GROUP BY 1,2 HAVING COUNT(*) > 1) x", 1, false)
	v.atLeast(d, "patients sans couverture", "SELECT COUNT(*) FROM patients p WHERE NOT EXISTS (SELECT 1 FROM patient_coverages c WHERE c.patient_id = p.id)", v.expected(300), true)
	v.atLeast(d, "couvertures principales", "SELECT COUNT(*) FROM patient_coverages WHERE is_principal", v.expected(300), true)
	v.atLeast(d, "couvertures secondaires", "SELECT COUNT(*) FROM patient_coverages WHERE NOT is_principal", v.expected(20), true)
	v.atLeast(d, "couvertures expirées", "SELECT COUNT(*) FROM patient_coverages WHERE valid_to < ?", v.expected(15), true, now)
	v.atLeast(d, "allergies", "SELECT COUNT(*) FROM allergies", v.expected(80), true)
	v.atLeast(d, "antécédents", "SELECT COUNT(*) FROM medical_histories", v.expected(200), true)
	v.atLeast(d, "alertes", "SELECT COUNT(*) FROM medical_alerts", v.expected(30), true)
	v.atLeast(d, "vaccinations", "SELECT COUNT(*) FROM vaccinations", v.expected(150), true)
	v.atLeast(d, "constantes (séries)", "SELECT COUNT(*) FROM vital_signs", v.expected(3000), true)
	v.atLeast(d, "documents médicaux", "SELECT COUNT(*) FROM medical_documents", v.expected(100), true)
	v.atLeast(d, "traitements de fond", "SELECT COUNT(*) FROM regular_treatments", v.expected(60), true)
	v.atLeast(d, "événements de timeline", "SELECT COUNT(*) FROM medical_timeline_events", v.expected(20000), true)
	v.atLeast(d, "dossiers minimaux (aucune constante hors parcours)", "SELECT COUNT(*) FROM medical_records r WHERE NOT EXISTS (SELECT 1 FROM patient_medical_profiles p WHERE p.medical_record_id = r.id)", v.expected(100), false)
}

func validateAgenda(v *validator) {
	d := "Agenda / file d'attente"
	v.atLeast(d, "rendez-vous", "SELECT COUNT(*) FROM patient_queue_appointments", v.expected(3000), true)
	v.statuses(d, "patient_queue_appointments", "status", []string{"SCHEDULED", "COMPLETED", "CANCELLED", "NO_SHOW"}, true)
	// Depends on the time of the run (no appointment is in progress at night).
	v.statuses(d, "patient_queue_appointments", "status", []string{"CHECKED_IN", "IN_PROGRESS"}, false)
	v.atLeast(d, "reports de RDV", "SELECT COUNT(*) FROM patient_queue_appointment_history WHERE event_type = 'RESCHEDULED'", v.expected(40), true)
	v.atLeast(d, "check-in annulés (ticket annulé)", "SELECT COUNT(*) FROM patient_queue_appointment_history WHERE event_type = 'CHECK_IN_REVERSED'", 1, false)
	v.atLeast(d, "séries de RDV", "SELECT COUNT(*) FROM patient_queue_appointment_series", v.expected(20), true)
	v.statuses(d, "patient_queue_appointment_series", "status", []string{"ACTIVE", "CANCELLED"}, true)
	v.atLeast(d, "RDV futurs", "SELECT COUNT(*) FROM patient_queue_appointments WHERE scheduled_at > ? AND status = 'SCHEDULED'", v.expected(30), true, v.cfg.SimulationNow())
	v.atLeast(d, "exceptions de planning", "SELECT COUNT(*) FROM patient_queue_schedule_exceptions", 20, true)
	v.statuses(d, "patient_queue_schedule_exceptions", "type", []string{"ABSENCE", "LEAVE", "MEETING", "BLOCKED", "TRAINING", "OTHER", "EXTRA_AVAILABILITY"}, true)
	v.atLeast(d, "services avec RDV", "SELECT COUNT(DISTINCT service_id) FROM patient_queue_appointments", 8, true)
	v.atLeast(d, "praticiens avec RDV", "SELECT COUNT(DISTINCT expected_doctor_id) FROM patient_queue_appointments", 12, true)
	v.atLeast(d, "tickets de file", "SELECT COUNT(*) FROM patient_queue_tickets", v.expected(3000), true)
	v.statuses(d, "patient_queue_tickets", "source", []string{"APPOINTMENT", "WALK_IN"}, true)
	v.statuses(d, "patient_queue_tickets", "priority", []string{"URGENT", "HIGH", "NORMAL", "LOW"}, true)
	v.statuses(d, "patient_queue_tickets", "stage", []string{"COMPLETED", "CANCELLED", "WAITING_TRIAGE", "WAITING_DOCTOR", "DOCTOR_IN_PROGRESS"}, false)
	v.atLeast(d, "dérogations financières au check-in", "SELECT COUNT(*) FROM patient_queue_tickets WHERE finance_override", 1, false)
	v.atLeast(d, "file active à l'instant présent", "SELECT COUNT(*) FROM patient_queue_tickets WHERE status = 'ACTIVE'", 1, true)
}

func validateClinical(v *validator) {
	d := "Consultations"
	v.atLeast(d, "consultations", "SELECT COUNT(*) FROM consultations", v.expected(3000), true)
	v.statuses(d, "consultations", "status", []string{"draft", "in_progress", "completed", "cancelled"}, true)
	v.atLeast(d, "consultations avec constantes liées", "SELECT COUNT(DISTINCT consultation_id) FROM vital_signs WHERE consultation_id IS NOT NULL", v.expected(1500), true)
	v.atLeast(d, "SOAP", "SELECT COUNT(*) FROM consultation_soaps", v.expected(1500), true)
	v.atLeast(d, "données de spécialité", "SELECT COUNT(*) FROM consultation_specialty_data", v.expected(300), true)
	v.atLeast(d, "prescriptions", "SELECT COUNT(*) FROM consultation_prescriptions", v.expected(1500), true)
	v.atLeast(d, "demandes d'examen", "SELECT COUNT(*) FROM consultation_exam_requests", v.expected(1500), true)
	v.atLeast(d, "examens physiques", "SELECT COUNT(*) FROM consultation_physical_exams", v.expected(2000), true)
	v.atLeast(d, "traitements administrés", "SELECT COUNT(*) FROM consultation_administered_treatments", v.expected(100), true)
	v.atLeast(d, "antécédents gynéco-obstétricaux", "SELECT COUNT(*) FROM consultation_gyneco_obstetric_histories", 5, false)
	v.zero(d, "consultation terminée sans date de fin", "SELECT COUNT(*) FROM consultations WHERE status = 'completed' AND completed_at IS NULL", true)
	v.zero(d, "consultation de file terminée sans acte réalisé", `SELECT COUNT(*) FROM consultations c JOIN patient_queue_tickets t ON t.consultation_id = c.id
		WHERE c.status = 'completed' AND NOT EXISTS (SELECT 1 FROM performed_acts a WHERE a.consultation_id = c.id)`, true)
	v.zero(d, "consultation en cours avant aujourd'hui", "SELECT COUNT(*) FROM consultations WHERE status = 'in_progress' AND started_at < ?", true, v.cfg.Anchor)
}

func validateTechnical(v *validator) {
	d := "Laboratoire / imagerie / hospitalisation"
	v.atLeast(d, "demandes de laboratoire", "SELECT COUNT(*) FROM laboratory_orders", v.expected(1500), true)
	v.statuses(d, "laboratory_orders", "status", []string{"ORDERED", "SAMPLE_PENDING", "VALIDATED", "CANCELLED"}, true)
	v.statuses(d, "laboratory_orders", "status", []string{"SAMPLE_COLLECTED", "IN_PROGRESS", "RESULT_ENTERED"}, false)
	v.atLeast(d, "résultats anormaux", "SELECT COUNT(*) FROM laboratory_results WHERE flag <> 'NORMAL'", v.expected(200), true)
	v.zero(d, "examen validé sans prélèvement ni résultat", `SELECT COUNT(*) FROM laboratory_orders o WHERE o.status = 'VALIDATED' AND (o.validated_at IS NULL
		OR NOT EXISTS (SELECT 1 FROM laboratory_samples s WHERE s.order_id = o.id) OR NOT EXISTS (SELECT 1 FROM laboratory_results r WHERE r.order_id = o.id))`, true)
	v.atLeast(d, "demandes d'imagerie", "SELECT COUNT(*) FROM imaging_orders", v.expected(500), true)
	v.statuses(d, "imaging_orders", "status", []string{"ORDERED", "SCHEDULED", "VALIDATED", "REPORT_CLOSED", "CANCELLED"}, true)
	v.statuses(d, "imaging_orders", "status", []string{"IN_PROGRESS", "REPORT_DRAFTED"}, false)
	v.statuses(d, "imaging_orders", "modality", []string{"XRAY", "ULTRASOUND", "CT"}, true)
	v.zero(d, "imagerie validée sans compte rendu", "SELECT COUNT(*) FROM imaging_orders o WHERE o.status = 'VALIDATED' AND NOT EXISTS (SELECT 1 FROM imaging_reports r WHERE r.order_id = o.id)", true)
	v.atLeast(d, "hospitalisations", "SELECT COUNT(*) FROM hospitalizations", v.expected(80), true)
	v.statuses(d, "hospitalizations", "status", []string{"ADMITTED", "DISCHARGED", "CANCELLED"}, true)
	v.statuses(d, "hospitalizations", "status", []string{"PLANNED"}, false)
	v.statuses(d, "hospitalization_beds", "status", []string{"AVAILABLE", "OCCUPIED", "OUT_OF_SERVICE"}, true)
	v.statuses(d, "hospitalization_beds", "status", []string{"RESERVED"}, false)
	v.atLeast(d, "transferts de lit", "SELECT COUNT(*) FROM (SELECT hospitalization_id FROM hospitalization_bed_assignments GROUP BY 1 HAVING COUNT(*) > 1) x", 1, true)
	v.zero(d, "sortie sans date ni diagnostic", "SELECT COUNT(*) FROM hospitalizations WHERE status = 'DISCHARGED' AND (discharged_at IS NULL OR COALESCE(discharge_diagnosis,'') = '')", true)
}

func validatePharmacy(v *validator) {
	d := "Pharmacie"
	v.atLeast(d, "dispensations", "SELECT COUNT(*) FROM pharmacy_dispensations", v.expected(1500), true)
	v.atLeast(d, "mouvements de stock", "SELECT COUNT(*) FROM stock_movements", v.expected(1500), true)
	v.statuses(d, "stock_movements", "type", []string{"BATCH_ENTRY", "DISPENSATION"}, true)
	v.atLeast(d, "lots", "SELECT COUNT(*) FROM pharmacy_batches", 40, true)
	v.atLeast(d, "présentations en rupture ou stock bas", "SELECT COUNT(*) FROM pharmacy_stocks WHERE quantity_available <= GREATEST(alert_threshold, 20)", 1, true)
	v.atLeast(d, "bons pharmacie", "SELECT COUNT(*) FROM pharmacy_vouchers", v.expected(1000), true)
	v.atLeast(d, "prescriptions non délivrées (en attente)", "SELECT COUNT(*) FROM consultation_prescriptions p WHERE NOT EXISTS (SELECT 1 FROM pharmacy_dispensations d WHERE d.reference_type = 'CONSULTATION_PRESCRIPTION' AND d.reference_id = p.id)", v.expected(200), true)
	v.atLeast(d, "délivrances partielles", "SELECT COUNT(*) FROM consultation_prescriptions p WHERE (SELECT COALESCE(SUM(quantity),0) FROM pharmacy_dispensations d WHERE d.reference_type = 'CONSULTATION_PRESCRIPTION' AND d.reference_id = p.id) BETWEEN 0.01 AND p.quantity - 0.01", v.expected(50), true)
}

func validateActs(v *validator) {
	d := "Actes réalisés"
	v.atLeast(d, "actes réalisés", "SELECT COUNT(*) FROM performed_acts", v.expected(4000), true)
	v.statuses(d, "performed_acts", "status", []string{"PERFORMED", "VOIDED"}, true)
	v.statuses(d, "performed_acts", "source_type", []string{"CONSULTATION", "LABORATORY", "IMAGING"}, true)
	v.atLeast(d, "actes d'hospitalisation", "SELECT COUNT(*) FROM performed_acts WHERE hospitalization_id IS NOT NULL", v.expected(150), true)
	v.atLeast(d, "correspondances producteur → acte", "SELECT COUNT(*) FROM act_catalog_producer_maps WHERE is_active", 20, true)
}

func validateBilling(v *validator) {
	d := "Facturation"
	v.atLeast(d, "factures", "SELECT COUNT(*) FROM billing_invoices", v.expected(3000), true)
	v.statuses(d, "billing_invoices", "status", []string{"DRAFT", "ISSUED", "PARTIALLY_PAID", "PAID", "CANCELLED"}, true)
	v.atLeast(d, "factures multi-lignes", "SELECT COUNT(*) FROM (SELECT invoice_id FROM billing_invoice_lines GROUP BY 1 HAVING COUNT(*) > 1) x", v.expected(500), true)
	v.atLeast(d, "factures avec part assurance", "SELECT COUNT(*) FROM billing_invoices WHERE insurance_amount > 0", v.expected(500), true)
	v.atLeast(d, "paiements", "SELECT COUNT(*) FROM billing_payments", v.expected(2500), true)
	v.statuses(d, "billing_payments", "payment_method", []string{"CASH", "CARD", "MOBILE_MONEY", "BANK_TRANSFER", "CHECK"}, true)
	v.statuses(d, "billing_payments", "payer_kind", []string{"INDIVIDUAL", "ORGANIZATION"}, true)
	v.atLeast(d, "paiements par un tiers (proche)", "SELECT COUNT(*) FROM billing_payments WHERE payer_kind = 'INDIVIDUAL' AND NOT payer_is_patient", v.expected(200), true)
	v.atLeast(d, "factures à paiements multiples", "SELECT COUNT(*) FROM (SELECT invoice_id FROM billing_payments GROUP BY 1 HAVING COUNT(*) > 1) x", v.expected(100), true)
	v.zero(d, "paiement CASH hors session de caisse", "SELECT COUNT(*) FROM billing_payments WHERE payment_method = 'CASH' AND cash_session_id IS NULL", true)
	v.zero(d, "facture payée avec solde", "SELECT COUNT(*) FROM billing_invoices WHERE status = 'PAID' AND balance_amount <> 0", true)
	v.atLeast(d, "contrepassations", "SELECT COUNT(*) FROM billing_payment_reversals", v.expected(30), true)
	v.atLeast(d, "avoirs", "SELECT COUNT(*) FROM billing_credit_notes", v.expected(40), true)
	v.atLeast(d, "applications de crédit", "SELECT COUNT(*) FROM billing_credit_applications", v.expected(20), true)
	v.atLeast(d, "annulations d'application de crédit", "SELECT COUNT(*) FROM billing_credit_application_reversals", 1, true)
	v.statuses(d, "billing_credit_ledger_entries", "entry_type", []string{"CREDIT", "APPLY", "REFUND"}, true)
	v.statuses(d, "billing_refunds", "status", []string{"REQUESTED", "APPROVED", "REJECTED", "CANCELLED", "EXECUTED"}, true)
	v.zero(d, "remboursement exécuté sans exécution", "SELECT COUNT(*) FROM billing_refunds r WHERE r.status = 'EXECUTED' AND NOT EXISTS (SELECT 1 FROM billing_refund_executions e WHERE e.refund_id = r.id)", true)
	v.zero(d, "séparation des tâches violée", `SELECT COUNT(*) FROM billing_refunds r LEFT JOIN billing_refund_executions e ON e.refund_id = r.id
		WHERE r.approved_by = r.requested_by OR e.executed_by = r.approved_by`, true)
	v.atLeast(d, "créances patients ouvertes", "SELECT COUNT(*) FROM billing_invoices WHERE status IN ('ISSUED','PARTIALLY_PAID') AND balance_amount > 0", v.expected(300), true)
	v.atLeast(d, "relances patients", "SELECT COUNT(*) FROM patient_receivable_follow_ups", 20, true)
}

func validateCash(v *validator) {
	d := "Caisse"
	v.atLeast(d, "sessions de caisse", "SELECT COUNT(*) FROM cash_sessions", int64(v.cfg.Days*2), true)
	// Historical invariant: sessions are closed every day; only the snapshot day may hold
	// open sessions.
	v.statuses(d, "cash_sessions", "status", []string{"CLOSED"}, true)
	v.zero(d, "session restée ouverte avant le jour du snapshot", "SELECT COUNT(*) FROM cash_sessions WHERE status = 'OPEN' AND opened_at < ?", true, v.cfg.Anchor)
	// Snapshot: open desks are expected only when the simulated present falls inside the
	// desks' opening hours (always the case at the 12:00 snapshot).
	if cashDeskOpenAt(v.cfg.SimulationNow()) {
		v.atLeast(d, "session ouverte à l'instant simulé", "SELECT COUNT(*) FROM cash_sessions WHERE status = 'OPEN' AND opened_at <= ?", 1, true, v.cfg.SimulationNow())
	}
	v.atLeast(d, "caisses utilisées", "SELECT COUNT(DISTINCT cash_register_id) FROM cash_sessions", 3, true)
	v.atLeast(d, "reçus", "SELECT COUNT(*) FROM cash_receipts", v.expected(2000), true)
	v.statuses(d, "cash_movements", "direction", []string{"IN", "OUT"}, true)
	v.atLeast(d, "corrections après clôture", "SELECT COUNT(*) FROM cash_correction_executions", 5, true)
	v.atLeast(d, "clôtures avec écart", "SELECT COUNT(*) FROM cash_sessions WHERE status = 'CLOSED' AND counted_cash_amount <> expected_cash_amount", 5, true)
	v.zero(d, "plus d'une session ouverte par caisse", "SELECT COUNT(*) FROM (SELECT cash_register_id FROM cash_sessions WHERE status = 'OPEN' GROUP BY 1 HAVING COUNT(*) > 1) x", true)
	v.zero(d, "session clôturée sans date", "SELECT COUNT(*) FROM cash_sessions WHERE status = 'CLOSED' AND closed_at IS NULL", true)
}

// cashDeskOpenAt mirrors the generator's desk hours (emergency desk 06:30–22:00 every day).
func cashDeskOpenAt(t time.Time) bool {
	m := t.Hour()*60 + t.Minute()
	return m >= 6*60+30 && m < 22*60
}

func validateInsurance(v *validator) {
	d := "Assurance / créances assureurs"
	v.atLeast(d, "assureurs actifs", "SELECT COUNT(*) FROM insurance_companies WHERE is_active", 4, true)
	v.atLeast(d, "garants", "SELECT COUNT(*) FROM insurance_guarantors", 5, true)
	v.statuses(d, "insurance_authorizations", "status", []string{"APPROVED", "PARTIALLY_APPROVED", "REJECTED", "CANCELLED"}, true)
	v.statuses(d, "insurance_authorizations", "status", []string{"DRAFT", "SUBMITTED", "PENDING"}, false)
	v.atLeast(d, "PEC avec actes rattachés", "SELECT COUNT(*) FROM insurance_authorization_acts", v.expected(300), true)
	// Historical invariants (persistent proof): terminal states present, and every voucher
	// that left "draft" has the workflow_histories row of that transition; the whole cycle
	// is demonstrated by the history (dashboard source).
	v.statuses(d, "insurance_vouchers", "status", voucherTerminalStatuses(), true)
	v.zero(d, "bon PEC hors draft sans passage par draft tracé", `SELECT COUNT(*) FROM insurance_vouchers b WHERE b.status <> 'draft'
		AND NOT EXISTS (SELECT 1 FROM workflow_histories h WHERE h.entity_name = 'InsuranceVoucher' AND h.entity_id = b.id AND h.from_state = 'draft')`, true)
	for _, tr := range voucherCycle() {
		v.atLeast(d, "transition de bon PEC "+tr[0]+" → "+tr[1], "SELECT COUNT(*) FROM workflow_histories WHERE entity_name = 'InsuranceVoucher' AND from_state = ? AND to_state = ?", 1, true, tr[0], tr[1])
	}
	// Snapshot: vouchers still in progress at the simulated present (depends on the
	// last days' activity; informative).
	v.statuses(d, "insurance_vouchers", "status", []string{"draft", "submitted", "controlled"}, false)
	v.atLeast(d, "historique de workflow (dashboard)", "SELECT COUNT(*) FROM workflow_histories", v.expected(500), true)
	v.atLeast(d, "bordereaux", "SELECT COUNT(*) FROM insurance_submission_batches", 5, true)
	v.statuses(d, "insurance_submission_batches", "status", []string{"SUBMITTED"}, true)
	v.statuses(d, "insurance_submission_batches", "status", []string{"DRAFT"}, false)
	v.statuses(d, "insurance_settlements", "status", []string{"POSTED", "CANCELLED"}, true)
	v.atLeast(d, "ventilations de règlements", "SELECT COUNT(*) FROM insurance_settlement_allocations", v.expected(100), true)
	v.atLeast(d, "relances assureurs", "SELECT COUNT(*) FROM insurance_receivable_followups", 10, true)
}

func voucherTerminalStatuses() []string { return []string{"validated", "rejected", "cancelled"} }

func voucherCycle() [][2]string {
	return [][2]string{{"draft", "submitted"}, {"draft", "cancelled"}, {"submitted", "controlled"}, {"submitted", "rejected"}, {"controlled", "validated"}, {"controlled", "rejected"}, {"controlled", "cancelled"}}
}

func validateSupport(v *validator) {
	d := "Support"
	v.atLeast(d, "tickets de support", "SELECT COUNT(*) FROM ticketing_tickets", v.expected(300), true)
	v.statuses(d, "ticketing_tickets", "status", []string{"NEW", "TRIAGED", "ASSIGNED", "IN_PROGRESS", "WAITING_USER", "WAITING_THIRD_PARTY", "RESOLVED", "CLOSED", "REOPENED", "CANCELLED"}, true)
	v.statuses(d, "ticketing_tickets", "priority", []string{"P1", "P2", "P3", "P4"}, true)
	v.atLeast(d, "SLA de résolution dépassés", "SELECT COUNT(*) FROM ticketing_tickets WHERE (resolved_at IS NULL AND resolution_due_at < ?) OR resolved_at > resolution_due_at", 10, true, v.cfg.SimulationNow())
	v.atLeast(d, "commentaires", "SELECT COUNT(*) FROM ticketing_comments", v.expected(300), true)
	v.atLeast(d, "historique", "SELECT COUNT(*) FROM ticketing_history", v.expected(1000), true)
	v.atLeast(d, "notifications internes", "SELECT COUNT(*) FROM ticketing_notifications", v.expected(300), true)
}

func validateNotifications(v *validator) {
	d := "Notifications de rendez-vous"
	v.statuses(d, "appointment_notification_intents", "status", []string{"PENDING", "SENT", "SKIPPED", "CANCELLED", "FAILED"}, true)
	v.statuses(d, "appointment_notification_intents", "status", []string{"PROCESSING"}, false)
	v.statuses(d, "appointment_notification_intents", "channel", []string{"LOG", "EMAIL"}, true)
	v.atLeast(d, "tentatives d'envoi", "SELECT COUNT(*) FROM appointment_notification_attempts", v.expected(1000), true)
}

// validateDocuments generates the 5 consultation PDFs for real and counts the data behind
// every printable document.
func validateDocuments(v *validator) {
	d := "Documents générables"
	repo := consultations.NewRepository(v.db)
	pdfs := []struct {
		name  string
		where string
		gen   func(*consultations.Consultation) ([]byte, error)
	}{
		{"arrêt de travail", "sick_leave_required", consultations.GenerateSickLeavePDF},
		{"demande d'examen", "EXISTS (SELECT 1 FROM consultation_exam_requests e WHERE e.consultation_id = consultations.id)", consultations.GenerateExamRequestPDF},
		{"ordonnance", "EXISTS (SELECT 1 FROM consultation_prescriptions p WHERE p.consultation_id = consultations.id)", consultations.GeneratePrescriptionPDF},
		{"compte rendu", "status = 'completed'", consultations.GenerateConsultationReportPDF},
		{"bon d'hospitalisation", "hospitalization_required", consultations.GenerateHospitalizationPDF},
	}
	for _, p := range pdfs {
		var ids []uint
		v.db.Raw("SELECT id FROM consultations WHERE " + p.where + " ORDER BY id DESC LIMIT 3").Scan(&ids)
		ok, detail := len(ids) >= 3, fmt.Sprintf("%d consultation(s) éligible(s)", len(ids))
		for _, id := range ids {
			c, err := repo.FindByID(id)
			if err != nil {
				ok, detail = false, err.Error()
				break
			}
			b, err := p.gen(c)
			if err != nil || len(b) < 800 {
				ok, detail = false, fmt.Sprintf("consultation %d : génération en échec", id)
				break
			}
		}
		v.check(d, "PDF "+p.name+" (3 exemples générés)", true, ok, detail)
	}
	v.atLeast(d, "reçus de caisse", "SELECT COUNT(*) FROM cash_receipts", 10, true)
	v.atLeast(d, "avoirs imprimables", "SELECT COUNT(*) FROM billing_credit_notes", 3, true)
	v.atLeast(d, "bons de remboursement (RMB)", "SELECT COUNT(*) FROM billing_refunds WHERE status = 'EXECUTED' AND refund_number IS NOT NULL", 3, true)
	v.atLeast(d, "bons PEC", "SELECT COUNT(*) FROM insurance_vouchers", 10, true)
	v.atLeast(d, "relevés financiers (patients facturés)", "SELECT COUNT(DISTINCT patient_id) FROM billing_invoices WHERE status <> 'DRAFT'", v.expected(500), true)
	v.atLeast(d, "comptes rendus d'imagerie (lien externe)", "SELECT COUNT(*) FROM imaging_reports WHERE COALESCE(document_url,'') <> ''", 3, false)
}

func validatePagination(v *validator) {
	d := "Recherche / pagination"
	for _, t := range []struct {
		label, sql string
		snapshot   bool
	}{
		{"patients", "SELECT COUNT(*) FROM patients", false},
		{"rendez-vous du mois", "SELECT COUNT(*) FROM patient_queue_appointments WHERE scheduled_at > ?::timestamptz - interval '30 days'", true},
		{"consultations du mois", "SELECT COUNT(*) FROM consultations WHERE created_at > ?::timestamptz - interval '30 days'", true},
		{"factures du mois", "SELECT COUNT(*) FROM billing_invoices WHERE created_at > ?::timestamptz - interval '30 days'", true},
		{"demandes labo du mois", "SELECT COUNT(*) FROM laboratory_orders WHERE created_at > ?::timestamptz - interval '30 days'", true},
		{"tickets support", "SELECT COUNT(*) FROM ticketing_tickets", false},
		{"actes réalisés du mois", "SELECT COUNT(*) FROM performed_acts WHERE performed_at > ?::timestamptz - interval '30 days'", true},
	} {
		if t.snapshot {
			v.atLeast(d, t.label+" (>= 3 pages de 20)", t.sql, 60, true, v.cfg.SimulationNow())
		} else {
			v.atLeast(d, t.label+" (>= 3 pages de 20)", t.sql, 60, true)
		}
	}
}

func validateIntegrity(v *validator) {
	d := "Intégrité"
	v.zero(d, "lignes de facture orphelines", "SELECT COUNT(*) FROM billing_invoice_lines l LEFT JOIN billing_invoices i ON i.id = l.invoice_id WHERE i.id IS NULL", true)
	v.zero(d, "paiements orphelins", "SELECT COUNT(*) FROM billing_payments p LEFT JOIN billing_invoices i ON i.id = p.invoice_id WHERE i.id IS NULL", true)
	v.zero(d, "demandes labo orphelines", "SELECT COUNT(*) FROM laboratory_orders o LEFT JOIN consultations c ON c.id = o.consultation_id WHERE c.id IS NULL", true)
	v.zero(d, "actes sans patient", "SELECT COUNT(*) FROM performed_acts a LEFT JOIN patients p ON p.id = a.patient_id WHERE p.id IS NULL", true)
	v.zero(d, "tickets de file sans patient", "SELECT COUNT(*) FROM patient_queue_tickets t LEFT JOIN patients p ON p.id = t.patient_id WHERE p.id IS NULL", true)
	v.zero(d, "ticket terminé sans consultation", "SELECT COUNT(*) FROM patient_queue_tickets WHERE status = 'COMPLETED' AND consultation_id IS NULL", true)
	v.zero(d, "plus d'un parcours actif par patient", "SELECT COUNT(*) FROM (SELECT patient_id FROM patient_queue_tickets WHERE status = 'ACTIVE' GROUP BY 1 HAVING COUNT(*) > 1) x", true)
}
