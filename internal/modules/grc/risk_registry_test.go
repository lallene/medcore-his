package grc

import (
	"net/http"
	"strings"
	"testing"
)

// GRC-04: pure validation, derived levels, relation rules on update, list
// filters and the read-only relations of the risk register.

func TestValidateRiskRequest(t *testing.T) {
	f := newAssetFixture(t)
	zero := uint(0)
	assetID := uint(1)

	valid := validRiskRequest(f, assetID)
	normalizeRiskRequest(&valid)
	if err := validateRiskRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if valid.Reference != "RISK-001" || valid.Title != "Indisponibilité MedCore" ||
		valid.Treatment != RiskTreatmentReduce || valid.Status != RiskStatusOpen || valid.Threat != "Panne infrastructure" {
		t.Fatalf("unexpected normalization: %+v", valid)
	}

	atLimit := valid
	atLimit.Reference = strings.Repeat("É", riskReferenceMaxLength)
	atLimit.Title = strings.Repeat("é", riskTitleMaxLength)
	if err := validateRiskRequest(atLimit); err != nil {
		t.Fatalf("lengths at the column limit rejected: %v", err)
	}

	cases := map[string]func(*RiskRequest){
		"whitespace reference": func(r *RiskRequest) { r.Reference = "  " },
		"whitespace title":     func(r *RiskRequest) { r.Title = "\t" },
		"reference too long":   func(r *RiskRequest) { r.Reference = strings.Repeat("É", riskReferenceMaxLength+1) },
		"title too long":       func(r *RiskRequest) { r.Title = strings.Repeat("é", riskTitleMaxLength+1) },
		"invalid treatment":    func(r *RiskRequest) { r.Treatment = "IGNORE" },
		"invalid status":       func(r *RiskRequest) { r.Status = "ARCHIVED" },
		"likelihood 0":         func(r *RiskRequest) { r.Likelihood = 0 },
		"impact 6":             func(r *RiskRequest) { r.Impact = 6 },
		"residual likelihood":  func(r *RiskRequest) { r.ResidualLikelihood = -1 },
		"residual impact":      func(r *RiskRequest) { r.ResidualImpact = 6 },
		"zero asset":           func(r *RiskRequest) { r.AssetID = &zero },
		"zero service":         func(r *RiskRequest) { r.OwningServiceID = &zero },
		"zero owner":           func(r *RiskRequest) { r.OwnerProfileID = &zero },
		"zero approver":        func(r *RiskRequest) { r.ApproverProfileID = &zero },
	}
	for name, mutate := range cases {
		r := valid
		mutate(&r)
		normalizeRiskRequest(&r)
		if err := validateRiskRequest(r); err == nil {
			t.Fatalf("%s accepted", name)
		}
		requireServiceError(t, validateRiskRequest(r), http.StatusBadRequest)
	}
}

func TestRiskLevelScoresFollowRiskLevel(t *testing.T) {
	// Every score of the scale belongs to exactly one level filter, the one
	// RiskLevel returns: the filter cannot drift from the classification.
	seen := map[int]string{}
	for _, level := range riskLevels {
		scores, err := riskLevelScores(level)
		if err != nil {
			t.Fatal(err)
		}
		for _, score := range scores {
			if previous, dup := seen[score]; dup {
				t.Fatalf("score %d in %s and %s", score, previous, level)
			}
			seen[score] = level
			if want, _ := RiskLevel(score); want != level {
				t.Fatalf("score %d filtered as %s, RiskLevel says %s", score, level, want)
			}
		}
	}
	if len(seen) != 25 {
		t.Fatalf("level filters cover %d scores, want 25", len(seen))
	}
	_, err := riskLevelScores("SEVERE")
	requireServiceError(t, err, http.StatusBadRequest)
}

func TestSaveRiskComputesScoresAndLevels(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	// Products on the RiskLevel boundaries (14 is not a product of 1–5).
	cases := []struct {
		likelihood, impact, score int
		level                     string
	}{
		{1, 1, 1, RiskLevelLow},
		{2, 2, 4, RiskLevelLow},
		{1, 5, 5, RiskLevelMedium},
		{3, 3, 9, RiskLevelMedium},
		{2, 5, 10, RiskLevelHigh},
		{3, 4, 12, RiskLevelHigh},
		{3, 5, 15, RiskLevelCritical},
		{5, 5, 25, RiskLevelCritical},
	}
	for i, tc := range cases {
		r := validRiskRequest(f, asset.ID)
		r.Reference = "RISK-SCORE-" + string(rune('A'+i))
		r.Likelihood, r.Impact = tc.likelihood, tc.impact
		r.ResidualLikelihood, r.ResidualImpact = tc.impact, tc.likelihood
		risk, err := svc.SaveRisk(0, 1, r)
		if err != nil {
			t.Fatal(err)
		}
		if risk.InherentScore != tc.score || risk.ResidualScore != tc.score ||
			risk.InherentLevel != tc.level || risk.ResidualLevel != tc.level {
			t.Fatalf("%dx%d: got %d/%s %d/%s, want %d/%s", tc.likelihood, tc.impact,
				risk.InherentScore, risk.InherentLevel, risk.ResidualScore, risk.ResidualLevel, tc.score, tc.level)
		}
	}

	// Inherent and residual levels are independent.
	r := validRiskRequest(f, asset.ID)
	r.Reference = "RISK-SPLIT"
	r.Likelihood, r.Impact, r.ResidualLikelihood, r.ResidualImpact = 5, 5, 1, 2
	risk, err := svc.SaveRisk(0, 1, r)
	if err != nil {
		t.Fatal(err)
	}
	found, err := svc.FindRisk(risk.ID)
	if err != nil || found.InherentLevel != RiskLevelCritical || found.ResidualLevel != RiskLevelLow {
		t.Fatalf("split levels: %v %+v", err, found)
	}
}

func TestSaveRiskKeepsHistoricalRelationsEditable(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	r := validRiskRequest(f, asset.ID)
	risk, err := svc.SaveRisk(0, 1, r)
	if err != nil {
		t.Fatal(err)
	}

	// Asset retired, service and profiles deactivated after assignment.
	retire := validAssetRequest(f)
	retire.Status = AssetStatusRetired
	if _, err := svc.SaveAsset(asset.ID, 1, retire); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.profile).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	r.Status = RiskStatusClosed
	r.ResidualLikelihood = 1
	closed, err := svc.SaveRisk(risk.ID, 2, r)
	if err != nil || closed.Status != RiskStatusClosed || closed.ResidualScore != 3 || closed.CreatedBy != 1 || closed.UpdatedBy != 2 {
		t.Fatalf("update with historical relations: %v %+v", err, closed)
	}

	// A newly assigned retired asset or inactive profile is refused.
	other := validAssetRequest(f)
	other.Code, other.Status, other.OwningServiceID, other.OwnerProfileID = "AST-RETIRED", AssetStatusRetired, nil, nil
	retired, err := svc.SaveAsset(0, 1, other)
	if err != nil {
		t.Fatal(err)
	}
	r.AssetID = &retired.ID
	_, err = svc.SaveRisk(risk.ID, 2, r)
	requireServiceError(t, err, http.StatusConflict)
	r.AssetID = &asset.ID
	r.ApproverProfileID = &f.inactiveProfile.ID
	_, err = svc.SaveRisk(risk.ID, 2, r)
	requireServiceError(t, err, http.StatusConflict)

	unknown := uint(999999)
	r.ApproverProfileID = nil
	r.AssetID = &unknown
	_, err = svc.SaveRisk(risk.ID, 2, r)
	requireServiceError(t, err, http.StatusNotFound)

	_, err = svc.SaveRisk(999999, 2, validRiskRequest(f, asset.ID))
	requireServiceError(t, err, http.StatusNotFound)
	_, err = svc.FindRisk(999999)
	requireServiceError(t, err, http.StatusNotFound)
}

func TestListRisksRegistry(t *testing.T) {
	f := newAssetFixture(t)
	svc := NewService(f.db)
	asset := createRiskAsset(t, f)

	seed := []struct {
		reference, title, status string
		l, i, rl, ri             int
	}{
		{"RISK-A", "Panne serveur", RiskStatusOpen, 5, 5, 3, 4},        // 25 CRITICAL / 12 HIGH
		{"RISK-B", "Fuite de données", RiskStatusOpen, 4, 4, 1, 3},     // 16 CRITICAL / 3 LOW
		{"RISK-C", "Panne réseau", RiskStatusClosed, 2, 5, 2, 5},       // 10 HIGH / 10 HIGH
		{"RISK-D", "Erreur de saisie", RiskStatusDraft, 1, 5, 1, 1},    // 5 MEDIUM / 1 LOW
		{"RISK-E", "Panne électrique", RiskStatusAccepted, 3, 5, 3, 5}, // 15 CRITICAL / 15 CRITICAL
	}
	for _, s := range seed {
		r := validRiskRequest(f, asset.ID)
		r.Reference, r.Title, r.Status = s.reference, s.title, s.status
		r.Likelihood, r.Impact, r.ResidualLikelihood, r.ResidualImpact = s.l, s.i, s.rl, s.ri
		if s.reference == "RISK-D" {
			r.AssetID, r.OwnerProfileID = nil, nil
		}
		if _, err := svc.SaveRisk(0, 1, r); err != nil {
			t.Fatal(err)
		}
	}

	list := func(filter RiskFilter) RiskPage {
		t.Helper()
		page, err := svc.ListRisks(filter)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	refs := func(page RiskPage) string {
		out := make([]string, 0, len(page.Items))
		for _, r := range page.Items {
			out = append(out, r.Reference)
		}
		return strings.Join(out, ",")
	}
	same := func(name string, page RiskPage, want string) {
		t.Helper()
		if got := refs(page); got != want {
			t.Fatalf("%s = %s, want %s", name, got, want)
		}
	}

	all := list(RiskFilter{})
	same("all", all, "RISK-A,RISK-B,RISK-C,RISK-D,RISK-E")
	for _, r := range all.Items {
		if r.InherentLevel == "" || r.ResidualLevel == "" {
			t.Fatalf("levels not derived in the list: %+v", r)
		}
	}

	// The search is parenthesized: it never bypasses the other filters.
	same("search title", list(RiskFilter{Search: "DONNÉES"}), "RISK-B")
	same("search threat", list(RiskFilter{Search: "infrastructure"}), "RISK-A,RISK-B,RISK-C,RISK-D,RISK-E")
	same("search + status", list(RiskFilter{Search: "infrastructure", Status: "open"}), "RISK-A,RISK-B")
	same("residual critical", list(RiskFilter{ResidualLevel: "critical"}), "RISK-E")
	same("residual high", list(RiskFilter{ResidualLevel: "HIGH"}), "RISK-A,RISK-C")
	same("inherent critical", list(RiskFilter{InherentLevel: "CRITICAL"}), "RISK-A,RISK-B,RISK-E")
	same("inherent medium + residual low", list(RiskFilter{InherentLevel: "MEDIUM", ResidualLevel: "LOW"}), "RISK-D")
	same("asset", list(RiskFilter{AssetID: &asset.ID}), "RISK-A,RISK-B,RISK-C,RISK-E")
	same("owner", list(RiskFilter{OwnerProfileID: &f.profile.ID}), "RISK-A,RISK-B,RISK-C,RISK-E")
	same("service", list(RiskFilter{ServiceID: &f.service.ID, Status: "ACCEPTED"}), "RISK-E")

	_, err := svc.ListRisks(RiskFilter{ResidualLevel: "SEVERE"})
	requireServiceError(t, err, http.StatusBadRequest)

	page := list(RiskFilter{Page: 2, Limit: 2})
	same("page 2", page, "RISK-C,RISK-D")
	if page.Total != 5 || page.TotalPages != 3 || page.Limit != 2 {
		t.Fatalf("page meta: %+v", page)
	}
	empty := list(RiskFilter{Search: "absent"})
	if empty.Items == nil || empty.Total != 0 || empty.TotalPages != 0 {
		t.Fatalf("empty page must have a non-nil list and zero pages: %+v", empty)
	}
}

func TestRiskRelationsAreReadableByRisk(t *testing.T) {
	f := newGRC01DServiceFixture(t)

	// Controls: through grc_control_risks only.
	controls, err := f.svc.ListControls(ControlFilter{RiskID: &f.risk.ID})
	if err != nil || controls.Total != 1 || controls.Items[0].ID != f.implementation.ControlID {
		t.Fatalf("controls of the risk: %v %+v", err, controls)
	}
	none, err := f.svc.ListControls(ControlFilter{RiskID: &f.otherRisk.ID})
	if err != nil || none.Total != 0 {
		t.Fatalf("unlinked risk has controls: %v %+v", err, none)
	}

	// Remediation actions and findings: existing riskId filters.
	action := f.actionRequest("ACT-RISK-04")
	action.RiskID = &f.risk.ID
	f.createAction(t, action)
	actions, err := f.svc.ListRemediationActions(RemediationActionFilter{RiskID: &f.risk.ID})
	if err != nil || actions.Total != 1 {
		t.Fatalf("actions of the risk: %v %+v", err, actions)
	}
	auditRequest := f.auditRequest("AUD-RISK-04")
	auditRequest.Status = AuditEngagementStatusInProgress
	audit := f.createAudit(t, auditRequest)
	if _, err := f.svc.SaveFinding(0, 1, FindingRequest{
		Reference: "FND-RISK-04", Title: "Constat", AuditEngagementID: &audit.ID,
		Category: "OBSERVATION", Severity: "LOW", Status: "OPEN", RiskID: &f.risk.ID,
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := f.svc.ListFindings(FindingFilter{RiskID: &f.risk.ID})
	if err != nil || findings.Total != 1 {
		t.Fatalf("findings of the risk: %v %+v", err, findings)
	}
}
