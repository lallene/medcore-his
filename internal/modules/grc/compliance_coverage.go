package grc

import "gorm.io/gorm"

// Control mapping coverage — the only definition, shared by the GRC
// dashboard and the compliance pages:
//   - applicable requirement: ACTIVE requirement of an ACTIVE framework;
//   - mapped requirement: applicable and linked to at least one control
//     through grc_requirement_controls (several links count once);
//   - coverage = mapped / applicable, 0 when there is no applicable
//     requirement (see dashboardRatio).
//
// It measures mapping, not compliance.
const requirementMappedSQL = "EXISTS (SELECT 1 FROM grc_requirement_controls rc WHERE rc.requirement_id = grc_requirements.id)"

// ComplianceCoverage gives the coverage figures of one framework, or of all
// frameworks when FrameworkID is nil.
type ComplianceCoverage struct {
	FrameworkID            *uint          `json:"frameworkId"`
	Requirements           int64          `json:"requirements"`
	ApplicableRequirements int64          `json:"applicableRequirements"`
	MappedRequirements     int64          `json:"mappedRequirements"`
	UnmappedRequirements   int64          `json:"unmappedRequirements"`
	ControlMappingCoverage DashboardRatio `json:"controlMappingCoverage"`
}

type ComplianceCoverageReport struct {
	Overall    ComplianceCoverage   `json:"overall"`
	Frameworks []ComplianceCoverage `json:"frameworks"`
}

// ComplianceCoverageMaxFrameworks bounds the per-framework breakdown of one
// request (one page of the framework registry).
const ComplianceCoverageMaxFrameworks = 100

func newComplianceCoverage(frameworkID *uint, requirements, applicable, mapped int64) ComplianceCoverage {
	return ComplianceCoverage{
		FrameworkID:            frameworkID,
		Requirements:           requirements,
		ApplicableRequirements: applicable,
		MappedRequirements:     mapped,
		UnmappedRequirements:   applicable - mapped,
		ControlMappingCoverage: dashboardRatio(mapped, applicable),
	}
}

// requirementCoverageRows counts, per framework, the requirements and the
// applicable and mapped ones in a single grouped query.
func (s *GRCService) requirementCoverageRows(frameworkIDs []uint) (map[uint]ComplianceCoverage, error) {
	var rows []struct {
		FrameworkID  uint
		Requirements int64
		Applicable   int64
		Mapped       int64
	}
	q := s.db.Model(&Requirement{}).
		Select(`grc_requirements.framework_id AS framework_id,
			COUNT(*) AS requirements,
			COALESCE(SUM(CASE WHEN grc_requirements.status = ? AND f.status = ? THEN 1 ELSE 0 END), 0) AS applicable,
			COALESCE(SUM(CASE WHEN grc_requirements.status = ? AND f.status = ? AND `+requirementMappedSQL+` THEN 1 ELSE 0 END), 0) AS mapped`,
			RequirementStatusActive, FrameworkStatusActive, RequirementStatusActive, FrameworkStatusActive).
		Joins("JOIN grc_frameworks f ON f.id = grc_requirements.framework_id").
		Group("grc_requirements.framework_id")
	if frameworkIDs != nil {
		q = q.Where("grc_requirements.framework_id IN ?", frameworkIDs)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]ComplianceCoverage, len(rows))
	for _, row := range rows {
		id := row.FrameworkID
		out[id] = newComplianceCoverage(&id, row.Requirements, row.Applicable, row.Mapped)
	}
	return out, nil
}

// overallCoverage sums the per-framework rows of every framework.
func (s *GRCService) overallCoverage() (ComplianceCoverage, error) {
	rows, err := s.requirementCoverageRows(nil)
	if err != nil {
		return ComplianceCoverage{}, err
	}
	var requirements, applicable, mapped int64
	for _, row := range rows {
		requirements += row.Requirements
		applicable += row.ApplicableRequirements
		mapped += row.MappedRequirements
	}
	return newComplianceCoverage(nil, requirements, applicable, mapped), nil
}

// ComplianceCoverageReport returns the overall coverage and, for each
// existing framework among frameworkIDs (in that order), its own coverage;
// a framework without requirement has zero figures.
func (s *GRCService) ComplianceCoverageReport(frameworkIDs []uint) (*ComplianceCoverageReport, error) {
	overall, err := s.overallCoverage()
	if err != nil {
		return nil, err
	}
	report := &ComplianceCoverageReport{Overall: overall, Frameworks: []ComplianceCoverage{}}
	if len(frameworkIDs) == 0 {
		return report, nil
	}

	var existing []uint
	if err := s.db.Model(&Framework{}).Where("id IN ?", frameworkIDs).Pluck("id", &existing).Error; err != nil {
		return nil, err
	}
	found := make(map[uint]bool, len(existing))
	for _, id := range existing {
		found[id] = true
	}
	rows, err := s.requirementCoverageRows(existing)
	if err != nil {
		return nil, err
	}
	seen := map[uint]bool{}
	for _, id := range frameworkIDs {
		if !found[id] || seen[id] {
			continue
		}
		seen[id] = true
		row, ok := rows[id]
		if !ok {
			frameworkID := id
			row = newComplianceCoverage(&frameworkID, 0, 0, 0)
		}
		report.Frameworks = append(report.Frameworks, row)
	}
	return report, nil
}

// requirementMappedControls counts the control links of each requirement of a
// page in one grouped query.
func (s *GRCService) requirementMappedControls(db *gorm.DB, requirementIDs []uint) (map[uint]int64, error) {
	out := map[uint]int64{}
	if len(requirementIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		RequirementID uint
		Total         int64
	}
	if err := db.Model(&RequirementControl{}).
		Select("requirement_id, COUNT(*) AS total").
		Where("requirement_id IN ?", requirementIDs).
		Group("requirement_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.RequirementID] = row.Total
	}
	return out, nil
}
