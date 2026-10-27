package grc

import (
	"time"

	"gorm.io/gorm"
)

var (
	riskTreatmentKeys = []string{RiskTreatmentReduce, RiskTreatmentAccept, RiskTreatmentAvoid, RiskTreatmentTransfer}
	evidenceTypeKeys  = []string{
		EvidenceTypeDocument, EvidenceTypeConfiguration, EvidenceTypeLog, EvidenceTypeScreenshot,
		EvidenceTypeReport, EvidenceTypeTestResult, EvidenceTypeCertificate, EvidenceTypeOther,
	}
	auditTypeKeys      = []string{AuditEngagementTypeInternal, AuditEngagementTypeExternal, AuditEngagementTypeVendor}
	incidentStatusKeys = []string{
		SecurityIncidentStatusOpen, SecurityIncidentStatusInvestigating, SecurityIncidentStatusContained,
		SecurityIncidentStatusResolved, SecurityIncidentStatusClosed,
	}
	incidentSeverityKeys = []string{
		SecurityIncidentSeverityLow, SecurityIncidentSeverityMedium, SecurityIncidentSeverityHigh, SecurityIncidentSeverityCritical,
	}
	policyStatusKeys = []string{
		GovernancePolicyStatusDraft, GovernancePolicyStatusInReview, GovernancePolicyStatusApproved,
		GovernancePolicyStatusPublished, GovernancePolicyStatusRetired,
	}
	auditActiveStatuses      = []string{AuditEngagementStatusPlanned, AuditEngagementStatusInProgress}
	openAssetCriticalityKeys = append(append([]string{}, assetCriticalityKeys...), reportingAssetCriticalityNone)
)

// Reporting aggregates the current GRC registers at one instant.
// reportingAggregateQueries statements, all grouped; no per-row query.
func (s *GRCService) Reporting(f ReportingFilter) (*GRCReporting, error) {
	if err := validateDashboardWindow(f.EvidenceExpiringSoonDays, "evidenceExpiringSoonDays"); err != nil {
		return nil, err
	}
	if err := validateDashboardWindow(f.ActionDueSoonDays, "actionDueSoonDays"); err != nil {
		return nil, err
	}

	now := s.currentTime().UTC()
	evidenceLimit := now.AddDate(0, 0, f.EvidenceExpiringSoonDays)
	actionLimit := now.AddDate(0, 0, f.ActionDueSoonDays)
	scopeIDs := f.FrameworkIDs
	if scopeIDs == nil {
		scopeIDs = []uint{}
	}
	out := &GRCReporting{
		GeneratedAt: now,
		Windows: ReportingWindows{
			EvidenceExpiringSoonDays: f.EvidenceExpiringSoonDays,
			ActionDueSoonDays:        f.ActionDueSoonDays,
		},
		FrameworkScope: ReportingFrameworkScope{
			AppliesTo:    ReportingFrameworkScopeCompliance,
			FrameworkIDs: scopeIDs,
		},
	}

	var err error
	if out.Executive.AssetsTotal, err = countAll(s.db.Model(&Asset{})); err != nil {
		return nil, err
	}
	if err = s.reportingRisks(&out.Risks); err != nil {
		return nil, err
	}
	if err = s.reportingCompliance(&out.Compliance, f.FrameworkIDs); err != nil {
		return nil, err
	}
	if err = s.reportingControls(&out.Controls); err != nil {
		return nil, err
	}
	if err = s.reportingEvidence(&out.Evidence, now, evidenceLimit); err != nil {
		return nil, err
	}
	if err = s.reportingActions(&out.Actions, now, actionLimit); err != nil {
		return nil, err
	}
	if err = s.reportingAudits(&out.Audits); err != nil {
		return nil, err
	}
	if err = s.reportingFindings(&out.Findings, now); err != nil {
		return nil, err
	}
	if err = s.reportingIncidents(&out.Incidents); err != nil {
		return nil, err
	}
	if err = s.reportingPolicies(&out.Policies); err != nil {
		return nil, err
	}

	out.Executive.OpenRisks = countOf(out.Risks.ByStatus, openRiskStatuses...)
	out.Executive.HighCriticalOpenRisks = out.Risks.HighCriticalOpenRisks
	out.Executive.ActiveFrameworks = out.Compliance.ActiveFrameworks
	out.Executive.ApplicableRequirements = out.Compliance.ApplicableRequirements
	out.Executive.MappedRequirements = out.Compliance.MappedRequirements
	out.Executive.MappingCoverage = out.Compliance.MappingCoverage
	out.Executive.ControlsTotal = out.Controls.Total
	out.Executive.ImplementationsTotal = out.Controls.ImplementationsTotal
	out.Executive.EvidenceExpired = out.Evidence.Expired
	out.Executive.EvidenceExpiringSoon = out.Evidence.ExpiringSoon
	out.Executive.EvidenceInvalid = out.Evidence.Invalid
	out.Executive.OpenActions = out.Actions.Open
	out.Executive.OverdueActions = out.Actions.Overdue
	out.Executive.DueSoonActions = out.Actions.DueSoon
	out.Executive.ActiveAudits = out.Audits.Active
	out.Executive.OpenFindings = out.Findings.Open
	out.Executive.OverdueFindings = out.Findings.Overdue
	out.Executive.ActiveIncidents = out.Incidents.Active
	out.Executive.PublishedPolicies = out.Policies.Published
	out.Executive.PoliciesInReview = out.Policies.InReview
	return out, nil
}

func countAll(q *gorm.DB) (int64, error) {
	var total int64
	err := q.Count(&total).Error
	return total, err
}

func (s *GRCService) reportingRisks(r *ReportingRisks) error {
	var err error
	if r.ByStatus, _, err = countByColumn(s.db.Model(&Risk{}), "status", riskStatusKeys); err != nil {
		return err
	}
	if r.ByTreatment, _, err = countByColumn(s.db.Model(&Risk{}), "treatment", riskTreatmentKeys); err != nil {
		return err
	}
	if r.OpenByResidualLevel, err = riskLevelDistribution(s.db, "residual_score"); err != nil {
		return err
	}
	if r.OpenByInherentLevel, err = riskLevelDistribution(s.db, "inherent_score"); err != nil {
		return err
	}
	r.HighCriticalOpenRisks = countOf(r.OpenByResidualLevel, RiskLevelHigh, RiskLevelCritical)
	if r.OpenByOwningService, err = serviceCounts(s.db.Model(&Risk{}).Where("grc_risks.status IN ?", openRiskStatuses), "grc_risks.owning_service_id"); err != nil {
		return err
	}
	r.OpenByAssetCriticality, err = openRisksByAssetCriticality(s.db)
	return err
}

func (s *GRCService) reportingCompliance(c *ReportingCompliance, frameworkIDs []uint) error {
	c.ByFramework = []ReportingFrameworkCoverage{}
	var rows []struct {
		ID         uint
		Code       string
		Name       string
		Status     string
		ActiveReqs int64
		Applicable int64
		Mapped     int64
	}
	q := s.db.Table("grc_frameworks AS f").
		Select(`f.id AS id, f.code AS code, f.name AS name, f.status AS status,
			COALESCE(SUM(CASE WHEN r.status = ? THEN 1 ELSE 0 END), 0) AS active_reqs,
			COALESCE(SUM(CASE WHEN r.status = ? AND f.status = ? THEN 1 ELSE 0 END), 0) AS applicable,
			COALESCE(SUM(CASE WHEN r.status = ? AND f.status = ? AND EXISTS (
				SELECT 1 FROM grc_requirement_controls rc WHERE rc.requirement_id = r.id
			) THEN 1 ELSE 0 END), 0) AS mapped`,
			RequirementStatusActive,
			RequirementStatusActive, FrameworkStatusActive,
			RequirementStatusActive, FrameworkStatusActive).
		Joins("LEFT JOIN grc_requirements r ON r.framework_id = f.id").
		Group("f.id, f.code, f.name, f.status").
		Order("f.code ASC, f.id ASC")
	if frameworkIDs != nil {
		q = q.Where("f.id IN ?", frameworkIDs)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if row.Status == FrameworkStatusActive {
			c.ActiveFrameworks++
		}
		c.ActiveRequirements += row.ActiveReqs
		c.ApplicableRequirements += row.Applicable
		c.MappedRequirements += row.Mapped
		c.ByFramework = append(c.ByFramework, ReportingFrameworkCoverage{
			FrameworkID:            row.ID,
			Code:                   row.Code,
			Name:                   row.Name,
			Status:                 row.Status,
			ActiveRequirements:     row.ActiveReqs,
			ApplicableRequirements: row.Applicable,
			MappedRequirements:     row.Mapped,
			MappingCoverage:        dashboardRatio(row.Mapped, row.Applicable),
		})
	}
	c.MappingCoverage = dashboardRatio(c.MappedRequirements, c.ApplicableRequirements)
	return nil
}

func (s *GRCService) reportingControls(c *ReportingControls) error {
	var err error
	if c.ByStatus, c.Total, err = countByColumn(s.db.Model(&Control{}), "status", controlStatusKeys); err != nil {
		return err
	}
	c.ImplementationsByStatus, c.ImplementationsTotal, err = countByColumn(
		s.db.Model(&ControlImplementation{}), "status", implementationStatuses)
	return err
}

func (s *GRCService) reportingEvidence(e *ReportingEvidence, now, soonLimit time.Time) error {
	var row struct {
		Valid        int64
		ExpiringSoon int64
		Expired      int64
		Invalid      int64
	}
	// Same boundaries as EvidenceValidity: expires_at == now is not expired.
	if err := s.db.Model(&Evidence{}).Select(`
		COALESCE(SUM(CASE WHEN status = ? AND (expires_at IS NULL OR expires_at > ?) THEN 1 ELSE 0 END), 0) AS valid,
		COALESCE(SUM(CASE WHEN status = ? AND expires_at IS NOT NULL AND expires_at >= ? AND expires_at <= ? THEN 1 ELSE 0 END), 0) AS expiring_soon,
		COALESCE(SUM(CASE WHEN status = ? AND expires_at IS NOT NULL AND expires_at < ? THEN 1 ELSE 0 END), 0) AS expired,
		COALESCE(SUM(CASE WHEN status <> ? THEN 1 ELSE 0 END), 0) AS invalid`,
		EvidenceStatusValid, soonLimit,
		EvidenceStatusValid, now, soonLimit,
		EvidenceStatusValid, now,
		EvidenceStatusValid,
	).Scan(&row).Error; err != nil {
		return err
	}
	e.Valid = row.Valid
	e.ExpiringSoon = row.ExpiringSoon
	e.Expired = row.Expired
	e.Invalid = row.Invalid
	e.ByValidity = distribution(evidenceValidityStates, map[string]int64{
		EvidenceValidityValid:        row.Valid,
		EvidenceValidityExpiringSoon: row.ExpiringSoon,
		EvidenceValidityExpired:      row.Expired,
		EvidenceValidityInvalid:      row.Invalid,
	})
	var err error
	e.ByType, _, err = countByColumn(s.db.Model(&Evidence{}), "type", evidenceTypeKeys)
	return err
}

func (s *GRCService) reportingActions(a *ReportingActions, now, soonLimit time.Time) error {
	var err error
	if a.ByStatus, _, err = countByColumn(s.db.Model(&RemediationAction{}), "status", actionStatusKeys); err != nil {
		return err
	}
	openPriority := s.db.Model(&RemediationAction{}).Where("status NOT IN ?", terminalActionStatuses)
	if a.OpenByPriority, _, err = countByColumn(openPriority, "priority", actionPriorityKeys); err != nil {
		return err
	}
	var row struct {
		Closed    int64
		NoDueDate int64
		Overdue   int64
		DueSoon   int64
		Upcoming  int64
	}
	// Same boundaries as RemediationActionDueState: due_at == now is not overdue.
	if err = s.db.Model(&RemediationAction{}).Select(`
		COALESCE(SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END), 0) AS closed,
		COALESCE(SUM(CASE WHEN due_at IS NULL AND status NOT IN (?, ?) THEN 1 ELSE 0 END), 0) AS no_due_date,
		COALESCE(SUM(CASE WHEN due_at IS NOT NULL AND due_at < ? AND status NOT IN (?, ?) THEN 1 ELSE 0 END), 0) AS overdue,
		COALESCE(SUM(CASE WHEN due_at IS NOT NULL AND due_at >= ? AND due_at <= ? AND status NOT IN (?, ?) THEN 1 ELSE 0 END), 0) AS due_soon,
		COALESCE(SUM(CASE WHEN due_at IS NOT NULL AND due_at > ? AND status NOT IN (?, ?) THEN 1 ELSE 0 END), 0) AS upcoming`,
		RemediationActionStatusDone, RemediationActionStatusCancelled,
		RemediationActionStatusDone, RemediationActionStatusCancelled,
		now, RemediationActionStatusDone, RemediationActionStatusCancelled,
		now, soonLimit, RemediationActionStatusDone, RemediationActionStatusCancelled,
		soonLimit, RemediationActionStatusDone, RemediationActionStatusCancelled,
	).Scan(&row).Error; err != nil {
		return err
	}
	a.ByDueState = distribution(remediationActionDueStates, map[string]int64{
		RemediationActionDueOverdue:   row.Overdue,
		RemediationActionDueSoon:      row.DueSoon,
		RemediationActionDueUpcoming:  row.Upcoming,
		RemediationActionDueNoDueDate: row.NoDueDate,
		RemediationActionDueClosed:    row.Closed,
	})
	a.Open = a.ByStatusTotalOpen()
	a.Overdue = row.Overdue
	a.DueSoon = row.DueSoon
	return nil
}

func (a *ReportingActions) ByStatusTotalOpen() int64 {
	return countOf(a.ByStatus, RemediationActionStatusOpen, RemediationActionStatusInProgress, RemediationActionStatusBlocked)
}

func (s *GRCService) reportingAudits(a *ReportingAudits) error {
	var total int64
	var err error
	if a.ByStatus, total, err = countByColumn(s.db.Model(&AuditEngagement{}), "status", auditStatusKeys); err != nil {
		return err
	}
	_ = total
	if a.ByType, _, err = countByColumn(s.db.Model(&AuditEngagement{}), "type", auditTypeKeys); err != nil {
		return err
	}
	a.Active = countOf(a.ByStatus, auditActiveStatuses...)
	a.Completed = countOf(a.ByStatus, AuditEngagementStatusCompleted)
	return nil
}

func (s *GRCService) reportingFindings(f *ReportingFindings, now time.Time) error {
	var err error
	var total int64
	if f.ByStatus, total, err = countByColumn(s.db.Model(&Finding{}), "status", findingStatusKeys); err != nil {
		return err
	}
	if f.BySeverity, _, err = countByColumn(s.db.Model(&Finding{}), "severity", findingSeverityKeys); err != nil {
		return err
	}
	if f.Overdue, err = countWhere(s.db.Model(&Finding{}), findingOverdueSQL,
		now, FindingStatusOpen, FindingStatusInRemediation); err != nil {
		return err
	}
	f.Open = total - countOf(f.ByStatus, findingTerminalStatuses...)
	return nil
}

func (s *GRCService) reportingIncidents(i *ReportingIncidents) error {
	var total int64
	var err error
	if i.ByStatus, total, err = countByColumn(s.db.Model(&SecurityIncident{}), "status", incidentStatusKeys); err != nil {
		return err
	}
	if i.BySeverity, _, err = countByColumn(s.db.Model(&SecurityIncident{}), "severity", incidentSeverityKeys); err != nil {
		return err
	}
	i.Active = total - countOf(i.ByStatus, SecurityIncidentStatusClosed)
	return nil
}

func (s *GRCService) reportingPolicies(p *ReportingPolicies) error {
	var err error
	if p.ByStatus, _, err = countByColumn(s.db.Model(&GovernancePolicy{}), "status", policyStatusKeys); err != nil {
		return err
	}
	if p.ByOwningService, err = serviceCounts(s.db.Model(&GovernancePolicy{}), "grc_governance_policies.owning_service_id"); err != nil {
		return err
	}
	p.Published = countOf(p.ByStatus, GovernancePolicyStatusPublished)
	p.InReview = countOf(p.ByStatus, GovernancePolicyStatusInReview)
	return nil
}

// column is a fixed GRC column name, never a client value.
func serviceCounts(q *gorm.DB, column string) ([]ReportingServiceCount, error) {
	var rows []struct {
		ServiceID *uint
		Code      string
		Name      string
		Total     int64
	}
	err := q.Select(column + ` AS service_id, COALESCE(svc.code, '') AS code, COALESCE(svc.name, '') AS name, COUNT(*) AS total`).
		Joins("LEFT JOIN organization_services svc ON svc.id = " + column).
		Group(column + ", svc.code, svc.name").
		Order("svc.code ASC, " + column + " ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ReportingServiceCount, 0, len(rows))
	for _, row := range rows {
		item := ReportingServiceCount{Code: row.Code, Name: row.Name, Count: row.Total}
		if row.ServiceID != nil && *row.ServiceID != 0 {
			id := *row.ServiceID
			item.ServiceID = &id
		}
		out = append(out, item)
	}
	return out, nil
}

func openRisksByAssetCriticality(db *gorm.DB) ([]DashboardCount, error) {
	var rows []struct {
		Bucket string
		Total  int64
	}
	err := db.Model(&Risk{}).
		Select("COALESCE(a.criticality, 'NONE') AS bucket, COUNT(*) AS total").
		Joins("LEFT JOIN grc_assets a ON a.id = grc_risks.asset_id").
		Where("grc_risks.status IN ?", openRiskStatuses).
		Group("COALESCE(a.criticality, 'NONE')").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, row := range rows {
		counts[row.Bucket] += row.Total
	}
	return distribution(openAssetCriticalityKeys, counts), nil
}
