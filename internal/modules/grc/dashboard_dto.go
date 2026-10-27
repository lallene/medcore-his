package grc

import "time"

const (
	DashboardDefaultWindowDays = 30
	DashboardMinWindowDays     = 1
	DashboardMaxWindowDays     = 365
)

// DashboardFilter holds the "soon" windows, in days.
type DashboardFilter struct {
	ExpiringSoonDays int
	DueSoonDays      int
}

// DashboardCount is one bucket of a distribution. Distributions list every
// known key in a stable order, zero counts included.
type DashboardCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// DashboardRatio is a percentage with its explicit numerator and
// denominator; Percent is 0 when the denominator is 0.
type DashboardRatio struct {
	Numerator   int64   `json:"numerator"`
	Denominator int64   `json:"denominator"`
	Percent     float64 `json:"percent"`
}

type DashboardWindows struct {
	ExpiringSoonDays int `json:"expiringSoonDays"`
	DueSoonDays      int `json:"dueSoonDays"`
}

type DashboardSummary struct {
	Assets                    int64   `json:"assets"`
	OpenRisks                 int64   `json:"openRisks"`
	CriticalOrHighRisks       int64   `json:"criticalOrHighRisks"`
	Controls                  int64   `json:"controls"`
	EvidenceIssues            int64   `json:"evidenceIssues"`
	OpenRemediationActions    int64   `json:"openRemediationActions"`
	OpenFindings              int64   `json:"openFindings"`
	OverdueItems              int64   `json:"overdueItems"`
	ControlMappingCoveragePct float64 `json:"controlMappingCoveragePercent"`
}

type DashboardAssets struct {
	Total         int64            `json:"total"`
	Active        int64            `json:"active"`
	Critical      int64            `json:"critical"`
	ByStatus      []DashboardCount `json:"byStatus"`
	ByCriticality []DashboardCount `json:"byCriticality"`
}

// DashboardRisks: open risks are OPEN or IN_TREATMENT; level distributions
// cover open risks only, with the residual score as the reference.
type DashboardRisks struct {
	Total           int64            `json:"total"`
	Open            int64            `json:"open"`
	Critical        int64            `json:"critical"`
	High            int64            `json:"high"`
	ByStatus        []DashboardCount `json:"byStatus"`
	ResidualByLevel []DashboardCount `json:"residualByLevel"`
	InherentByLevel []DashboardCount `json:"inherentByLevel"`
}

type DashboardControls struct {
	Total                                   int64            `json:"total"`
	ByStatus                                []DashboardCount `json:"byStatus"`
	Implementations                         int64            `json:"implementations"`
	ImplementationsByStatus                 []DashboardCount `json:"implementationsByStatus"`
	ImplementationsByLatestAssessmentResult []DashboardCount `json:"implementationsByLatestAssessmentResult"`
}

// DashboardCompliance: ControlMappingCoverage counts ACTIVE requirements of
// ACTIVE frameworks mapped to at least one control. It measures mapping, not
// compliance.
type DashboardCompliance struct {
	Frameworks             int64            `json:"frameworks"`
	FrameworksByStatus     []DashboardCount `json:"frameworksByStatus"`
	Requirements           int64            `json:"requirements"`
	ApplicableRequirements int64            `json:"applicableRequirements"`
	MappedRequirements     int64            `json:"mappedRequirements"`
	UnmappedRequirements   int64            `json:"unmappedRequirements"`
	ControlMappingCoverage DashboardRatio   `json:"controlMappingCoverage"`
}

type DashboardEvidence struct {
	Total        int64            `json:"total"`
	ByStatus     []DashboardCount `json:"byStatus"`
	Valid        int64            `json:"valid"`
	Expired      int64            `json:"expired"`
	ExpiringSoon int64            `json:"expiringSoon"`
	Invalid      int64            `json:"invalid"`
}

type DashboardActions struct {
	Total      int64            `json:"total"`
	ByStatus   []DashboardCount `json:"byStatus"`
	ByPriority []DashboardCount `json:"byPriority"`
	Open       int64            `json:"open"`
	Overdue    int64            `json:"overdue"`
	DueSoon    int64            `json:"dueSoon"`
}

type DashboardAssessments struct {
	Total              int64            `json:"total"`
	ByStatus           []DashboardCount `json:"byStatus"`
	ReassessmentDue    int64            `json:"reassessmentDue"`
	HasInvalidEvidence int64            `json:"hasInvalidEvidence"`
}

type DashboardAudits struct {
	Total    int64            `json:"total"`
	ByStatus []DashboardCount `json:"byStatus"`
}

type DashboardFindings struct {
	Total      int64            `json:"total"`
	ByStatus   []DashboardCount `json:"byStatus"`
	BySeverity []DashboardCount `json:"bySeverity"`
	Open       int64            `json:"open"`
	Overdue    int64            `json:"overdue"`
}

type DashboardAttention struct {
	CriticalResidualRisks int64 `json:"criticalResidualRisks"`
	HighResidualRisks     int64 `json:"highResidualRisks"`
	ExpiredEvidence       int64 `json:"expiredEvidence"`
	EvidenceExpiringSoon  int64 `json:"evidenceExpiringSoon"`
	OverdueActions        int64 `json:"overdueActions"`
	ActionsDueSoon        int64 `json:"actionsDueSoon"`
	ReassessmentsDue      int64 `json:"reassessmentsDue"`
	OverdueFindings       int64 `json:"overdueFindings"`
}

type GRCDashboard struct {
	GeneratedAt time.Time            `json:"generatedAt"`
	Windows     DashboardWindows     `json:"windows"`
	Summary     DashboardSummary     `json:"summary"`
	Assets      DashboardAssets      `json:"assets"`
	Risks       DashboardRisks       `json:"risks"`
	Controls    DashboardControls    `json:"controls"`
	Compliance  DashboardCompliance  `json:"compliance"`
	Evidence    DashboardEvidence    `json:"evidence"`
	Actions     DashboardActions     `json:"actions"`
	Assessments DashboardAssessments `json:"assessments"`
	Audits      DashboardAudits      `json:"audits"`
	Findings    DashboardFindings    `json:"findings"`
	Attention   DashboardAttention   `json:"attention"`
}
