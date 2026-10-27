package grc

import "time"

// Reporting is a read-only analytical view over the current GRC tables.
// It does not persist a report, does not snapshot history and does not
// replace the operational dashboard.
//
// The framework filter applies only to the compliance section and to the
// four executive figures that are compliance figures (active frameworks,
// applicable requirements, mapped requirements, mapping coverage). Every
// other figure stays global: a framework has no defined relation to risks,
// assets, incidents, actions, audits, findings or policies in this read model.
//
// Historical trends and document exports are out of scope: the tables hold
// the current state, not a time series.

const (
	ReportingFrameworkScopeCompliance = "compliance"
	reportingAssetCriticalityNone     = "NONE"
	// reportingAggregateQueries is the number of SQL statements one
	// Reporting call executes. It does not grow with the number of rows.
	reportingAggregateQueries = 24
)

// ReportingFilter is the allowlist of client inputs. Windows use the same
// 1..365 bounds as the dashboard. FrameworkIDs nil means every framework;
// a non-nil slice restricts compliance only. Identifiers are bound, never
// concatenated into SQL.
type ReportingFilter struct {
	EvidenceExpiringSoonDays int
	ActionDueSoonDays        int
	FrameworkIDs             []uint
}

type ReportingWindows struct {
	EvidenceExpiringSoonDays int `json:"evidenceExpiringSoonDays"`
	ActionDueSoonDays        int `json:"actionDueSoonDays"`
}

// ReportingFrameworkScope tells the client that frameworkIds do not filter
// the rest of the response. An empty list means compliance is unfiltered.
// Unknown ids are ignored, the same way as the compliance coverage API.
type ReportingFrameworkScope struct {
	AppliesTo    string `json:"appliesTo"`
	FrameworkIDs []uint `json:"frameworkIds"`
}

type ReportingServiceCount struct {
	ServiceID *uint  `json:"serviceId"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	Count     int64  `json:"count"`
}

type ReportingExecutive struct {
	AssetsTotal            int64          `json:"assetsTotal"`
	OpenRisks              int64          `json:"openRisks"`
	HighCriticalOpenRisks  int64          `json:"highCriticalOpenRisks"`
	ActiveFrameworks       int64          `json:"activeFrameworks"`
	ApplicableRequirements int64          `json:"applicableRequirements"`
	MappedRequirements     int64          `json:"mappedRequirements"`
	MappingCoverage        DashboardRatio `json:"mappingCoverage"`
	ControlsTotal          int64          `json:"controlsTotal"`
	ImplementationsTotal   int64          `json:"implementationsTotal"`
	EvidenceExpired        int64          `json:"evidenceExpired"`
	EvidenceExpiringSoon   int64          `json:"evidenceExpiringSoon"`
	EvidenceInvalid        int64          `json:"evidenceInvalid"`
	OpenActions            int64          `json:"openActions"`
	OverdueActions         int64          `json:"overdueActions"`
	DueSoonActions         int64          `json:"dueSoonActions"`
	ActiveAudits           int64          `json:"activeAudits"`
	OpenFindings           int64          `json:"openFindings"`
	OverdueFindings        int64          `json:"overdueFindings"`
	ActiveIncidents        int64          `json:"activeIncidents"`
	PublishedPolicies      int64          `json:"publishedPolicies"`
	PoliciesInReview       int64          `json:"policiesInReview"`
}

// ReportingRisks. Open risks are OPEN and IN_TREATMENT. Level distributions
// cover open risks only. Residual and inherent stay separate; high/critical
// operational risk uses the residual score, as the dashboard does.
type ReportingRisks struct {
	ByStatus               []DashboardCount        `json:"byStatus"`
	ByTreatment            []DashboardCount        `json:"byTreatment"`
	OpenByInherentLevel    []DashboardCount        `json:"openByInherentLevel"`
	OpenByResidualLevel    []DashboardCount        `json:"openByResidualLevel"`
	HighCriticalOpenRisks  int64                   `json:"highCriticalOpenRisks"`
	OpenByOwningService    []ReportingServiceCount `json:"openByOwningService"`
	OpenByAssetCriticality []DashboardCount        `json:"openByAssetCriticality"`
}

type ReportingFrameworkCoverage struct {
	FrameworkID            uint           `json:"frameworkId"`
	Code                   string         `json:"code"`
	Name                   string         `json:"name"`
	Status                 string         `json:"status"`
	ActiveRequirements     int64          `json:"activeRequirements"`
	ApplicableRequirements int64          `json:"applicableRequirements"`
	MappedRequirements     int64          `json:"mappedRequirements"`
	MappingCoverage        DashboardRatio `json:"mappingCoverage"`
}

// ReportingCompliance measures control mapping, not a compliance score.
// Applicable means an ACTIVE requirement of an ACTIVE framework, the same
// definition as ComplianceCoverage.
type ReportingCompliance struct {
	ActiveFrameworks       int64                        `json:"activeFrameworks"`
	ActiveRequirements     int64                        `json:"activeRequirements"`
	ApplicableRequirements int64                        `json:"applicableRequirements"`
	MappedRequirements     int64                        `json:"mappedRequirements"`
	MappingCoverage        DashboardRatio               `json:"mappingCoverage"`
	ByFramework            []ReportingFrameworkCoverage `json:"byFramework"`
}

// ReportingControls has no effectiveness score. Category is free text and
// is not aggregated.
type ReportingControls struct {
	Total                   int64            `json:"total"`
	ByStatus                []DashboardCount `json:"byStatus"`
	ImplementationsTotal    int64            `json:"implementationsTotal"`
	ImplementationsByStatus []DashboardCount `json:"implementationsByStatus"`
}

// ReportingEvidence uses the four mutually exclusive EvidenceValidity states.
// INVALID is a non-VALID status. EXPIRED is not also counted as INVALID.
type ReportingEvidence struct {
	ByValidity   []DashboardCount `json:"byValidity"`
	ByType       []DashboardCount `json:"byType"`
	Valid        int64            `json:"valid"`
	ExpiringSoon int64            `json:"expiringSoon"`
	Expired      int64            `json:"expired"`
	Invalid      int64            `json:"invalid"`
}

type ReportingActions struct {
	ByStatus       []DashboardCount `json:"byStatus"`
	ByDueState     []DashboardCount `json:"byDueState"`
	OpenByPriority []DashboardCount `json:"openByPriority"`
	Open           int64            `json:"open"`
	Overdue        int64            `json:"overdue"`
	DueSoon        int64            `json:"dueSoon"`
}

// ReportingAudits. Active means PLANNED or IN_PROGRESS. Completed means
// COMPLETED. CANCELLED is neither. There is no audit score.
type ReportingAudits struct {
	ByStatus  []DashboardCount `json:"byStatus"`
	ByType    []DashboardCount `json:"byType"`
	Active    int64            `json:"active"`
	Completed int64            `json:"completed"`
}

// ReportingFindings. Open excludes CLOSED and ACCEPTED. Overdue reuses
// FindingOverdue (OPEN or IN_REMEDIATION, due strictly before now).
type ReportingFindings struct {
	ByStatus   []DashboardCount `json:"byStatus"`
	BySeverity []DashboardCount `json:"bySeverity"`
	Open       int64            `json:"open"`
	Overdue    int64            `json:"overdue"`
}

// ReportingIncidents. Active means every status except CLOSED. No MTTR.
type ReportingIncidents struct {
	ByStatus   []DashboardCount `json:"byStatus"`
	BySeverity []DashboardCount `json:"bySeverity"`
	Active     int64            `json:"active"`
}

// ReportingPolicies. PUBLISHED means published, not acknowledged or effective
// as a compliance result.
type ReportingPolicies struct {
	ByStatus        []DashboardCount        `json:"byStatus"`
	ByOwningService []ReportingServiceCount `json:"byOwningService"`
	Published       int64                   `json:"published"`
	InReview        int64                   `json:"inReview"`
}

type GRCReporting struct {
	GeneratedAt    time.Time               `json:"generatedAt"`
	Windows        ReportingWindows        `json:"windows"`
	FrameworkScope ReportingFrameworkScope `json:"frameworkScope"`
	Executive      ReportingExecutive      `json:"executive"`
	Risks          ReportingRisks          `json:"risks"`
	Compliance     ReportingCompliance     `json:"compliance"`
	Controls       ReportingControls       `json:"controls"`
	Evidence       ReportingEvidence       `json:"evidence"`
	Actions        ReportingActions        `json:"actions"`
	Audits         ReportingAudits         `json:"audits"`
	Findings       ReportingFindings       `json:"findings"`
	Incidents      ReportingIncidents      `json:"incidents"`
	Policies       ReportingPolicies       `json:"policies"`
}
