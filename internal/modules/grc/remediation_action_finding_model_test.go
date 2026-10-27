package grc

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestRemediationActionTargetCheckIsOnlyV2(t *testing.T) {
	f := newAssessmentFixture(t)

	stmt := &gorm.Statement{DB: f.db}
	if err := stmt.Parse(&RemediationAction{}); err != nil {
		t.Fatal(err)
	}
	checks := stmt.Schema.ParseCheckConstraints()
	if _, ok := checks[RemediationActionLegacyTargetConstraint]; ok {
		t.Fatalf("model must not declare the legacy CHECK %s", RemediationActionLegacyTargetConstraint)
	}
	v2, ok := checks[RemediationActionTargetConstraint]
	if !ok {
		t.Fatalf("model must declare %s", RemediationActionTargetConstraint)
	}
	if v2.Constraint != remediationActionTargetCheck {
		t.Fatalf("model CHECK %q differs from the migration expression %q", v2.Constraint, remediationActionTargetCheck)
	}

	// A fresh schema only contains the v2 constraint.
	var ddl string
	if err := f.db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'grc_remediation_actions'").Scan(&ddl).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ddl, RemediationActionTargetConstraint) {
		t.Fatalf("fresh schema misses %s: %s", RemediationActionTargetConstraint, ddl)
	}
	if strings.Contains(ddl, "CONSTRAINT `"+RemediationActionLegacyTargetConstraint+"`") {
		t.Fatalf("fresh schema must not contain %s: %s", RemediationActionLegacyTargetConstraint, ddl)
	}
	if !f.db.Migrator().HasColumn(&RemediationAction{}, "finding_id") {
		t.Fatal("grc_remediation_actions.finding_id missing")
	}
}

func TestRemediationActionModelFindingTarget(t *testing.T) {
	f := newAssessmentFixture(t)
	audit := f.createAuditEngagement(t, "AUD-ACTION")
	finding := f.finding("FND-ACTION", audit.ID)
	requireCreate(t, f, "finding", &finding)

	action := func(reference string, mutate func(*RemediationAction)) RemediationAction {
		a := validRemediationAction(f.evidenceFixture, reference)
		a.ControlImplementationID = nil
		mutate(&a)
		return a
	}

	accepted := map[string]RemediationAction{
		"finding only":        action("ACT-FINDING", func(a *RemediationAction) { a.FindingID = &finding.ID }),
		"control only":        action("ACT-CI", func(a *RemediationAction) { a.ControlImplementationID = &f.implementation.ID }),
		"risk only":           action("ACT-RISK", func(a *RemediationAction) { a.RiskID = &f.risk.ID }),
		"control and finding": action("ACT-CI-FINDING", func(a *RemediationAction) { a.ControlImplementationID, a.FindingID = &f.implementation.ID, &finding.ID }),
		"risk and finding":    action("ACT-RISK-FINDING", func(a *RemediationAction) { a.RiskID, a.FindingID = &f.risk.ID, &finding.ID }),
		"three targets": action("ACT-ALL", func(a *RemediationAction) {
			a.ControlImplementationID, a.RiskID, a.FindingID = &f.implementation.ID, &f.risk.ID, &finding.ID
		}),
	}
	for name, a := range accepted {
		requireCreate(t, f, name, &a)
	}

	targetless := action("ACT-NONE", func(a *RemediationAction) {})
	requireRejected(t, f, "targetless action", &targetless)

	unknown := uint(999999)
	unknownFinding := action("ACT-UNKNOWN-FINDING", func(a *RemediationAction) { a.FindingID = &unknown })
	requireRejected(t, f, "unknown finding", &unknownFinding)

	requireRestrictConstraints(t, f.db, &RemediationAction{}, "Finding")
	if err := f.db.Delete(&Finding{}, finding.ID).Error; err == nil {
		t.Fatal("deleting a finding referenced by an action must be restricted")
	}
	requireRowExists(t, f.db, &Finding{}, finding.ID)
}
