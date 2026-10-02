package medical_records

// LOT28E-B2-B — AUTH-C timeline read projection.
// medical_records.read → occurrence visible.
// billing.read / insurance.authorization.read → domain detail.
// LOT28E-B3 — AUTH-B for performed_act_* under performed_acts.read.
// Projection is read-time only; stored rows are never mutated.

const (
	permBillingRead                = "billing.read"
	permInsuranceAuthorizationRead = "insurance.authorization.read"
	permPerformedActsRead          = "performed_acts.read"

	timelineDomainFinancial    = "financial"
	timelineDomainInsurance    = "insurance"
	timelineDomainPerformedAct = "performed_act"
	timelineDomainClinical     = "clinical"

	// Generic projected event types for unauthorized domain readers (no lifecycle status).
	TimelineEventBillingOccurrence   = "billing_event"
	TimelineEventInsuranceOccurrence = "insurance_event"

	timelineTitleBillingOccurrence     = "Événement de facturation"
	timelineTitleInsuranceOccurrence   = "Événement d'assurance"
	timelineTitlePerformedActPerformed = "Acte réalisé"
	timelineTitlePerformedActVoided    = "Acte annulé"

	timelineCategoryBilling      = "billing"
	timelineCategoryInsurance    = "insurance"
	timelineCategoryPerformedAct = "performed_act"
	timelineRefBillingInvoice    = "billing_invoice"
	timelineRefInsuranceAuth     = "insurance_authorization"
	timelineRefPerformedAct      = "PerformedAct"

	timelineEventPerformedActPerformed = "performed_act_performed"
	timelineEventPerformedActVoided    = "performed_act_voided"
)

// Runtime financial event types written by billing (exact inventory).
var financialTimelineEventTypes = map[string]struct{}{
	"invoice_issued":    {},
	"payment_received":  {},
	"invoice_paid":      {},
	"invoice_cancelled": {},
}

// Runtime insurance/PEC event types written by authorization module.
var insuranceTimelineEventTypes = map[string]struct{}{
	"insurance_authorization_created":            {},
	"insurance_authorization_submitted":          {},
	"insurance_authorization_approved":           {},
	"insurance_authorization_partially_approved": {},
	"insurance_authorization_rejected":           {},
	"insurance_authorization_cancelled":          {},
	"insurance_authorization_act_linked":         {},
}

var performedActTimelineEventTypes = map[string]struct{}{
	timelineEventPerformedActPerformed: {},
	timelineEventPerformedActVoided:    {},
}

func permissionGranted(permissions []string, required string) bool {
	for _, p := range permissions {
		if p == "*" || p == required {
			return true
		}
	}
	return false
}

func classifyTimelineEventDomain(event MedicalTimelineEvent) string {
	if _, ok := financialTimelineEventTypes[event.EventType]; ok {
		return timelineDomainFinancial
	}
	if event.Category == timelineCategoryBilling || event.ReferenceType == timelineRefBillingInvoice {
		return timelineDomainFinancial
	}
	if _, ok := insuranceTimelineEventTypes[event.EventType]; ok {
		return timelineDomainInsurance
	}
	if event.Category == timelineCategoryInsurance || event.ReferenceType == timelineRefInsuranceAuth {
		return timelineDomainInsurance
	}
	if _, ok := performedActTimelineEventTypes[event.EventType]; ok {
		return timelineDomainPerformedAct
	}
	if event.Category == timelineCategoryPerformedAct || event.ReferenceType == timelineRefPerformedAct {
		return timelineDomainPerformedAct
	}
	return timelineDomainClinical
}

// ProjectTimelineEventsForCaller returns authority-aware copies.
// Original slice elements are never mutated.
func ProjectTimelineEventsForCaller(events []MedicalTimelineEvent, permissions []string) []MedicalTimelineEvent {
	if len(events) == 0 {
		return []MedicalTimelineEvent{}
	}
	canBilling := permissionGranted(permissions, permBillingRead)
	canInsurance := permissionGranted(permissions, permInsuranceAuthorizationRead)
	canPerformedActs := permissionGranted(permissions, permPerformedActsRead)
	out := make([]MedicalTimelineEvent, len(events))
	for i, event := range events {
		out[i] = projectTimelineEvent(event, canBilling, canInsurance, canPerformedActs)
	}
	return out
}

func projectTimelineEvent(event MedicalTimelineEvent, canBilling, canInsurance, canPerformedActs bool) MedicalTimelineEvent {
	projected := event // value copy
	switch classifyTimelineEventDomain(event) {
	case timelineDomainFinancial:
		if !canBilling {
			redactFinancialTimelineEvent(&projected)
		}
	case timelineDomainInsurance:
		if !canInsurance {
			redactInsuranceTimelineEvent(&projected)
		}
	case timelineDomainPerformedAct:
		if !canPerformedActs {
			redactPerformedActTimelineEvent(&projected)
		}
	}
	return projected
}

func redactFinancialTimelineEvent(event *MedicalTimelineEvent) {
	event.EventType = TimelineEventBillingOccurrence
	event.Category = timelineCategoryBilling
	event.Title = timelineTitleBillingOccurrence
	event.Description = ""
	event.ReferenceType = ""
	event.ReferenceID = nil
	event.Severity = "info"
}

func redactInsuranceTimelineEvent(event *MedicalTimelineEvent) {
	event.EventType = TimelineEventInsuranceOccurrence
	event.Category = timelineCategoryInsurance
	event.Title = timelineTitleInsuranceOccurrence
	event.Description = ""
	event.ReferenceType = ""
	event.ReferenceID = nil
	event.Severity = "info"
}

// AUTH-B: occurrence visible under medical_records.read; PA detail requires performed_acts.read.
func redactPerformedActTimelineEvent(event *MedicalTimelineEvent) {
	switch event.EventType {
	case timelineEventPerformedActVoided:
		event.Title = timelineTitlePerformedActVoided
	default:
		event.Title = timelineTitlePerformedActPerformed
	}
	event.Category = timelineCategoryPerformedAct
	event.Description = ""
	event.ReferenceType = ""
	event.ReferenceID = nil
	event.Severity = "info"
	// EventType + EventDate retained for chronology / FE labels.
}
