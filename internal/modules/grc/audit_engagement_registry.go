package grc

import "time"

// Finding semantics shared by the audit registry counters and the GRC
// dashboard: a finding is open unless CLOSED or ACCEPTED, and overdue as
// defined by FindingOverdue / findingOverdueSQL.
var findingTerminalStatuses = []string{FindingStatusClosed, FindingStatusAccepted}

// Lengths mirror the grc_audit_engagements and grc_findings columns.
const (
	auditEngagementReferenceMaxLength = 60
	auditEngagementTitleMaxLength     = 220
	findingReferenceMaxLength         = 60
	findingTitleMaxLength             = 220
)

// fillAuditFindingCounts sets the finding counters of the given audits:
// one grouped query over grc_findings.audit_engagement_id, at one instant.
func (s *GRCService) fillAuditFindingCounts(audits []AuditEngagement, now time.Time) error {
	if len(audits) == 0 {
		return nil
	}
	ids := make([]uint, len(audits))
	for i := range audits {
		ids[i] = audits[i].ID
	}
	var rows []struct {
		AuditEngagementID uint
		Total             int64
		Open              int64
		Overdue           int64
	}
	if err := s.db.Model(&Finding{}).
		Select(`audit_engagement_id,
			COUNT(*) AS total,
			COALESCE(SUM(CASE WHEN status NOT IN ? THEN 1 ELSE 0 END), 0) AS open,
			COALESCE(SUM(CASE WHEN `+findingOverdueSQL+` THEN 1 ELSE 0 END), 0) AS overdue`,
			findingTerminalStatuses, now, FindingStatusOpen, FindingStatusInRemediation).
		Where("audit_engagement_id IN ?", ids).
		Group("audit_engagement_id").
		Scan(&rows).Error; err != nil {
		return err
	}
	byAudit := make(map[uint]int, len(rows))
	for i, row := range rows {
		byAudit[row.AuditEngagementID] = i
	}
	for i := range audits {
		if j, ok := byAudit[audits[i].ID]; ok {
			audits[i].FindingCount = rows[j].Total
			audits[i].OpenFindingCount = rows[j].Open
			audits[i].OverdueFindingCount = rows[j].Overdue
		}
	}
	return nil
}
