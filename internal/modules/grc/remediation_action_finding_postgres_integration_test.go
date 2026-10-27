package grc

import (
	"net/http"
	"testing"
	"time"
)

// TestPostgresGRC01D08RemediationActionFinding proves on PostgreSQL that the
// service persists finding targets, that the FK, the RESTRICT policy and the
// v2 target CHECK hold, and that an active action blocks the closure of its
// finding until it is done.
func TestPostgresGRC01D08RemediationActionFinding(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	if err := f.db.AutoMigrate(&AuditEngagement{}, &Assessment{}, &AssessmentEvidence{}, &Finding{}); err != nil {
		t.Fatal(err)
	}
	svc := f.svc

	audit, err := svc.SaveAuditEngagement(0, 11, AuditEngagementRequest{
		Reference: "AUD-PG-08", Title: "Audit PG", Type: "INTERNAL", Status: "IN_PROGRESS",
	})
	if err != nil {
		t.Fatal(err)
	}
	findingRequest := FindingRequest{
		Reference: "FND-PG-08", Title: "Constat PG", AuditEngagementID: &audit.ID,
		Category: "CONTROL_DEFICIENCY", Severity: "HIGH", Status: "OPEN",
		ControlImplementationID: &f.implementation.ID, RiskID: &f.risk.ID,
	}
	finding, err := svc.SaveFinding(0, 12, findingRequest)
	if err != nil {
		t.Fatal(err)
	}

	actionRequest := RemediationActionRequest{
		Reference: "ACT-PG-08", Title: "Action PG", FindingID: &finding.ID,
		ControlImplementationID: &f.implementation.ID, Priority: "HIGH", Status: "OPEN",
	}
	action, err := svc.SaveRemediationAction(0, 13, actionRequest)
	if err != nil {
		t.Fatal(err)
	}
	var stored RemediationAction
	if err := f.db.First(&stored, action.ID).Error; err != nil || stored.FindingID == nil || *stored.FindingID != finding.ID {
		t.Fatalf("finding target not persisted: %v %+v", err, stored)
	}

	// Service-level rules before PostgreSQL.
	incoherent := actionRequest
	incoherent.Reference = "ACT-PG-08-BAD"
	incoherent.ControlImplementationID = &f.other.ID
	_, err = svc.SaveRemediationAction(0, 13, incoherent)
	requireServiceError(t, err, http.StatusConflict)
	unknown := uint(999999)
	incoherent.ControlImplementationID = nil
	incoherent.FindingID = &unknown
	_, err = svc.SaveRemediationAction(0, 13, incoherent)
	requireServiceError(t, err, http.StatusNotFound)

	// PostgreSQL itself: FK, v2 target CHECK and RESTRICT.
	direct := RemediationAction{
		Reference: "ACT-PG-08-DIRECT", Title: "Directe", FindingID: &unknown,
		Priority: RemediationActionPriorityLow, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
	}
	requirePostgresError(t, f.db.Create(&direct).Error, pgForeignKeyViolation, "")
	direct.FindingID = nil
	requirePostgresError(t, f.db.Create(&direct).Error, pgCheckViolation, RemediationActionTargetConstraint)
	findingOnly := RemediationAction{
		Reference: "ACT-PG-08-FINDING", Title: "Constat seul", FindingID: &finding.ID,
		Priority: RemediationActionPriorityLow, Status: RemediationActionStatusCancelled, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&findingOnly).Error; err != nil {
		t.Fatalf("finding-only action rejected by PostgreSQL: %v", err)
	}
	requirePostgresError(t, f.db.Delete(&Finding{}, finding.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, f.db, &Finding{}, finding.ID)

	// Closure: blocked while the service-created action is active.
	findingRequest.Status = "RESOLVED"
	if _, err := svc.SaveFinding(finding.ID, 14, findingRequest); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return pgNow.Add(time.Hour) }
	findingRequest.Status = "CLOSED"
	_, err = svc.SaveFinding(finding.ID, 15, findingRequest)
	requireServiceError(t, err, http.StatusConflict)

	actionRequest.Status = "DONE"
	if _, err := svc.SaveRemediationAction(action.ID, 16, actionRequest); err != nil {
		t.Fatal(err)
	}
	closed, err := svc.SaveFinding(finding.ID, 17, findingRequest)
	if err != nil || closed.Status != FindingStatusClosed || closed.ClosedAt == nil {
		t.Fatalf("closure after the action is done: %v %+v", err, closed)
	}

	// A closed finding accepts no new action.
	late := actionRequest
	late.Reference = "ACT-PG-08-LATE"
	late.Status = "OPEN"
	_, err = svc.SaveRemediationAction(0, 18, late)
	requireServiceError(t, err, http.StatusConflict)
}
