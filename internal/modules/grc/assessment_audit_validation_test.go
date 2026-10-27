package grc

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Pure tests: no database is opened in this file.

var validationNow = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)

func validAssessmentRequest() AssessmentRequest {
	return AssessmentRequest{
		Reference:               "ASM-001",
		Title:                   "Évaluation sauvegardes",
		ControlImplementationID: 1,
		Type:                    AssessmentTypeOperatingEffectiveness,
		Status:                  AssessmentStatusDraft,
	}
}

func validCompletedAssessmentRequest() AssessmentRequest {
	r := validAssessmentRequest()
	r.Status = AssessmentStatusCompleted
	r.Result = stringPtr(AssessmentResultEffective)
	r.AssessorProfileID = uintPtr(2)
	r.AssessedAt = timePtr(validationNow)
	return r
}

func validAuditEngagementRequest() AuditEngagementRequest {
	return AuditEngagementRequest{
		Reference: "AUD-001",
		Title:     "Audit interne sécurité",
		Type:      AuditEngagementTypeInternal,
		Status:    AuditEngagementStatusPlanned,
	}
}

func validFindingRequest() FindingRequest {
	return FindingRequest{
		Reference:         "FND-001",
		Title:             "Sauvegardes non testées",
		AuditEngagementID: uintPtr(1),
		Category:          FindingCategoryControlDeficiency,
		Severity:          FindingSeverityHigh,
		Status:            FindingStatusOpen,
	}
}

func TestNormalizeAssessmentAuditFindingRequests(t *testing.T) {
	assessed := time.Date(2026, 1, 2, 3, 4, 5, 6, time.FixedZone("UTC+2", 2*3600))
	a := AssessmentRequest{
		Reference: " asm-001 ", Title: "  Titre  ", Type: " design ", Status: " draft ",
		Result: stringPtr(" not_applicable "), Summary: "  Hors périmètre  ", AssessedAt: &assessed,
	}
	normalizeAssessmentRequest(&a)
	if a.Reference != "ASM-001" || a.Title != "Titre" || a.Type != AssessmentTypeDesign ||
		a.Status != AssessmentStatusDraft || a.Result == nil || *a.Result != AssessmentResultNotApplicable ||
		a.Summary != "Hors périmètre" {
		t.Fatalf("unexpected assessment normalization: %+v", a)
	}
	if a.AssessedAt != &assessed || a.AssessedAt.Location() != assessed.Location() {
		t.Fatal("normalization must not modify timestamps")
	}

	for _, blank := range []string{"", "   "} {
		r := AssessmentRequest{Result: stringPtr(blank)}
		normalizeAssessmentRequest(&r)
		if r.Result != nil {
			t.Fatalf("blank result %q must normalize to nil", blank)
		}
	}

	audit := AuditEngagementRequest{
		Reference: " aud-001 ", Title: " Audit ", Description: " Desc ", Scope: " Périmètre ",
		Type: " vendor ", Status: " in_progress ",
	}
	normalizeAuditEngagementRequest(&audit)
	if audit.Reference != "AUD-001" || audit.Title != "Audit" || audit.Description != "Desc" ||
		audit.Scope != "Périmètre" || audit.Type != AuditEngagementTypeVendor ||
		audit.Status != AuditEngagementStatusInProgress {
		t.Fatalf("unexpected audit normalization: %+v", audit)
	}

	finding := FindingRequest{
		Reference: " fnd-001 ", Title: " Constat ", Description: " Desc ", Category: " observation ",
		Severity: " low ", Status: " accepted ", ClosureNote: "  Accepté  ",
	}
	normalizeFindingRequest(&finding)
	if finding.Reference != "FND-001" || finding.Title != "Constat" || finding.Description != "Desc" ||
		finding.Category != FindingCategoryObservation || finding.Severity != FindingSeverityLow ||
		finding.Status != FindingStatusAccepted || finding.ClosureNote != "Accepté" {
		t.Fatalf("unexpected finding normalization: %+v", finding)
	}
}

func TestValidateAssessmentRequest(t *testing.T) {
	for name, r := range map[string]AssessmentRequest{
		"draft":     validAssessmentRequest(),
		"completed": validCompletedAssessmentRequest(),
	} {
		if err := validateAssessmentRequest(r); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}

	for _, value := range []string{AssessmentTypeDesign, AssessmentTypeOperatingEffectiveness, AssessmentTypeCompliance} {
		r := validAssessmentRequest()
		r.Type = value
		if err := validateAssessmentRequest(r); err != nil {
			t.Fatalf("type %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{AssessmentStatusDraft, AssessmentStatusCancelled} {
		r := validAssessmentRequest()
		r.Status = value
		if err := validateAssessmentRequest(r); err != nil {
			t.Fatalf("status %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		AssessmentResultEffective, AssessmentResultPartiallyEffective,
		AssessmentResultIneffective, AssessmentResultNotApplicable,
	} {
		r := validAssessmentRequest()
		r.Result = stringPtr(value)
		r.Summary = "Justification"
		if err := validateAssessmentRequest(r); err != nil {
			t.Fatalf("result %s rejected: %v", value, err)
		}
	}

	nextEqual := validCompletedAssessmentRequest()
	nextEqual.NextAssessmentAt = timePtr(validationNow)
	if err := validateAssessmentRequest(nextEqual); err != nil {
		t.Fatalf("next assessment equal to assessed_at rejected: %v", err)
	}

	cases := map[string]func(*AssessmentRequest){
		"missing reference":      func(r *AssessmentRequest) { r.Reference = "" },
		"blank title":            func(r *AssessmentRequest) { r.Title = "   " },
		"zero implementation":    func(r *AssessmentRequest) { r.ControlImplementationID = 0 },
		"type IMPLEMENTATION":    func(r *AssessmentRequest) { r.Type = "IMPLEMENTATION" },
		"type PERIODIC_REVIEW":   func(r *AssessmentRequest) { r.Type = "PERIODIC_REVIEW" },
		"status IN_REVIEW":       func(r *AssessmentRequest) { r.Status = "IN_REVIEW" },
		"result NOT_ASSESSED":    func(r *AssessmentRequest) { r.Result = stringPtr("NOT_ASSESSED") },
		"zero assessor":          func(r *AssessmentRequest) { r.AssessorProfileID = uintPtr(0) },
		"completed no result":    func(r *AssessmentRequest) { *r = validCompletedAssessmentRequest(); r.Result = nil },
		"completed no assessor":  func(r *AssessmentRequest) { *r = validCompletedAssessmentRequest(); r.AssessorProfileID = nil },
		"completed no assessed":  func(r *AssessmentRequest) { *r = validCompletedAssessmentRequest(); r.AssessedAt = nil },
		"not applicable no text": func(r *AssessmentRequest) { r.Result = stringPtr(AssessmentResultNotApplicable) },
		"not applicable spaces": func(r *AssessmentRequest) {
			r.Result = stringPtr(AssessmentResultNotApplicable)
			r.Summary = "   "
		},
		"next before assessed": func(r *AssessmentRequest) {
			*r = validCompletedAssessmentRequest()
			r.NextAssessmentAt = timePtr(validationNow.Add(-time.Second))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validAssessmentRequest()
			mutate(&r)
			normalizeAssessmentRequest(&r)
			requireBadRequest(t, validateAssessmentRequest(r))
		})
	}

	requireBadRequest(t, validateAssessmentEvidenceRequest(AssessmentEvidenceRequest{}))
	if err := validateAssessmentEvidenceRequest(AssessmentEvidenceRequest{EvidenceID: 3}); err != nil {
		t.Fatalf("valid evidence link rejected: %v", err)
	}
}

func TestValidateAuditEngagementRequest(t *testing.T) {
	if err := validateAuditEngagementRequest(validAuditEngagementRequest()); err != nil {
		t.Fatalf("valid audit rejected: %v", err)
	}
	for _, value := range []string{AuditEngagementTypeInternal, AuditEngagementTypeExternal, AuditEngagementTypeVendor} {
		r := validAuditEngagementRequest()
		r.Type = value
		if err := validateAuditEngagementRequest(r); err != nil {
			t.Fatalf("type %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		AuditEngagementStatusPlanned, AuditEngagementStatusInProgress,
		AuditEngagementStatusCompleted, AuditEngagementStatusCancelled,
	} {
		r := validAuditEngagementRequest()
		r.Status = value
		if err := validateAuditEngagementRequest(r); err != nil {
			t.Fatalf("status %s rejected: %v", value, err)
		}
	}

	start := validationNow
	for name, end := range map[string]time.Time{"equal": start, "later": start.Add(time.Hour)} {
		r := validAuditEngagementRequest()
		r.PlannedStartAt, r.PlannedEndAt = timePtr(start), timePtr(end)
		if err := validateAuditEngagementRequest(r); err != nil {
			t.Fatalf("planned range %s rejected: %v", name, err)
		}
	}

	cases := map[string]func(*AuditEngagementRequest){
		"blank reference":   func(r *AuditEngagementRequest) { r.Reference = "  " },
		"missing title":     func(r *AuditEngagementRequest) { r.Title = "" },
		"type COMPLIANCE":   func(r *AuditEngagementRequest) { r.Type = "COMPLIANCE" },
		"type SECURITY":     func(r *AuditEngagementRequest) { r.Type = "SECURITY" },
		"status DRAFT":      func(r *AuditEngagementRequest) { r.Status = "DRAFT" },
		"zero lead auditor": func(r *AuditEngagementRequest) { r.LeadAuditorProfileID = uintPtr(0) },
		"zero service":      func(r *AuditEngagementRequest) { r.OwningServiceID = uintPtr(0) },
		"planned end before": func(r *AuditEngagementRequest) {
			r.PlannedStartAt, r.PlannedEndAt = timePtr(start), timePtr(start.Add(-time.Second))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validAuditEngagementRequest()
			mutate(&r)
			normalizeAuditEngagementRequest(&r)
			requireBadRequest(t, validateAuditEngagementRequest(r))
		})
	}
}

func TestValidateFindingRequest(t *testing.T) {
	fromAssessment := validFindingRequest()
	fromAssessment.AuditEngagementID = nil
	fromAssessment.AssessmentID = uintPtr(4)
	for name, r := range map[string]FindingRequest{"from audit": validFindingRequest(), "from assessment": fromAssessment} {
		if err := validateFindingRequest(r); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}

	for _, value := range []string{
		FindingCategoryNonConformity, FindingCategoryControlDeficiency,
		FindingCategoryObservation, FindingCategoryImprovement,
	} {
		r := validFindingRequest()
		r.Category = value
		if err := validateFindingRequest(r); err != nil {
			t.Fatalf("category %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{FindingSeverityLow, FindingSeverityMedium, FindingSeverityHigh, FindingSeverityCritical} {
		r := validFindingRequest()
		r.Severity = value
		if err := validateFindingRequest(r); err != nil {
			t.Fatalf("severity %s rejected: %v", value, err)
		}
	}
	for _, value := range []string{
		FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved, FindingStatusClosed,
	} {
		r := validFindingRequest()
		r.Status = value
		if err := validateFindingRequest(r); err != nil {
			t.Fatalf("status %s rejected: %v", value, err)
		}
	}
	accepted := validFindingRequest()
	accepted.Status = FindingStatusAccepted
	accepted.ClosureNote = "Risque accepté par la direction"
	if err := validateFindingRequest(accepted); err != nil {
		t.Fatalf("justified acceptance rejected: %v", err)
	}

	cases := map[string]func(*FindingRequest){
		"missing reference":     func(r *FindingRequest) { r.Reference = "" },
		"blank title":           func(r *FindingRequest) { r.Title = " \t " },
		"no source":             func(r *FindingRequest) { r.AuditEngagementID = nil },
		"two sources":           func(r *FindingRequest) { r.AssessmentID = uintPtr(4) },
		"category RISK":         func(r *FindingRequest) { r.Category = "RISK" },
		"severity INFO":         func(r *FindingRequest) { r.Severity = "INFO" },
		"status REJECTED":       func(r *FindingRequest) { r.Status = "REJECTED" },
		"zero audit":            func(r *FindingRequest) { r.AuditEngagementID = uintPtr(0) },
		"zero implementation":   func(r *FindingRequest) { r.ControlImplementationID = uintPtr(0) },
		"zero risk":             func(r *FindingRequest) { r.RiskID = uintPtr(0) },
		"zero owner":            func(r *FindingRequest) { r.OwnerProfileID = uintPtr(0) },
		"zero service":          func(r *FindingRequest) { r.OwningServiceID = uintPtr(0) },
		"accepted without note": func(r *FindingRequest) { r.Status = FindingStatusAccepted },
		"accepted blank note": func(r *FindingRequest) {
			r.Status = FindingStatusAccepted
			r.ClosureNote = "   "
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validFindingRequest()
			mutate(&r)
			normalizeFindingRequest(&r)
			requireBadRequest(t, validateFindingRequest(r))
		})
	}
}

func TestInitialStatuses(t *testing.T) {
	cases := []struct {
		name     string
		validate func(string) error
		allowed  []string
		statuses []string
	}{
		{"assessment", validateAssessmentInitialStatus, []string{AssessmentStatusDraft},
			[]string{AssessmentStatusDraft, AssessmentStatusCompleted, AssessmentStatusCancelled}},
		{"audit", validateAuditEngagementInitialStatus,
			[]string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress},
			[]string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress,
				AuditEngagementStatusCompleted, AuditEngagementStatusCancelled}},
		{"finding", validateFindingInitialStatus, []string{FindingStatusOpen},
			[]string{FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved,
				FindingStatusClosed, FindingStatusAccepted}},
	}
	for _, tc := range cases {
		allowed := map[string]bool{}
		for _, s := range tc.allowed {
			allowed[s] = true
		}
		for _, status := range tc.statuses {
			err := tc.validate(status)
			if allowed[status] && err != nil {
				t.Errorf("%s: initial status %s rejected: %v", tc.name, status, err)
			}
			if !allowed[status] {
				requireBadRequest(t, err)
			}
		}
	}
}

// Each matrix lists, for every current status, the target statuses that are
// allowed; every other pair must be rejected with 409.
func TestStatusTransitionMatrices(t *testing.T) {
	cases := []struct {
		name     string
		validate func(from, to string) error
		statuses []string
		allowed  map[string][]string
	}{
		{
			name:     "assessment",
			validate: validateAssessmentTransition,
			statuses: []string{AssessmentStatusDraft, AssessmentStatusCompleted, AssessmentStatusCancelled},
			allowed: map[string][]string{
				AssessmentStatusDraft: {AssessmentStatusDraft, AssessmentStatusCompleted, AssessmentStatusCancelled},
			},
		},
		{
			name:     "audit",
			validate: validateAuditEngagementTransition,
			statuses: []string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress,
				AuditEngagementStatusCompleted, AuditEngagementStatusCancelled},
			allowed: map[string][]string{
				AuditEngagementStatusPlanned: {AuditEngagementStatusPlanned, AuditEngagementStatusInProgress,
					AuditEngagementStatusCancelled},
				AuditEngagementStatusInProgress: {AuditEngagementStatusInProgress, AuditEngagementStatusCompleted,
					AuditEngagementStatusCancelled},
			},
		},
		{
			name:     "finding",
			validate: validateFindingTransition,
			statuses: []string{FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved,
				FindingStatusClosed, FindingStatusAccepted},
			allowed: map[string][]string{
				FindingStatusOpen: {FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved,
					FindingStatusAccepted},
				FindingStatusInRemediation: {FindingStatusInRemediation, FindingStatusResolved, FindingStatusAccepted},
				FindingStatusResolved:      {FindingStatusResolved, FindingStatusOpen, FindingStatusClosed, FindingStatusAccepted},
			},
		},
	}

	for _, tc := range cases {
		for _, from := range tc.statuses {
			allowed := map[string]bool{}
			for _, to := range tc.allowed[from] {
				allowed[to] = true
			}
			for _, to := range append(tc.statuses, "UNKNOWN") {
				err := tc.validate(from, to)
				if allowed[to] {
					if err != nil {
						t.Errorf("%s: %s -> %s rejected: %v", tc.name, from, to, err)
					}
					continue
				}
				if err == nil {
					t.Errorf("%s: %s -> %s must be rejected", tc.name, from, to)
					continue
				}
				expectAppStatus(t, err, http.StatusConflict)
			}
		}
		if err := tc.validate("UNKNOWN", tc.statuses[0]); err == nil {
			t.Errorf("%s: transition from an unknown status must be rejected", tc.name)
		}
	}

	// Terminal statuses report immutability, not a mere forbidden step.
	for _, err := range []error{
		validateAssessmentTransition(AssessmentStatusCompleted, AssessmentStatusDraft),
		validateAuditEngagementTransition(AuditEngagementStatusCancelled, AuditEngagementStatusPlanned),
		validateFindingTransition(FindingStatusClosed, FindingStatusOpen),
	} {
		if err == nil || !strings.Contains(err.Error(), "ne peut plus être modifié") {
			t.Errorf("terminal status error = %v, want immutability message", err)
		}
	}
}

func TestAssessmentReassessmentDue(t *testing.T) {
	past := validationNow.Add(-time.Second)
	future := validationNow.Add(time.Second)

	cases := []struct {
		name   string
		status string
		next   *time.Time
		want   bool
	}{
		{"completed past", AssessmentStatusCompleted, &past, true},
		{"completed now", AssessmentStatusCompleted, &validationNow, false},
		{"completed future", AssessmentStatusCompleted, &future, false},
		{"completed nil", AssessmentStatusCompleted, nil, false},
		{"draft past", AssessmentStatusDraft, &past, false},
		{"cancelled past", AssessmentStatusCancelled, &past, false},
	}
	for _, tc := range cases {
		if got := AssessmentReassessmentDue(tc.status, tc.next, validationNow); got != tc.want {
			t.Errorf("%s: AssessmentReassessmentDue=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestFindingOverdue(t *testing.T) {
	past := validationNow.Add(-time.Second)
	future := validationNow.Add(time.Second)

	cases := []struct {
		name   string
		status string
		due    *time.Time
		want   bool
	}{
		{"open past", FindingStatusOpen, &past, true},
		{"in remediation past", FindingStatusInRemediation, &past, true},
		{"resolved past", FindingStatusResolved, &past, false},
		{"closed past", FindingStatusClosed, &past, false},
		{"accepted past", FindingStatusAccepted, &past, false},
		{"open now", FindingStatusOpen, &validationNow, false},
		{"open future", FindingStatusOpen, &future, false},
		{"open nil", FindingStatusOpen, nil, false},
	}
	for _, tc := range cases {
		if got := FindingOverdue(tc.status, tc.due, validationNow); got != tc.want {
			t.Errorf("%s: FindingOverdue=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestAssessmentAuditFindingFilters(t *testing.T) {
	after, before := validationNow, validationNow.Add(time.Hour)

	assessment := AssessmentFilter{Type: " compliance ", Status: " completed ", Result: " ineffective ",
		AssessedAfter: &after, AssessedBefore: &before}
	normalizeAssessmentFilter(&assessment)
	if err := validateAssessmentFilter(assessment); err != nil {
		t.Fatalf("valid assessment filter rejected: %v", err)
	}
	audit := AuditEngagementFilter{Type: " external ", Status: " planned ", PlannedStartAfter: &after, PlannedStartBefore: &after}
	normalizeAuditEngagementFilter(&audit)
	if err := validateAuditEngagementFilter(audit); err != nil {
		t.Fatalf("valid audit filter rejected: %v", err)
	}
	finding := FindingFilter{Category: " improvement ", Severity: " critical ", Status: " resolved "}
	normalizeFindingFilter(&finding)
	if err := validateFindingFilter(finding); err != nil {
		t.Fatalf("valid finding filter rejected: %v", err)
	}

	for name, err := range map[string]error{
		"assessment type":     validateAssessmentFilter(AssessmentFilter{Type: "PERIODIC_REVIEW"}),
		"assessment status":   validateAssessmentFilter(AssessmentFilter{Status: "IN_REVIEW"}),
		"assessment result":   validateAssessmentFilter(AssessmentFilter{Result: "NOT_ASSESSED"}),
		"assessment id":       validateAssessmentFilter(AssessmentFilter{AssessorProfileID: uintPtr(0)}),
		"assessment period":   validateAssessmentFilter(AssessmentFilter{AssessedAfter: &before, AssessedBefore: &after}),
		"audit type":          validateAuditEngagementFilter(AuditEngagementFilter{Type: "SECURITY"}),
		"audit status":        validateAuditEngagementFilter(AuditEngagementFilter{Status: "DRAFT"}),
		"audit id":            validateAuditEngagementFilter(AuditEngagementFilter{OwningServiceID: uintPtr(0)}),
		"audit period":        validateAuditEngagementFilter(AuditEngagementFilter{PlannedStartAfter: &before, PlannedStartBefore: &after}),
		"finding category":    validateFindingFilter(FindingFilter{Category: "RISK"}),
		"finding severity":    validateFindingFilter(FindingFilter{Severity: "INFO"}),
		"finding status":      validateFindingFilter(FindingFilter{Status: "REJECTED"}),
		"finding id":          validateFindingFilter(FindingFilter{RiskID: uintPtr(0)}),
		"finding assessment":  validateFindingFilter(FindingFilter{AssessmentID: uintPtr(0)}),
		"finding audit":       validateFindingFilter(FindingFilter{AuditEngagementID: uintPtr(0)}),
		"finding owner":       validateFindingFilter(FindingFilter{OwnerProfileID: uintPtr(0)}),
		"finding service":     validateFindingFilter(FindingFilter{OwningServiceID: uintPtr(0)}),
		"finding implementer": validateFindingFilter(FindingFilter{ControlImplementationID: uintPtr(0)}),
	} {
		t.Run(name, func(t *testing.T) { requireBadRequest(t, err) })
	}
}

func TestAssessmentAuditFindingRequestsExposeNoServerFields(t *testing.T) {
	forbidden := map[string]bool{
		"id": true, "createdby": true, "updatedby": true, "createdat": true, "updatedat": true,
		"completedat": true, "resolvedat": true, "closedat": true,
		"reassessmentdue": true, "hasinvalidevidence": true, "overdue": true,
	}

	for _, dto := range []any{AssessmentRequest{}, AssessmentEvidenceRequest{}, AuditEngagementRequest{}, FindingRequest{}} {
		typ := reflect.TypeOf(dto)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
			if forbidden[strings.ToLower(field.Name)] || forbidden[strings.ToLower(jsonName)] {
				t.Errorf("%s must not expose server field %s (json %q)", typ.Name(), field.Name, jsonName)
			}
		}
	}
}
