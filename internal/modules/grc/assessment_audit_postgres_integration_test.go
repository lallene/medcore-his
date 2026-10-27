package grc

import (
	"net/http"
	"testing"
	"time"
)

// TestPostgresGRC01DServices runs the 01D services on PostgreSQL: row locks
// (FOR UPDATE) inside the completion and closure transactions, EXISTS
// subqueries, timestamptz instants with offsets and RESTRICT foreign keys.
func TestPostgresGRC01DServices(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	if err := f.db.AutoMigrate(&AuditEngagement{}, &Assessment{}, &AssessmentEvidence{}, &Finding{}); err != nil {
		t.Fatal(err)
	}
	svc := f.svc

	// AuditEngagement: started at now, written at +02:00.
	audit, err := svc.SaveAuditEngagement(0, 11, AuditEngagementRequest{
		Reference: "aud-pg", Title: "Audit PG", Type: "INTERNAL", Status: "IN_PROGRESS", StartedAt: timePtr(pgNowParis),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !audit.StartedAt.Equal(pgNow) {
		t.Fatalf("StartedAt at +02:00 must equal now: %v", audit.StartedAt)
	}
	_, err = svc.SaveAuditEngagement(0, 11, AuditEngagementRequest{
		Reference: "AUD-PG", Title: "Doublon", Type: "INTERNAL", Status: "PLANNED",
	})
	requireServiceError(t, err, http.StatusConflict)
	_, err = svc.SaveAuditEngagement(0, 11, AuditEngagementRequest{
		Reference: "AUD-PG-FUTURE", Title: "Futur", Type: "INTERNAL", Status: "IN_PROGRESS",
		StartedAt: timePtr(pgNowParis.Add(time.Second)),
	})
	requireServiceError(t, err, http.StatusBadRequest)

	// Evidence of the assessed implementation, and of another one.
	saveEvidence := func(reference string, implementationID uint, expiresAt *time.Time) Evidence {
		t.Helper()
		evidence, err := svc.SaveEvidence(0, 12, EvidenceRequest{
			Reference: reference, Title: "Preuve", ControlImplementationID: implementationID,
			Type: "REPORT", Status: "VALID", ExpiresAt: expiresAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		return *evidence
	}
	valid := saveEvidence("EVD-PG-01D", f.implementation.ID, nil)
	foreign := saveEvidence("EVD-PG-01D-OTHER", f.other.ID, nil)

	// Assessment: links under row lock, duplicate and CI coherence.
	request := AssessmentRequest{
		Reference: "ASM-PG", Title: "Évaluation PG", ControlImplementationID: f.implementation.ID,
		Type: "OPERATING_EFFECTIVENESS", Status: "DRAFT",
	}
	assessment, err := svc.SaveAssessment(0, 13, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LinkAssessmentEvidence(13, assessment.ID, AssessmentEvidenceRequest{EvidenceID: valid.ID}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.LinkAssessmentEvidence(13, assessment.ID, AssessmentEvidenceRequest{EvidenceID: valid.ID})
	requireServiceError(t, err, http.StatusConflict)
	_, err = svc.LinkAssessmentEvidence(13, assessment.ID, AssessmentEvidenceRequest{EvidenceID: foreign.ID})
	requireServiceError(t, err, http.StatusConflict)

	// Completion transaction: CompletedAt from the clock, next assessment at
	// now written at +02:00 (not due yet).
	request.Status = "COMPLETED"
	request.Result = stringPtr("EFFECTIVE")
	request.AssessorProfileID = &f.profile.ID
	request.AssessedAt = timePtr(pgNowParis.Add(-time.Hour))
	request.NextAssessmentAt = timePtr(pgNowParis)
	completed, err := svc.SaveAssessment(assessment.ID, 14, request)
	if err != nil {
		t.Fatal(err)
	}
	if completed.CompletedAt == nil || !completed.CompletedAt.Equal(pgNow) || completed.ReassessmentDue || completed.HasInvalidEvidence {
		t.Fatalf("unexpected completed assessment: %+v", completed)
	}

	// Revoking the evidence afterwards is flagged by the EXISTS subquery.
	if _, err := svc.SaveEvidence(valid.ID, 15, EvidenceRequest{
		Reference: valid.Reference, Title: valid.Title, ControlImplementationID: valid.ControlImplementationID,
		Type: valid.Type, Status: "REVOKED",
	}); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return pgNow.Add(time.Second) }
	yes, no := true, false
	for _, tc := range []struct {
		filter AssessmentFilter
		want   int64
	}{
		{AssessmentFilter{HasInvalidEvidence: &yes}, 1},
		{AssessmentFilter{HasInvalidEvidence: &no}, 0},
		{AssessmentFilter{ReassessmentDue: &yes}, 1},
		{AssessmentFilter{ReassessmentDue: &no}, 0},
	} {
		page, err := svc.ListAssessments(tc.filter)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != tc.want {
			t.Fatalf("filter %+v total=%d want %d", tc.filter, page.Total, tc.want)
		}
	}
	found, err := svc.FindAssessment(assessment.ID)
	if err != nil || !found.HasInvalidEvidence || !found.ReassessmentDue || !found.CompletedAt.Equal(pgNow) {
		t.Fatalf("computed fields on PostgreSQL: %v %+v", err, found)
	}
	svc.now = func() time.Time { return pgNow }

	// Findings: from the assessment (implementation defaulted) and the audit.
	fromAssessment, err := svc.SaveFinding(0, 16, FindingRequest{
		Reference: "FND-PG-ASM", Title: "Constat évaluation", AssessmentID: &assessment.ID,
		Category: "CONTROL_DEFICIENCY", Severity: "HIGH", Status: "OPEN", DueAt: timePtr(pgNowParis.Add(-time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if fromAssessment.ControlImplementationID == nil || *fromAssessment.ControlImplementationID != f.implementation.ID ||
		!fromAssessment.Overdue {
		t.Fatalf("unexpected finding from assessment: %+v", fromAssessment)
	}

	findingRequest := FindingRequest{
		Reference: "FND-PG-AUD", Title: "Constat audit", AuditEngagementID: &audit.ID,
		Category: "NON_CONFORMITY", Severity: "MEDIUM", Status: "OPEN", DueAt: timePtr(pgNowParis),
	}
	finding, err := svc.SaveFinding(0, 17, findingRequest)
	if err != nil || finding.Overdue {
		t.Fatalf("finding due at now must not be overdue: %v %+v", err, finding)
	}
	page, err := svc.ListFindings(FindingFilter{Overdue: &yes})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "FND-PG-ASM" {
		t.Fatalf("overdue findings on PostgreSQL: %v %+v", err, page)
	}

	// Closure transaction: blocked while a linked action is open.
	findingRequest.Status = "RESOLVED"
	if _, err := svc.SaveFinding(finding.ID, 18, findingRequest); err != nil {
		t.Fatal(err)
	}
	action := RemediationAction{
		Reference: "ACT-PG-FINDING", Title: "Action", FindingID: &finding.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	findingRequest.Status = "CLOSED"
	svc.now = func() time.Time { return pgNow.Add(time.Hour) }
	_, err = svc.SaveFinding(finding.ID, 19, findingRequest)
	requireServiceError(t, err, http.StatusConflict)

	if err := f.db.Model(&RemediationAction{}).Where("id = ?", action.ID).
		Updates(map[string]any{"status": RemediationActionStatusCancelled}).Error; err != nil {
		t.Fatal(err)
	}
	closed, err := svc.SaveFinding(finding.ID, 20, findingRequest)
	if err != nil || closed.ClosedAt == nil || !closed.ClosedAt.Equal(pgNow.Add(time.Hour)) ||
		closed.ResolvedAt == nil || closed.ClosedAt.Before(*closed.ResolvedAt) {
		t.Fatalf("closure on PostgreSQL: %v %+v", err, closed)
	}

	// RESTRICT: referenced 01D rows survive direct deletes.
	requirePostgresError(t, f.db.Delete(&Assessment{}, assessment.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&AuditEngagement{}, audit.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&Evidence{}, valid.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&Finding{}, finding.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, f.db, &Assessment{}, assessment.ID)
	requireRowExists(t, f.db, &AuditEngagement{}, audit.ID)
	requireRowExists(t, f.db, &Finding{}, finding.ID)
}
