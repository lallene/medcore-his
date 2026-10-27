package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

const (
	AssessmentTypeDesign                 = "DESIGN"
	AssessmentTypeOperatingEffectiveness = "OPERATING_EFFECTIVENESS"
	AssessmentTypeCompliance             = "COMPLIANCE"

	AssessmentStatusDraft     = "DRAFT"
	AssessmentStatusCompleted = "COMPLETED"
	AssessmentStatusCancelled = "CANCELLED"

	// A not yet assessed control is a DRAFT assessment without result:
	// there is deliberately no NOT_ASSESSED result.
	AssessmentResultEffective          = "EFFECTIVE"
	AssessmentResultPartiallyEffective = "PARTIALLY_EFFECTIVE"
	AssessmentResultIneffective        = "INEFFECTIVE"
	AssessmentResultNotApplicable      = "NOT_APPLICABLE"

	AuditEngagementTypeInternal = "INTERNAL"
	AuditEngagementTypeExternal = "EXTERNAL"
	AuditEngagementTypeVendor   = "VENDOR"

	AuditEngagementStatusPlanned    = "PLANNED"
	AuditEngagementStatusInProgress = "IN_PROGRESS"
	AuditEngagementStatusCompleted  = "COMPLETED"
	AuditEngagementStatusCancelled  = "CANCELLED"

	FindingCategoryNonConformity     = "NON_CONFORMITY"
	FindingCategoryControlDeficiency = "CONTROL_DEFICIENCY"
	FindingCategoryObservation       = "OBSERVATION"
	FindingCategoryImprovement       = "IMPROVEMENT"

	FindingSeverityLow      = "LOW"
	FindingSeverityMedium   = "MEDIUM"
	FindingSeverityHigh     = "HIGH"
	FindingSeverityCritical = "CRITICAL"

	FindingStatusOpen          = "OPEN"
	FindingStatusInRemediation = "IN_REMEDIATION"
	FindingStatusResolved      = "RESOLVED"
	FindingStatusClosed        = "CLOSED"
	FindingStatusAccepted      = "ACCEPTED"
)

// GORM reads a single check tag per field: multi-column CHECK constraints are
// attached to one of the fields they involve but always apply to the row.
// No CHECK depends on the current time; time-based rules belong to services.

// Assessment is a formal evaluation of one control implementation. Its
// requirements are derived through the control (RequirementControl).
type Assessment struct {
	ID                      uint                   `gorm:"primaryKey" json:"id"`
	Reference               string                 `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title                   string                 `gorm:"size:220;not null" json:"title"`
	ControlImplementationID uint                   `gorm:"not null;index" json:"controlImplementationId"`
	ControlImplementation   *ControlImplementation `gorm:"foreignKey:ControlImplementationID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlImplementation,omitempty"`
	Type                    string                 `gorm:"size:30;not null;index;check:grc_assessment_type_valid,type IN ('DESIGN','OPERATING_EFFECTIVENESS','COMPLIANCE')" json:"type"`
	Status                  string                 `gorm:"size:20;not null;index;check:grc_assessment_status_valid,status IN ('DRAFT','COMPLETED','CANCELLED')" json:"status"`
	Result                  *string                `gorm:"size:30;index;check:grc_assessment_result_valid,result IS NULL OR result IN ('EFFECTIVE','PARTIALLY_EFFECTIVE','INEFFECTIVE','NOT_APPLICABLE')" json:"result"`
	AssessorProfileID       *uint                  `gorm:"index" json:"assessorProfileId"`
	AssessorProfile         *staff.Profile         `gorm:"foreignKey:AssessorProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"assessorProfile,omitempty"`
	AssessedAt              *time.Time             `gorm:"index;check:grc_assessment_completed_fields,status <> 'COMPLETED' OR (result IS NOT NULL AND assessor_profile_id IS NOT NULL AND assessed_at IS NOT NULL)" json:"assessedAt"`
	CompletedAt             *time.Time             `gorm:"check:grc_assessment_completion,(status = 'COMPLETED') = (completed_at IS NOT NULL)" json:"completedAt"`
	NextAssessmentAt        *time.Time             `gorm:"index;check:grc_assessment_next_range,next_assessment_at IS NULL OR assessed_at IS NULL OR next_assessment_at >= assessed_at" json:"nextAssessmentAt"`
	Summary                 string                 `gorm:"type:text;check:grc_assessment_not_applicable_reason,result IS NULL OR result <> 'NOT_APPLICABLE' OR length(trim(coalesce(summary, ''))) > 0" json:"summary"`
	CreatedBy               uint                   `gorm:"not null;index" json:"createdBy"`
	UpdatedBy               uint                   `gorm:"not null;index" json:"updatedBy"`
	CreatedAt               time.Time              `gorm:"not null" json:"createdAt"`
	UpdatedAt               time.Time              `gorm:"not null" json:"updatedAt"`

	ReassessmentDue    bool `gorm:"-" json:"reassessmentDue"`
	HasInvalidEvidence bool `gorm:"-" json:"hasInvalidEvidence"`
}

func (Assessment) TableName() string { return "grc_assessments" }

// AssessmentEvidence records which evidence was examined. It stores no copy
// of the evidence: its validity is checked when the assessment is completed.
type AssessmentEvidence struct {
	ID           uint        `gorm:"primaryKey" json:"id"`
	AssessmentID uint        `gorm:"not null;index;uniqueIndex:idx_grc_assessment_evidence_assessment_evidence,priority:1" json:"assessmentId"`
	Assessment   *Assessment `gorm:"foreignKey:AssessmentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"assessment,omitempty"`
	EvidenceID   uint        `gorm:"not null;index;uniqueIndex:idx_grc_assessment_evidence_assessment_evidence,priority:2" json:"evidenceId"`
	Evidence     *Evidence   `gorm:"foreignKey:EvidenceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"evidence,omitempty"`
	CreatedBy    uint        `gorm:"not null;index" json:"createdBy"`
	CreatedAt    time.Time   `gorm:"not null" json:"createdAt"`
}

func (AssessmentEvidence) TableName() string { return "grc_assessment_evidence" }

// AuditEngagement is a business compliance or security audit. It is unrelated
// to the technical audit trail (internal/core/audit, audit_logs).
type AuditEngagement struct {
	ID                   uint                  `gorm:"primaryKey" json:"id"`
	Reference            string                `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title                string                `gorm:"size:220;not null" json:"title"`
	Description          string                `gorm:"type:text" json:"description"`
	Scope                string                `gorm:"type:text" json:"scope"`
	Type                 string                `gorm:"size:20;not null;index;check:grc_audit_engagement_type_valid,type IN ('INTERNAL','EXTERNAL','VENDOR')" json:"type"`
	Status               string                `gorm:"size:20;not null;index;check:grc_audit_engagement_status_valid,status IN ('PLANNED','IN_PROGRESS','COMPLETED','CANCELLED')" json:"status"`
	LeadAuditorProfileID *uint                 `gorm:"index" json:"leadAuditorProfileId"`
	LeadAuditorProfile   *staff.Profile        `gorm:"foreignKey:LeadAuditorProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"leadAuditorProfile,omitempty"`
	OwningServiceID      *uint                 `gorm:"index" json:"owningServiceId"`
	OwningService        *organization.Service `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	PlannedStartAt       *time.Time            `gorm:"index;check:grc_audit_engagement_planned_range,planned_end_at IS NULL OR planned_start_at IS NULL OR planned_end_at >= planned_start_at" json:"plannedStartAt"`
	PlannedEndAt         *time.Time            `gorm:"check:grc_audit_engagement_execution_range,completed_at IS NULL OR started_at IS NULL OR completed_at >= started_at" json:"plannedEndAt"`
	StartedAt            *time.Time            `gorm:"check:grc_audit_engagement_started_required,status NOT IN ('IN_PROGRESS','COMPLETED') OR started_at IS NOT NULL" json:"startedAt"`
	CompletedAt          *time.Time            `gorm:"check:grc_audit_engagement_completion,(status = 'COMPLETED') = (completed_at IS NOT NULL)" json:"completedAt"`
	CreatedBy            uint                  `gorm:"not null;index" json:"createdBy"`
	UpdatedBy            uint                  `gorm:"not null;index" json:"updatedBy"`
	CreatedAt            time.Time             `gorm:"not null" json:"createdAt"`
	UpdatedAt            time.Time             `gorm:"not null" json:"updatedAt"`

	// Finding counters (grc_findings.audit_engagement_id), derived with the
	// dashboard semantics; never persisted.
	FindingCount        int64 `gorm:"-" json:"findingCount"`
	OpenFindingCount    int64 `gorm:"-" json:"openFindingCount"`
	OverdueFindingCount int64 `gorm:"-" json:"overdueFindingCount"`
}

func (AuditEngagement) TableName() string { return "grc_audit_engagements" }

// Finding comes from exactly one source: an audit engagement or an assessment.
type Finding struct {
	ID                      uint                   `gorm:"primaryKey" json:"id"`
	Reference               string                 `gorm:"size:60;not null;uniqueIndex" json:"reference"`
	Title                   string                 `gorm:"size:220;not null" json:"title"`
	Description             string                 `gorm:"type:text" json:"description"`
	AuditEngagementID       *uint                  `gorm:"index;check:grc_finding_single_source,(audit_engagement_id IS NULL) <> (assessment_id IS NULL)" json:"auditEngagementId"`
	AuditEngagement         *AuditEngagement       `gorm:"foreignKey:AuditEngagementID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"auditEngagement,omitempty"`
	AssessmentID            *uint                  `gorm:"index" json:"assessmentId"`
	Assessment              *Assessment            `gorm:"foreignKey:AssessmentID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"assessment,omitempty"`
	Category                string                 `gorm:"size:30;not null;index;check:grc_finding_category_valid,category IN ('NON_CONFORMITY','CONTROL_DEFICIENCY','OBSERVATION','IMPROVEMENT')" json:"category"`
	Severity                string                 `gorm:"size:20;not null;index;check:grc_finding_severity_valid,severity IN ('LOW','MEDIUM','HIGH','CRITICAL')" json:"severity"`
	Status                  string                 `gorm:"size:20;not null;index;check:grc_finding_status_valid,status IN ('OPEN','IN_REMEDIATION','RESOLVED','CLOSED','ACCEPTED')" json:"status"`
	ControlImplementationID *uint                  `gorm:"index" json:"controlImplementationId"`
	ControlImplementation   *ControlImplementation `gorm:"foreignKey:ControlImplementationID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"controlImplementation,omitempty"`
	RiskID                  *uint                  `gorm:"index" json:"riskId"`
	Risk                    *Risk                  `gorm:"foreignKey:RiskID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"risk,omitempty"`
	OwnerProfileID          *uint                  `gorm:"index" json:"ownerProfileId"`
	OwnerProfile            *staff.Profile         `gorm:"foreignKey:OwnerProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"ownerProfile,omitempty"`
	OwningServiceID         *uint                  `gorm:"index" json:"owningServiceId"`
	OwningService           *organization.Service  `gorm:"foreignKey:OwningServiceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"owningService,omitempty"`
	DueAt                   *time.Time             `gorm:"index;check:grc_finding_closure_range,closed_at IS NULL OR resolved_at IS NULL OR closed_at >= resolved_at" json:"dueAt"`
	ResolvedAt              *time.Time             `gorm:"check:grc_finding_resolution,status NOT IN ('RESOLVED','CLOSED') OR resolved_at IS NOT NULL" json:"resolvedAt"`
	ClosedAt                *time.Time             `gorm:"check:grc_finding_closure,(status IN ('CLOSED','ACCEPTED')) = (closed_at IS NOT NULL)" json:"closedAt"`
	ClosureNote             string                 `gorm:"type:text;check:grc_finding_acceptance_reason,status <> 'ACCEPTED' OR length(trim(coalesce(closure_note, ''))) > 0" json:"closureNote"`
	CreatedBy               uint                   `gorm:"not null;index" json:"createdBy"`
	UpdatedBy               uint                   `gorm:"not null;index" json:"updatedBy"`
	CreatedAt               time.Time              `gorm:"not null" json:"createdAt"`
	UpdatedAt               time.Time              `gorm:"not null" json:"updatedAt"`

	Overdue bool `gorm:"-" json:"overdue"`
}

func (Finding) TableName() string { return "grc_findings" }
