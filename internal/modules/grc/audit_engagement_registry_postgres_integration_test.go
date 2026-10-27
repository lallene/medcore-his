package grc

import (
	"net/http"
	"testing"
	"time"
)

// TestPostgresGRC09Audits runs audits and their findings on PostgreSQL:
// persistence and lifecycle, UNIQUE/CHECK/FK/RESTRICT, finding source
// immutability and lifecycle, finding → risk and → action relations with the
// close guard, audit finding counters against the dashboard (timestamptz
// boundaries written at +02:00), search, filters and pagination.
func TestPostgresGRC09Audits(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	db := f.db
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	svc := f.svc
	day := 24 * time.Hour
	at := func(d time.Duration) *time.Time { v := pgNowParis.Add(d); return &v }

	// Audit persistence and lifecycle.
	request := AuditEngagementRequest{
		Reference: "aud-pg-09", Title: "Audit PostgreSQL", Description: "Texte", Scope: "Périmètre",
		Type: "INTERNAL", Status: "PLANNED", LeadAuditorProfileID: &f.profile.ID, OwningServiceID: &f.service.ID,
		PlannedStartAt: at(-2 * day), PlannedEndAt: at(5 * day),
	}
	audit, err := svc.SaveAuditEngagement(0, 41, request)
	if err != nil || audit.Reference != "AUD-PG-09" || audit.CreatedBy != 41 || audit.StartedAt != nil {
		t.Fatalf("create: %v %+v", err, audit)
	}
	request.Status = "IN_PROGRESS"
	started, err := svc.SaveAuditEngagement(audit.ID, 42, request)
	if err != nil || started.StartedAt == nil || !started.StartedAt.Equal(pgNow) {
		t.Fatalf("start: %v %+v", err, started)
	}
	_, err = svc.SaveAuditEngagement(0, 41, AuditEngagementRequest{Reference: "AUD-PG-09", Title: "Doublon", Type: "INTERNAL", Status: "PLANNED"})
	requireServiceError(t, err, http.StatusConflict)

	// CHECK and FK on audits.
	direct := AuditEngagement{Reference: "AUD-PG-DIRECT", Title: "Direct", Type: "INTERNAL", Status: "PLANNED", CreatedBy: 1, UpdatedBy: 1}
	bad := direct
	bad.Status = "APPROVED"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_audit_engagement_status_valid")
	bad = direct
	bad.Type = "SELF"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_audit_engagement_type_valid")
	bad = direct
	bad.PlannedStartAt, bad.PlannedEndAt = at(day), at(0)
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_audit_engagement_planned_range")
	bad = direct
	bad.Status = "IN_PROGRESS"
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_audit_engagement_started_required")
	bad = direct
	bad.Status, bad.StartedAt = "COMPLETED", at(-day)
	requirePostgresError(t, db.Create(&bad).Error, pgCheckViolation, "grc_audit_engagement_completion")
	missing := uint(999999)
	bad = direct
	bad.LeadAuditorProfileID = &missing
	requirePostgresError(t, db.Create(&bad).Error, pgForeignKeyViolation, "")

	// Findings: persistence, source immutability, CHECK, FK.
	findingRequest := func(reference string, due *time.Time) FindingRequest {
		return FindingRequest{
			Reference: reference, Title: "Constat " + reference, Description: "Texte", AuditEngagementID: &audit.ID,
			Category: "CONTROL_DEFICIENCY", Severity: "HIGH", Status: "OPEN", DueAt: due,
			ControlImplementationID: &f.implementation.ID, RiskID: &f.risk.ID,
		}
	}
	late, err := svc.SaveFinding(0, 43, findingRequest("FND-PG-LATE", at(-time.Second)))
	if err != nil || late.Risk == nil || late.AuditEngagement == nil || !late.Overdue {
		t.Fatalf("finding: %v %+v", err, late)
	}
	if _, err := svc.SaveFinding(0, 43, findingRequest("FND-PG-NOW", at(0))); err != nil {
		t.Fatal(err)
	}
	other, err := svc.SaveAuditEngagement(0, 1, AuditEngagementRequest{Reference: "AUD-PG-09-B", Title: "Autre", Type: "VENDOR", Status: "IN_PROGRESS"})
	if err != nil {
		t.Fatal(err)
	}
	moved := findingRequest("FND-PG-LATE", at(-time.Second))
	moved.AuditEngagementID = &other.ID
	_, err = svc.SaveFinding(late.ID, 44, moved)
	requireServiceError(t, err, http.StatusConflict)

	directFinding := Finding{Reference: "FND-PG-DIRECT", Title: "Direct", AuditEngagementID: &audit.ID, Category: "OBSERVATION",
		Severity: "LOW", Status: "OPEN", CreatedBy: 1, UpdatedBy: 1}
	badFinding := directFinding
	badFinding.AuditEngagementID = nil
	requirePostgresError(t, db.Create(&badFinding).Error, pgCheckViolation, "grc_finding_single_source")
	badFinding = directFinding
	badFinding.Severity = "EXTREME"
	requirePostgresError(t, db.Create(&badFinding).Error, pgCheckViolation, "grc_finding_severity_valid")
	badFinding = directFinding
	badFinding.Status = "REOPENED"
	requirePostgresError(t, db.Create(&badFinding).Error, pgCheckViolation, "grc_finding_status_valid")
	badFinding = directFinding
	badFinding.AuditEngagementID = &missing
	requirePostgresError(t, db.Create(&badFinding).Error, pgForeignKeyViolation, "")
	badFinding = directFinding
	badFinding.Reference = "FND-PG-LATE"
	requirePostgresError(t, db.Create(&badFinding).Error, pgUniqueViolation, "")

	// Counters with the dashboard semantics, compared with the dashboard.
	page, err := svc.ListAuditEngagements(AuditEngagementFilter{Search: "aud-pg-09"})
	if err != nil || page.Total != 2 {
		t.Fatalf("audit list: %v %+v", err, page)
	}
	if found, err := svc.FindAuditEngagement(audit.ID); err != nil ||
		found.FindingCount != 2 || found.OpenFindingCount != 2 || found.OverdueFindingCount != 1 {
		t.Fatalf("counters: %v %+v", err, found)
	}
	dashboard, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if d := dashboard.Findings; d.Total != 2 || d.Open != 2 || d.Overdue != 1 ||
		countOf(d.BySeverity, FindingSeverityHigh) != 2 || countOf(d.ByStatus, FindingStatusOpen) != 2 {
		t.Fatalf("dashboard findings %+v diverge", d)
	}
	if countOf(dashboard.Audits.ByStatus, AuditEngagementStatusInProgress) != 2 || dashboard.Audits.Total != 2 {
		t.Fatalf("dashboard audits %+v diverge", dashboard.Audits)
	}

	// Finding → action and close guard; RESTRICT on the links.
	action, err := svc.SaveRemediationAction(0, 1, RemediationActionRequest{
		Reference: "ACT-PG-09", Title: "Action", FindingID: &late.ID, Priority: "HIGH", Status: "OPEN",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := findingRequest("FND-PG-LATE", at(-time.Second))
	resolved.Status = FindingStatusResolved
	if _, err := svc.SaveFinding(late.ID, 45, resolved); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return pgNow.Add(time.Hour) }
	resolved.Status = FindingStatusClosed
	_, err = svc.SaveFinding(late.ID, 46, resolved)
	requireServiceError(t, err, http.StatusConflict)
	requirePostgresError(t, db.Delete(&Finding{}, late.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&AuditEngagement{}, audit.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, db.Delete(&Risk{}, f.risk.ID).Error, pgForeignKeyViolation, "")
	if _, err := svc.SaveRemediationAction(action.ID, 1, RemediationActionRequest{
		Reference: "ACT-PG-09", Title: "Action", FindingID: &late.ID, Priority: "HIGH", Status: "DONE",
	}); err != nil {
		t.Fatal(err)
	}
	closed, err := svc.SaveFinding(late.ID, 47, resolved)
	if err != nil || closed.Status != FindingStatusClosed || closed.ClosedAt == nil {
		t.Fatalf("close: %v %+v", err, closed)
	}
	resolved.Status = FindingStatusOpen
	_, err = svc.SaveFinding(late.ID, 48, resolved)
	requireServiceError(t, err, http.StatusConflict)

	// Audit completion and terminal state.
	request.Status = AuditEngagementStatusCompleted
	completed, err := svc.SaveAuditEngagement(audit.ID, 49, request)
	if err != nil || completed.CompletedAt == nil || completed.FindingCount != 2 || completed.OpenFindingCount != 1 {
		t.Fatalf("complete: %v %+v", err, completed)
	}
	request.Status = AuditEngagementStatusInProgress
	_, err = svc.SaveAuditEngagement(audit.ID, 50, request)
	requireServiceError(t, err, http.StatusConflict)

	// Search, filters and pagination.
	filtered, err := svc.ListAuditEngagements(AuditEngagementFilter{Search: "aud-pg", Status: "COMPLETED", Type: "INTERNAL", LeadAuditorProfileID: &f.profile.ID})
	if err != nil || filtered.Total != 1 || filtered.Items[0].ID != audit.ID {
		t.Fatalf("filters: %v %+v", err, filtered)
	}
	first, err := svc.ListAuditEngagements(AuditEngagementFilter{Page: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ListAuditEngagements(AuditEngagementFilter{Page: 2, Limit: 1})
	if err != nil || second.Total != 2 || second.TotalPages != 2 || first.Items[0].ID == second.Items[0].ID {
		t.Fatalf("pagination: %v %+v %+v", err, first, second)
	}
	findings, err := svc.ListFindings(FindingFilter{AuditEngagementID: &audit.ID, Severity: "HIGH", RiskID: &f.risk.ID, Search: "fnd-pg"})
	if err != nil || findings.Total != 2 {
		t.Fatalf("finding filters: %v %+v", err, findings)
	}
}
