package grc

import (
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestPostgresGRC07Evidence runs the evidence registry on PostgreSQL:
// persistence and update, REVOKED as terminal, UNIQUE/CHECK/FK/RESTRICT, the
// shared validity definition on timestamptz boundaries written at +02:00
// (filters, computed field and dashboard), search with filters, pagination
// and the evidence → assessments read.
func TestPostgresGRC07Evidence(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := f.svc
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := pgNowParis.Add(d); return &v }

	request := func(reference, status string, expires *time.Time) EvidenceRequest {
		return EvidenceRequest{
			Reference: reference, Title: "Preuve " + reference, ControlImplementationID: f.implementation.ID,
			Type: EvidenceTypeReport, Status: status, ExpiresAt: expires,
			ExternalReference: "https://ged.example.org/docs/" + reference,
		}
	}

	// Persistence and update.
	created, err := svc.SaveEvidence(0, 41, request("EVD-PG-07", EvidenceStatusDraft, nil))
	if err != nil {
		t.Fatal(err)
	}
	var stored Evidence
	if err := db.First(&stored, created.ID).Error; err != nil || stored.Reference != "EVD-PG-07" || stored.CreatedBy != 41 ||
		stored.ExternalReference != "https://ged.example.org/docs/EVD-PG-07" {
		t.Fatalf("stored evidence: %v %+v", err, stored)
	}
	updated, err := svc.SaveEvidence(created.ID, 42, request("EVD-PG-07", EvidenceStatusValid, at(10*day)))
	if err != nil || updated.Validity != EvidenceValidityExpiringSoon || updated.UpdatedBy != 42 || updated.CreatedBy != 41 {
		t.Fatalf("update: %v %+v", err, updated)
	}

	// UNIQUE, CHECK and FK.
	_, err = svc.SaveEvidence(0, 1, request("EVD-PG-07", EvidenceStatusDraft, nil))
	requireServiceError(t, err, http.StatusConflict)
	direct := Evidence{Reference: "EVD-PG-07", Title: "Direct", ControlImplementationID: f.implementation.ID, Type: "REPORT", Status: "DRAFT", CreatedBy: 1, UpdatedBy: 1}
	requirePostgresError(t, db.Create(&direct).Error, pgUniqueViolation, "")
	direct.Reference = "EVD-PG-CHECK"
	bad := direct
	bad.Type = "VIDEO"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_evidence_type_valid")
	bad = direct
	bad.Status = "ARCHIVED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_evidence_status_valid")
	bad = direct
	bad.ValidFrom, bad.ExpiresAt = at(day), at(0)
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_evidence_validity_range")
	bad = direct
	bad.ControlImplementationID = 999999
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")

	// Validity boundaries (written at +02:00, compared as instants).
	for _, e := range []struct {
		reference, status string
		expires           *time.Time
	}{
		{"EVD-NOEXP", EvidenceStatusValid, nil},
		{"EVD-PAST", EvidenceStatusValid, at(-time.Second)},
		{"EVD-NOW", EvidenceStatusValid, at(0)},
		{"EVD-END", EvidenceStatusValid, at(30 * day)},
		{"EVD-LATER", EvidenceStatusValid, at(30*day + time.Second)},
		{"EVD-DRAFT", EvidenceStatusDraft, at(-day)},
		{"EVD-REVOKED", EvidenceStatusRevoked, nil},
	} {
		if _, err := svc.SaveEvidence(0, 1, request(e.reference, e.status, e.expires)); err != nil {
			t.Fatal(err)
		}
	}
	states := map[string]string{}
	for _, state := range evidenceValidityStates {
		page, err := svc.ListEvidence(EvidenceFilter{Validity: state, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, e := range page.Items {
			if e.Validity != state {
				t.Fatalf("%s listed under %s has validity %s", e.Reference, state, e.Validity)
			}
			refs = append(refs, e.Reference)
		}
		sort.Strings(refs)
		states[state] = strings.Join(refs, ",")
	}
	want := map[string]string{
		EvidenceValidityValid:        "EVD-LATER,EVD-NOEXP",
		EvidenceValidityExpiringSoon: "EVD-END,EVD-NOW,EVD-PG-07",
		EvidenceValidityExpired:      "EVD-PAST",
		EvidenceValidityInvalid:      "EVD-DRAFT,EVD-REVOKED",
	}
	for state, refs := range want {
		if states[state] != refs {
			t.Fatalf("%s = %s, want %s", state, states[state], refs)
		}
	}
	dashboard, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if e := dashboard.Evidence; e.Expired != 1 || e.ExpiringSoon != 3 || e.Valid != 5 || e.Invalid != 3 {
		t.Fatalf("dashboard evidence %+v diverges from the validity states", e)
	}

	// REVOKED is terminal.
	revoked, err := svc.ListEvidence(EvidenceFilter{Search: "EVD-REVOKED"})
	if err != nil || revoked.Total != 1 {
		t.Fatalf("revoked lookup: %v %+v", err, revoked)
	}
	_, err = svc.SaveEvidence(revoked.Items[0].ID, 1, request("EVD-REVOKED", EvidenceStatusValid, nil))
	requireServiceError(t, err, http.StatusConflict)

	// Search with filters, and pagination.
	page, err := svc.ListEvidence(EvidenceFilter{Search: "evd-", Validity: "EXPIRED", Type: "REPORT", ControlImplementationID: &f.implementation.ID})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "EVD-PAST" {
		t.Fatalf("search + filters: %v %+v", err, page)
	}
	first, err := svc.ListEvidence(EvidenceFilter{Page: 1, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListEvidence(EvidenceFilter{Page: 2, Limit: 5})
	if err != nil || first.Total != 8 || second.TotalPages != 2 || len(second.Items) != 3 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("pagination: %v %+v %+v", err, first, second)
	}

	// Evidence → assessments, and RESTRICT on the links.
	assessment, err := svc.SaveAssessment(0, 1, AssessmentRequest{
		Reference: "ASM-PG-07", Title: "Évaluation", ControlImplementationID: f.implementation.ID,
		Type: AssessmentTypeDesign, Status: AssessmentStatusDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkAssessmentEvidence(1, assessment.ID, AssessmentEvidenceRequest{EvidenceID: created.ID}); err != nil {
		t.Fatal(err)
	}
	assessments, err := svc.ListAssessments(AssessmentFilter{EvidenceID: &created.ID})
	if err != nil || assessments.Total != 1 || assessments.Items[0].ID != assessment.ID {
		t.Fatalf("assessments of the evidence: %v %+v", err, assessments)
	}
	requirePostgresError(t, db.Delete(&Evidence{}, created.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&ControlImplementation{}, f.implementation.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, db, &Evidence{}, created.ID)
}
