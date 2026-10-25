package grc

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func uintPtr(value uint) *uint {
	return &value
}

func validEvidenceRequest() EvidenceRequest {
	return EvidenceRequest{
		Reference:               "EVD-001",
		Title:                   "Rapport de sauvegarde",
		ControlImplementationID: 1,
		Type:                    EvidenceTypeReport,
		Status:                  EvidenceStatusDraft,
	}
}

func validRemediationActionRequest() RemediationActionRequest {
	return RemediationActionRequest{
		Reference:               "ACT-001",
		Title:                   "Chiffrer les sauvegardes",
		ControlImplementationID: uintPtr(1),
		Priority:                RemediationActionPriorityHigh,
		Status:                  RemediationActionStatusOpen,
	}
}

func requireBadRequest(t *testing.T, err error) {
	t.Helper()
	expectAppStatus(t, err, http.StatusBadRequest)
}

func TestNormalizeEvidenceRequest(t *testing.T) {
	collected := time.Date(2026, 2, 3, 4, 5, 6, 7, time.FixedZone("UTC+2", 2*3600))
	r := EvidenceRequest{
		Reference:         "  evd-001 ",
		Title:             "  Rapport  ",
		Description:       " Détail ",
		Type:              " report ",
		Status:            " valid ",
		ExternalReference: "  DOC-SEC-2026-0042  ",
		CollectedAt:       &collected,
	}

	normalizeEvidenceRequest(&r)

	if r.Reference != "EVD-001" ||
		r.Title != "Rapport" ||
		r.Description != "Détail" ||
		r.Type != EvidenceTypeReport ||
		r.Status != EvidenceStatusValid ||
		r.ExternalReference != "DOC-SEC-2026-0042" {
		t.Fatalf("unexpected normalization: %+v", r)
	}
	if r.CollectedAt != &collected || !r.CollectedAt.Equal(collected) || r.CollectedAt.Location() != collected.Location() {
		t.Fatal("normalization must not modify timestamps")
	}
}

func TestValidateEvidenceRequestRequiredFields(t *testing.T) {
	if err := validateEvidenceRequest(validEvidenceRequest()); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	cases := map[string]func(*EvidenceRequest){
		"missing reference":      func(r *EvidenceRequest) { r.Reference = "" },
		"missing title":          func(r *EvidenceRequest) { r.Title = "" },
		"zero implementation id": func(r *EvidenceRequest) { r.ControlImplementationID = 0 },
		"zero owner id":          func(r *EvidenceRequest) { r.OwnerProfileID = uintPtr(0) },
		"zero service id":        func(r *EvidenceRequest) { r.OwningServiceID = uintPtr(0) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := validEvidenceRequest()
			mutate(&r)
			requireBadRequest(t, validateEvidenceRequest(r))
		})
	}
}

func TestValidateEvidenceRequestEnums(t *testing.T) {
	for _, value := range []string{
		EvidenceTypeDocument,
		EvidenceTypeConfiguration,
		EvidenceTypeLog,
		EvidenceTypeScreenshot,
		EvidenceTypeReport,
		EvidenceTypeTestResult,
		EvidenceTypeCertificate,
		EvidenceTypeOther,
	} {
		r := validEvidenceRequest()
		r.Type = value
		if err := validateEvidenceRequest(r); err != nil {
			t.Fatalf("type %q rejected: %v", value, err)
		}
	}
	if len(validEvidenceTypes) != 8 {
		t.Fatalf("evidence types=%d want 8", len(validEvidenceTypes))
	}

	for _, value := range []string{"", "PASSWORD", "document"} {
		r := validEvidenceRequest()
		r.Type = value
		requireBadRequest(t, validateEvidenceRequest(r))
	}

	for _, value := range []string{EvidenceStatusDraft, EvidenceStatusValid, EvidenceStatusRevoked} {
		r := validEvidenceRequest()
		r.Status = value
		if err := validateEvidenceRequest(r); err != nil {
			t.Fatalf("status %q rejected: %v", value, err)
		}
	}
	if len(validEvidenceStatuses) != 3 {
		t.Fatalf("evidence statuses=%d want 3", len(validEvidenceStatuses))
	}

	for _, value := range []string{"", "EXPIRED", "ARCHIVED", "valid"} {
		r := validEvidenceRequest()
		r.Status = value
		requireBadRequest(t, validateEvidenceRequest(r))
	}
}

func TestValidateEvidenceRequestValidityRange(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)

	for name, dates := range map[string][2]*time.Time{
		"none":  {nil, nil},
		"start": {&start, nil},
		"end":   {nil, &end},
		"both":  {&start, &end},
		"equal": {&start, &start},
	} {
		r := validEvidenceRequest()
		r.ValidFrom, r.ExpiresAt = dates[0], dates[1]
		if err := validateEvidenceRequest(r); err != nil {
			t.Fatalf("%s range rejected: %v", name, err)
		}
	}

	r := validEvidenceRequest()
	r.ValidFrom, r.ExpiresAt = &end, &start
	requireBadRequest(t, validateEvidenceRequest(r))
}

func TestValidateEvidenceExternalReference(t *testing.T) {
	accepted := []string{
		"",
		"DOC-SEC-2026-0042",
		"https://ged.example.local/documents/42",
		"ged:DOC-42",
		strings.Repeat("a", 500),
		strings.Repeat("é", 500),
	}
	for _, value := range accepted {
		if err := validateEvidenceExternalReference(value); err != nil {
			t.Fatalf("reference of length %d rejected: %v", len(value), err)
		}
	}

	rejected := map[string]string{
		"userinfo":        "https://user:secret@ged.example.local/documents/42",
		"username only":   "https://user@ged.example.local/documents/42",
		"query":           "https://ged.example.local/documents/42?token=abc",
		"fragment":        "https://ged.example.local/documents/42#page=2",
		"line feed":       "DOC-42\nsecond line",
		"carriage return": "DOC-42\rsecond line",
		"too long":        strings.Repeat("a", 501),
		"malformed uri":   "https://ged.example.local/%zz",
	}
	for name, value := range rejected {
		t.Run(name, func(t *testing.T) {
			err := validateEvidenceExternalReference(value)
			requireBadRequest(t, err)
			if strings.Contains(err.Error(), value) || strings.Contains(err.Error(), "secret") {
				t.Fatal("error must not echo the external reference")
			}
		})
	}

	// Validation runs on the request after normalization.
	r := validEvidenceRequest()
	r.ExternalReference = "https://ged.example.local/doc?sig=abc"
	requireBadRequest(t, validateEvidenceRequest(r))
}

func TestEvidenceExpired(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)

	cases := []struct {
		name      string
		status    string
		expiresAt *time.Time
		want      bool
	}{
		{"valid past", EvidenceStatusValid, &past, true},
		{"valid future", EvidenceStatusValid, &future, false},
		{"valid now", EvidenceStatusValid, &now, false},
		{"draft past", EvidenceStatusDraft, &past, false},
		{"revoked past", EvidenceStatusRevoked, &past, false},
		{"valid nil", EvidenceStatusValid, nil, false},
	}
	for _, tc := range cases {
		if got := EvidenceExpired(tc.status, tc.expiresAt, now); got != tc.want {
			t.Errorf("%s: EvidenceExpired=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestNormalizeRemediationActionRequest(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	r := RemediationActionRequest{
		Reference:   " act-001 ",
		Title:       "  Chiffrer  ",
		Description: " Détail ",
		Priority:    " critical ",
		Status:      " in_progress ",
		DueAt:       &due,
	}

	normalizeRemediationActionRequest(&r)

	if r.Reference != "ACT-001" ||
		r.Title != "Chiffrer" ||
		r.Description != "Détail" ||
		r.Priority != RemediationActionPriorityCritical ||
		r.Status != RemediationActionStatusInProgress {
		t.Fatalf("unexpected normalization: %+v", r)
	}
	if r.DueAt != &due || !r.DueAt.Equal(due) {
		t.Fatal("normalization must not modify timestamps")
	}
}

func TestValidateRemediationActionRequestRequiredFieldsAndEnums(t *testing.T) {
	if err := validateRemediationActionRequest(validRemediationActionRequest()); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	for name, mutate := range map[string]func(*RemediationActionRequest){
		"missing reference": func(r *RemediationActionRequest) { r.Reference = "" },
		"missing title":     func(r *RemediationActionRequest) { r.Title = "" },
		"invalid priority":  func(r *RemediationActionRequest) { r.Priority = "URGENT" },
		"invalid status":    func(r *RemediationActionRequest) { r.Status = "CLOSED" },
		"lowercase status":  func(r *RemediationActionRequest) { r.Status = "open" },
	} {
		t.Run(name, func(t *testing.T) {
			r := validRemediationActionRequest()
			mutate(&r)
			requireBadRequest(t, validateRemediationActionRequest(r))
		})
	}

	for _, value := range []string{
		RemediationActionPriorityLow,
		RemediationActionPriorityMedium,
		RemediationActionPriorityHigh,
		RemediationActionPriorityCritical,
	} {
		r := validRemediationActionRequest()
		r.Priority = value
		if err := validateRemediationActionRequest(r); err != nil {
			t.Fatalf("priority %q rejected: %v", value, err)
		}
	}
	if len(validRemediationActionPriorities) != 4 {
		t.Fatalf("priorities=%d want 4", len(validRemediationActionPriorities))
	}

	for _, value := range []string{
		RemediationActionStatusOpen,
		RemediationActionStatusInProgress,
		RemediationActionStatusBlocked,
		RemediationActionStatusDone,
		RemediationActionStatusCancelled,
	} {
		r := validRemediationActionRequest()
		r.Status = value
		if err := validateRemediationActionRequest(r); err != nil {
			t.Fatalf("status %q rejected: %v", value, err)
		}
	}
	if len(validRemediationActionStatuses) != 5 {
		t.Fatalf("statuses=%d want 5", len(validRemediationActionStatuses))
	}
}

func TestValidateRemediationActionRequestTargets(t *testing.T) {
	controlOnly := validRemediationActionRequest()
	if err := validateRemediationActionRequest(controlOnly); err != nil {
		t.Fatalf("control implementation target rejected: %v", err)
	}

	riskOnly := validRemediationActionRequest()
	riskOnly.ControlImplementationID = nil
	riskOnly.RiskID = uintPtr(2)
	if err := validateRemediationActionRequest(riskOnly); err != nil {
		t.Fatalf("risk target rejected: %v", err)
	}

	both := validRemediationActionRequest()
	both.RiskID = uintPtr(2)
	if err := validateRemediationActionRequest(both); err != nil {
		t.Fatalf("both targets rejected: %v", err)
	}

	none := validRemediationActionRequest()
	none.ControlImplementationID = nil
	requireBadRequest(t, validateRemediationActionRequest(none))

	for name, mutate := range map[string]func(*RemediationActionRequest){
		"zero control implementation": func(r *RemediationActionRequest) { r.ControlImplementationID = uintPtr(0) },
		"zero risk":                   func(r *RemediationActionRequest) { r.RiskID = uintPtr(0) },
		"zero assignee":               func(r *RemediationActionRequest) { r.AssigneeProfileID = uintPtr(0) },
		"zero service":                func(r *RemediationActionRequest) { r.OwningServiceID = uintPtr(0) },
	} {
		t.Run(name, func(t *testing.T) {
			r := validRemediationActionRequest()
			mutate(&r)
			requireBadRequest(t, validateRemediationActionRequest(r))
		})
	}
}

func TestValidateRemediationActionRequestCompletion(t *testing.T) {
	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	doneWithoutDate := validRemediationActionRequest()
	doneWithoutDate.Status = RemediationActionStatusDone
	if err := validateRemediationActionRequest(doneWithoutDate); err != nil {
		t.Fatalf("DONE without CompletedAt must pass pure validation: %v", err)
	}

	doneWithDate := doneWithoutDate
	doneWithDate.CompletedAt = &completed
	if err := validateRemediationActionRequest(doneWithDate); err != nil {
		t.Fatalf("DONE with CompletedAt rejected: %v", err)
	}

	for _, status := range []string{
		RemediationActionStatusOpen,
		RemediationActionStatusInProgress,
		RemediationActionStatusBlocked,
		RemediationActionStatusCancelled,
	} {
		r := validRemediationActionRequest()
		r.Status = status
		if err := validateRemediationActionRequest(r); err != nil {
			t.Fatalf("%s without CompletedAt rejected: %v", status, err)
		}

		r.CompletedAt = &completed
		requireBadRequest(t, validateRemediationActionRequest(r))
	}
}

func TestRemediationActionOverdue(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)

	cases := []struct {
		name   string
		status string
		dueAt  *time.Time
		want   bool
	}{
		{"open past", RemediationActionStatusOpen, &past, true},
		{"in progress past", RemediationActionStatusInProgress, &past, true},
		{"blocked past", RemediationActionStatusBlocked, &past, true},
		{"done past", RemediationActionStatusDone, &past, false},
		{"cancelled past", RemediationActionStatusCancelled, &past, false},
		{"open future", RemediationActionStatusOpen, &future, false},
		{"open now", RemediationActionStatusOpen, &now, false},
		{"open nil", RemediationActionStatusOpen, nil, false},
	}
	for _, tc := range cases {
		if got := RemediationActionOverdue(tc.status, tc.dueAt, now); got != tc.want {
			t.Errorf("%s: RemediationActionOverdue=%v want %v", tc.name, got, tc.want)
		}
	}
}

func TestEvidenceAndActionRequestsExposeNoServerFields(t *testing.T) {
	forbidden := map[string]bool{
		"id":        true,
		"createdby": true,
		"updatedby": true,
		"createdat": true,
		"updatedat": true,
		"expired":   true,
		"overdue":   true,
	}

	for _, dto := range []any{EvidenceRequest{}, RemediationActionRequest{}} {
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
