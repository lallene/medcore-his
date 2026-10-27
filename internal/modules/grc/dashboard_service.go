package grc

import (
	"math"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
)

// The dashboard is read-only: every figure is a SQL aggregate (COUNT, GROUP
// BY, EXISTS) over existing GRC tables, computed at one instant. Time-based
// figures reuse the validated rules: expired/overdue are strictly before now,
// and "soon" is the complementary window now <= date <= now + days, so an
// item is never counted twice.

var (
	assetStatusKeys        = []string{AssetStatusActive, AssetStatusInactive, AssetStatusRetired}
	assetCriticalityKeys   = []string{AssetCriticalityLow, AssetCriticalityMedium, AssetCriticalityHigh, AssetCriticalityCritical}
	riskStatusKeys         = []string{RiskStatusDraft, RiskStatusOpen, RiskStatusInTreatment, RiskStatusAccepted, RiskStatusClosed}
	controlStatusKeys      = []string{ControlStatusDraft, ControlStatusActive, ControlStatusInactive, ControlStatusRetired}
	implementationStatuses = []string{
		ControlImplementationStatusPlanned, ControlImplementationStatusImplemented, ControlImplementationStatusPartial,
		ControlImplementationStatusNotImplemented, ControlImplementationStatusNotApplicable,
	}
	assessmentResultKeys = []string{
		AssessmentResultEffective, AssessmentResultPartiallyEffective, AssessmentResultIneffective,
		AssessmentResultNotApplicable, dashboardNotAssessed,
	}
	frameworkStatusKeys = []string{FrameworkStatusDraft, FrameworkStatusActive, FrameworkStatusArchived}
	evidenceStatusKeys  = []string{EvidenceStatusDraft, EvidenceStatusValid, EvidenceStatusRevoked}
	actionStatusKeys    = []string{
		RemediationActionStatusOpen, RemediationActionStatusInProgress, RemediationActionStatusBlocked,
		RemediationActionStatusDone, RemediationActionStatusCancelled,
	}
	actionPriorityKeys     = []string{RemediationActionPriorityLow, RemediationActionPriorityMedium, RemediationActionPriorityHigh, RemediationActionPriorityCritical}
	assessmentStatusKeys   = []string{AssessmentStatusDraft, AssessmentStatusCompleted, AssessmentStatusCancelled}
	auditStatusKeys        = []string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress, AuditEngagementStatusCompleted, AuditEngagementStatusCancelled}
	findingStatusKeys      = []string{FindingStatusOpen, FindingStatusInRemediation, FindingStatusResolved, FindingStatusClosed, FindingStatusAccepted}
	findingSeverityKeys    = []string{FindingSeverityLow, FindingSeverityMedium, FindingSeverityHigh, FindingSeverityCritical}
	openRiskStatuses       = []string{RiskStatusOpen, RiskStatusInTreatment}
	terminalActionStatuses = []string{RemediationActionStatusDone, RemediationActionStatusCancelled}
)

// dashboardNotAssessed buckets control implementations without any completed
// assessment. It is a dashboard bucket, not an assessment result.
const dashboardNotAssessed = "NOT_ASSESSED"

func validateDashboardWindow(days int, name string) error {
	if days < DashboardMinWindowDays || days > DashboardMaxWindowDays {
		return coreerrors.BadRequest(name + " doit être compris entre 1 et 365")
	}
	return nil
}

// dashboardRatio returns a percentage rounded to two decimals; 0/0 is 0.
func dashboardRatio(numerator, denominator int64) DashboardRatio {
	ratio := DashboardRatio{Numerator: numerator, Denominator: denominator}
	if denominator > 0 {
		ratio.Percent = math.Round(float64(numerator)*10000/float64(denominator)) / 100
	}
	return ratio
}

func distribution(keys []string, counts map[string]int64) []DashboardCount {
	out := make([]DashboardCount, 0, len(keys))
	for _, key := range keys {
		out = append(out, DashboardCount{Key: key, Count: counts[key]})
	}
	return out
}

func countOf(buckets []DashboardCount, keys ...string) int64 {
	var total int64
	for _, bucket := range buckets {
		for _, key := range keys {
			if bucket.Key == key {
				total += bucket.Count
			}
		}
	}
	return total
}

// countByColumn groups one table by an enum column; the total also counts
// values outside keys (none exist while the CHECK constraints hold).
func countByColumn(q *gorm.DB, column string, keys []string) ([]DashboardCount, int64, error) {
	var rows []struct {
		Bucket string
		Total  int64
	}
	if err := q.Select(column + " AS bucket, COUNT(*) AS total").Group(column).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	counts := map[string]int64{}
	var total int64
	for _, row := range rows {
		counts[row.Bucket] += row.Total
		total += row.Total
	}
	return distribution(keys, counts), total, nil
}

func countWhere(q *gorm.DB, query string, args ...any) (int64, error) {
	var total int64
	err := q.Where(query, args...).Count(&total).Error
	return total, err
}

// riskLevelDistribution groups open risks by score (at most 25 groups) and
// classifies each score with RiskLevel, the single source of the thresholds.
func riskLevelDistribution(db *gorm.DB, scoreColumn string) ([]DashboardCount, error) {
	var rows []struct {
		Score int
		Total int64
	}
	if err := db.Model(&Risk{}).
		Select(scoreColumn+" AS score, COUNT(*) AS total").
		Where("status IN ?", openRiskStatuses).
		Group(scoreColumn).
		Scan(&rows).
		Error; err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		level, err := RiskLevel(row.Score)
		if err != nil {
			return nil, err
		}
		counts[level] += row.Total
	}
	return distribution(riskLevels, counts), nil
}

func (s *GRCService) Dashboard(f DashboardFilter) (*GRCDashboard, error) {
	if err := validateDashboardWindow(f.ExpiringSoonDays, "expiringSoonDays"); err != nil {
		return nil, err
	}
	if err := validateDashboardWindow(f.DueSoonDays, "dueSoonDays"); err != nil {
		return nil, err
	}

	now := s.currentTime().UTC()
	expiringLimit := now.AddDate(0, 0, f.ExpiringSoonDays)
	dueLimit := now.AddDate(0, 0, f.DueSoonDays)
	d := &GRCDashboard{
		GeneratedAt: now,
		Windows:     DashboardWindows{ExpiringSoonDays: f.ExpiringSoonDays, DueSoonDays: f.DueSoonDays},
	}

	steps := []func() error{
		func() error { return s.dashboardAssets(&d.Assets) },
		func() error { return s.dashboardRisks(&d.Risks) },
		func() error { return s.dashboardControls(&d.Controls) },
		func() error { return s.dashboardCompliance(&d.Compliance) },
		func() error { return s.dashboardEvidence(&d.Evidence, now, expiringLimit) },
		func() error { return s.dashboardActions(&d.Actions, now, dueLimit) },
		func() error { return s.dashboardAssessments(&d.Assessments, now) },
		func() error { return s.dashboardAudits(&d.Audits) },
		func() error { return s.dashboardFindings(&d.Findings, now) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}

	d.Attention = DashboardAttention{
		CriticalResidualRisks: d.Risks.Critical,
		HighResidualRisks:     d.Risks.High,
		ExpiredEvidence:       d.Evidence.Expired,
		EvidenceExpiringSoon:  d.Evidence.ExpiringSoon,
		OverdueActions:        d.Actions.Overdue,
		ActionsDueSoon:        d.Actions.DueSoon,
		ReassessmentsDue:      d.Assessments.ReassessmentDue,
		OverdueFindings:       d.Findings.Overdue,
	}
	d.Summary = DashboardSummary{
		Assets:                    d.Assets.Total,
		OpenRisks:                 d.Risks.Open,
		CriticalOrHighRisks:       d.Risks.Critical + d.Risks.High,
		Controls:                  d.Controls.Total,
		EvidenceIssues:            d.Evidence.Invalid,
		OpenRemediationActions:    d.Actions.Open,
		OpenFindings:              d.Findings.Open,
		OverdueItems:              d.Actions.Overdue + d.Findings.Overdue,
		ControlMappingCoveragePct: d.Compliance.ControlMappingCoverage.Percent,
	}
	return d, nil
}

func (s *GRCService) dashboardAssets(a *DashboardAssets) error {
	var err error
	if a.ByStatus, a.Total, err = countByColumn(s.db.Model(&Asset{}), "status", assetStatusKeys); err != nil {
		return err
	}
	if a.ByCriticality, _, err = countByColumn(s.db.Model(&Asset{}), "criticality", assetCriticalityKeys); err != nil {
		return err
	}
	a.Active = countOf(a.ByStatus, AssetStatusActive)
	a.Critical = countOf(a.ByCriticality, AssetCriticalityCritical)
	return nil
}

func (s *GRCService) dashboardRisks(r *DashboardRisks) error {
	var err error
	if r.ByStatus, r.Total, err = countByColumn(s.db.Model(&Risk{}), "status", riskStatusKeys); err != nil {
		return err
	}
	if r.ResidualByLevel, err = riskLevelDistribution(s.db, "residual_score"); err != nil {
		return err
	}
	if r.InherentByLevel, err = riskLevelDistribution(s.db, "inherent_score"); err != nil {
		return err
	}
	r.Open = countOf(r.ByStatus, openRiskStatuses...)
	r.Critical = countOf(r.ResidualByLevel, RiskLevelCritical)
	r.High = countOf(r.ResidualByLevel, RiskLevelHigh)
	return nil
}

func (s *GRCService) dashboardControls(c *DashboardControls) error {
	var err error
	if c.ByStatus, c.Total, err = countByColumn(s.db.Model(&Control{}), "status", controlStatusKeys); err != nil {
		return err
	}
	if c.ImplementationsByStatus, c.Implementations, err = countByColumn(
		s.db.Model(&ControlImplementation{}), "status", implementationStatuses); err != nil {
		return err
	}

	// Latest completed assessment per implementation (completed_at, then id).
	var rows []struct {
		Result *string
		Total  int64
	}
	if err := s.db.Raw(`SELECT a.result AS result, COUNT(*) AS total
		FROM grc_control_implementations ci
		LEFT JOIN grc_assessments a ON a.id = (
			SELECT a2.id FROM grc_assessments a2
			WHERE a2.control_implementation_id = ci.id AND a2.status = ?
			ORDER BY a2.completed_at DESC, a2.id DESC
			LIMIT 1)
		GROUP BY a.result`, AssessmentStatusCompleted).Scan(&rows).Error; err != nil {
		return err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		key := dashboardNotAssessed
		if row.Result != nil {
			key = *row.Result
		}
		counts[key] += row.Total
	}
	c.ImplementationsByLatestAssessmentResult = distribution(assessmentResultKeys, counts)
	return nil
}

func (s *GRCService) dashboardCompliance(c *DashboardCompliance) error {
	var err error
	if c.FrameworksByStatus, c.Frameworks, err = countByColumn(s.db.Model(&Framework{}), "status", frameworkStatusKeys); err != nil {
		return err
	}
	// Same definition as the compliance pages (compliance_coverage.go).
	coverage, err := s.overallCoverage()
	if err != nil {
		return err
	}
	c.Requirements = coverage.Requirements
	c.ApplicableRequirements = coverage.ApplicableRequirements
	c.MappedRequirements = coverage.MappedRequirements
	c.UnmappedRequirements = coverage.UnmappedRequirements
	c.ControlMappingCoverage = coverage.ControlMappingCoverage
	return nil
}

func (s *GRCService) dashboardEvidence(e *DashboardEvidence, now, expiringLimit time.Time) error {
	var err error
	if e.ByStatus, e.Total, err = countByColumn(s.db.Model(&Evidence{}), "status", evidenceStatusKeys); err != nil {
		return err
	}
	if e.Expired, err = countWhere(s.db.Model(&Evidence{}), evidenceExpiredSQL, EvidenceStatusValid, now); err != nil {
		return err
	}
	// Shared validity definition (evidence_validity.go).
	if e.ExpiringSoon, err = countWhere(s.db.Model(&Evidence{}), evidenceExpiringSoonSQL,
		EvidenceStatusValid, now, expiringLimit); err != nil {
		return err
	}
	e.Valid = countOf(e.ByStatus, EvidenceStatusValid) - e.Expired
	// Same definition as Assessment.HasInvalidEvidence: not VALID, or expired.
	e.Invalid = e.Total - e.Valid
	return nil
}

func (s *GRCService) dashboardActions(a *DashboardActions, now, dueLimit time.Time) error {
	var err error
	if a.ByStatus, a.Total, err = countByColumn(s.db.Model(&RemediationAction{}), "status", actionStatusKeys); err != nil {
		return err
	}
	if a.ByPriority, _, err = countByColumn(s.db.Model(&RemediationAction{}), "priority", actionPriorityKeys); err != nil {
		return err
	}
	if a.Overdue, err = countWhere(s.db.Model(&RemediationAction{}), remediationActionOverdueSQL,
		now, RemediationActionStatusDone, RemediationActionStatusCancelled); err != nil {
		return err
	}
	// Shared due-state definition (remediation_action_due.go).
	if a.DueSoon, err = countWhere(s.db.Model(&RemediationAction{}), remediationActionDueSoonSQL,
		now, dueLimit, terminalActionStatuses); err != nil {
		return err
	}
	a.Open = a.Total - countOf(a.ByStatus, terminalActionStatuses...)
	return nil
}

func (s *GRCService) dashboardAssessments(a *DashboardAssessments, now time.Time) error {
	var err error
	if a.ByStatus, a.Total, err = countByColumn(s.db.Model(&Assessment{}), "status", assessmentStatusKeys); err != nil {
		return err
	}
	if a.ReassessmentDue, err = countWhere(s.db.Model(&Assessment{}), assessmentReassessmentDueSQL,
		AssessmentStatusCompleted, now); err != nil {
		return err
	}
	a.HasInvalidEvidence, err = countWhere(s.db.Model(&Assessment{}), assessmentHasInvalidEvidenceSQL, EvidenceStatusValid, now)
	return err
}

func (s *GRCService) dashboardAudits(a *DashboardAudits) error {
	var err error
	a.ByStatus, a.Total, err = countByColumn(s.db.Model(&AuditEngagement{}), "status", auditStatusKeys)
	return err
}

func (s *GRCService) dashboardFindings(f *DashboardFindings, now time.Time) error {
	var err error
	if f.ByStatus, f.Total, err = countByColumn(s.db.Model(&Finding{}), "status", findingStatusKeys); err != nil {
		return err
	}
	if f.BySeverity, _, err = countByColumn(s.db.Model(&Finding{}), "severity", findingSeverityKeys); err != nil {
		return err
	}
	if f.Overdue, err = countWhere(s.db.Model(&Finding{}), findingOverdueSQL,
		now, FindingStatusOpen, FindingStatusInRemediation); err != nil {
		return err
	}
	// Shared finding semantics (audit_engagement_registry.go).
	f.Open = f.Total - countOf(f.ByStatus, findingTerminalStatuses...)
	return nil
}
