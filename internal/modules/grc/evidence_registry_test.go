package grc

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// GRC-07: the single evidence validity definition (Go, SQL and dashboard),
// validity filter and window, combined search, lengths and the
// evidence → assessments read.

func TestEvidenceValidityBoundaries(t *testing.T) {
	now := evidenceTestNow
	limit := now.AddDate(0, 0, 30)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }

	cases := []struct {
		name      string
		status    string
		expiresAt *time.Time
		want      string
	}{
		{"draft", EvidenceStatusDraft, nil, EvidenceValidityInvalid},
		{"revoked, even if expired", EvidenceStatusRevoked, at(-time.Hour), EvidenceValidityInvalid},
		{"draft past expiry is not expired", EvidenceStatusDraft, at(-time.Hour), EvidenceValidityInvalid},
		{"missing expiry", EvidenceStatusValid, nil, EvidenceValidityValid},
		{"after expiry", EvidenceStatusValid, at(-time.Second), EvidenceValidityExpired},
		{"expiry exactly at now", EvidenceStatusValid, at(0), EvidenceValidityExpiringSoon},
		{"inside the window", EvidenceStatusValid, at(10 * 24 * time.Hour), EvidenceValidityExpiringSoon},
		{"window end", EvidenceStatusValid, &limit, EvidenceValidityExpiringSoon},
		{"after the window", EvidenceStatusValid, func() *time.Time { v := limit.Add(time.Second); return &v }(), EvidenceValidityValid},
	}
	for _, tc := range cases {
		if got := EvidenceValidity(tc.status, tc.expiresAt, now, limit); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	if _, _, err := evidenceValidityCondition("STALE", now, limit); err == nil {
		t.Fatal("unknown validity accepted")
	}
}

// seedEvidenceValidity creates one evidence per boundary of the default window.
func seedEvidenceValidity(t *testing.T, f evidenceServiceFixture) map[string]Evidence {
	t.Helper()
	day := 24 * time.Hour
	now := evidenceTestNow
	out := map[string]Evidence{}
	add := func(reference, status string, expires *time.Time) {
		r := f.request(reference)
		r.Title = "Preuve " + reference
		r.Status = status
		r.ExpiresAt = expires
		out[reference] = f.create(t, r)
	}
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	add("EVD-NOEXP", EvidenceStatusValid, nil)
	add("EVD-PAST", EvidenceStatusValid, at(-time.Second))
	add("EVD-NOW", EvidenceStatusValid, at(0))
	add("EVD-END", EvidenceStatusValid, at(30*day))
	add("EVD-LATER", EvidenceStatusValid, at(30*day+time.Second))
	add("EVD-DRAFT", EvidenceStatusDraft, at(-day))
	add("EVD-REVOKED", EvidenceStatusRevoked, nil)
	return out
}

func TestEvidenceValidityFilterMatchesDefinitionAndDashboard(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	if err := f.db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	seedEvidenceValidity(t, f)

	all, err := f.svc.ListEvidence(EvidenceFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	limit := evidenceTestNow.AddDate(0, 0, 30)
	byState := map[string][]string{}
	for _, e := range all.Items {
		// The computed field and the Go definition agree.
		if want := EvidenceValidity(e.Status, e.ExpiresAt, evidenceTestNow, limit); e.Validity != want {
			t.Fatalf("%s validity=%s, want %s", e.Reference, e.Validity, want)
		}
		byState[e.Validity] = append(byState[e.Validity], e.Reference)
	}

	// Each SQL filter returns exactly the rows of its state.
	for _, state := range evidenceValidityStates {
		page, err := f.svc.ListEvidence(EvidenceFilter{Validity: strings.ToLower(state), Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, e := range page.Items {
			got = append(got, e.Reference)
		}
		sort.Strings(got)
		want := append([]string{}, byState[state]...)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s filter = %v, want %v", state, got, want)
		}
	}
	// Listed by reference: the exact state of each boundary.
	for state, want := range map[string]string{
		EvidenceValidityValid:        "EVD-LATER,EVD-NOEXP",
		EvidenceValidityExpiringSoon: "EVD-END,EVD-NOW",
		EvidenceValidityExpired:      "EVD-PAST",
		EvidenceValidityInvalid:      "EVD-DRAFT,EVD-REVOKED",
	} {
		if got := strings.Join(byState[state], ","); got != want {
			t.Fatalf("%s = %s, want %s", state, got, want)
		}
	}

	// Dashboard figures are sums of the same states.
	dashboard, err := f.svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	count := func(states ...string) int64 {
		var n int64
		for _, s := range states {
			n += int64(len(byState[s]))
		}
		return n
	}
	e := dashboard.Evidence
	if e.Expired != count(EvidenceValidityExpired) || e.ExpiringSoon != count(EvidenceValidityExpiringSoon) ||
		e.Valid != count(EvidenceValidityValid, EvidenceValidityExpiringSoon) ||
		e.Invalid != count(EvidenceValidityInvalid, EvidenceValidityExpired) {
		t.Fatalf("dashboard %+v diverges from %v", e, byState)
	}

	// A one-day window moves EVD-END back to VALID; bad windows are 400.
	page, err := f.svc.ListEvidence(EvidenceFilter{Validity: "EXPIRING_SOON", ExpiringSoonDays: 1})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "EVD-NOW" {
		t.Fatalf("one-day window: %v %+v", err, page)
	}
	for _, bad := range []EvidenceFilter{{Validity: "STALE"}, {ExpiringSoonDays: -1}, {ExpiringSoonDays: 366}} {
		_, err := f.svc.ListEvidence(bad)
		requireServiceError(t, err, http.StatusBadRequest)
	}
	found, err := f.svc.FindEvidenceWithWindow(page.Items[0].ID, 1)
	if err != nil || found.Validity != EvidenceValidityExpiringSoon {
		t.Fatalf("detail validity: %v %+v", err, found)
	}
}

func TestEvidenceRegistrySearchAndValidation(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	seed := seedEvidenceValidity(t, f)
	other := f.request("EVD-OTHER")
	other.Title = "Preuve d'un autre périmètre"
	other.ControlImplementationID = f.other.ID
	other.Status = EvidenceStatusValid
	f.create(t, other)

	// The search never widens validity, type or implementation filters.
	page, err := f.svc.ListEvidence(EvidenceFilter{Search: "preuve", Validity: "EXPIRED", ControlImplementationID: &f.implementation.ID})
	if err != nil || page.Total != 1 || page.Items[0].ID != seed["EVD-PAST"].ID {
		t.Fatalf("search + validity + implementation: %v %+v", err, page)
	}
	page, err = f.svc.ListEvidence(EvidenceFilter{Search: "preuve", Type: "LOG"})
	if err != nil || page.Total != 0 || page.Items == nil {
		t.Fatalf("search + type: %v %+v", err, page)
	}
	page, err = f.svc.ListEvidence(EvidenceFilter{ControlImplementationID: &f.other.ID})
	if err != nil || page.Total != 1 || page.Items[0].ControlImplementation == nil ||
		page.Items[0].ControlImplementation.Control == nil || page.Items[0].ControlImplementation.Asset == nil {
		t.Fatalf("implementation, control and asset preloaded: %v %+v", err, page)
	}
	page, err = f.svc.ListEvidence(EvidenceFilter{Page: 2, Limit: 3})
	if err != nil || page.Total != 8 || page.TotalPages != 3 || len(page.Items) != 3 {
		t.Fatalf("pagination: %v %+v", err, page)
	}

	// Column lengths, counted in characters.
	r := f.request(strings.Repeat("É", evidenceReferenceMaxLength))
	r.Title = strings.Repeat("é", evidenceTitleMaxLength)
	if _, err := f.svc.SaveEvidence(0, 1, r); err != nil {
		t.Fatalf("limits rejected: %v", err)
	}
	r.Reference = strings.Repeat("É", evidenceReferenceMaxLength+1)
	_, err = f.svc.SaveEvidence(0, 1, r)
	requireServiceError(t, err, http.StatusBadRequest)
	r.Reference, r.Title = "EVD-LONG", strings.Repeat("é", evidenceTitleMaxLength+1)
	_, err = f.svc.SaveEvidence(0, 1, r)
	requireServiceError(t, err, http.StatusBadRequest)
}

func TestEvidenceAssessmentsRead(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	evidence := f.validEvidence(t, "EVD-ASM-07", f.implementation.ID)
	unused := f.validEvidence(t, "EVD-UNUSED-07", f.implementation.ID)
	assessment := f.createAssessment(t, f.assessmentRequest("ASM-EVD-07"))
	if _, err := f.svc.LinkAssessmentEvidence(1, assessment.ID, AssessmentEvidenceRequest{EvidenceID: evidence.ID}); err != nil {
		t.Fatal(err)
	}

	page, err := f.svc.ListAssessments(AssessmentFilter{EvidenceID: &evidence.ID})
	if err != nil || page.Total != 1 || page.Items[0].ID != assessment.ID {
		t.Fatalf("assessments of the evidence: %v %+v", err, page)
	}
	none, err := f.svc.ListAssessments(AssessmentFilter{EvidenceID: &unused.ID})
	if err != nil || none.Total != 0 || none.Items == nil {
		t.Fatalf("assessments of an unused evidence: %v %+v", err, none)
	}
}
