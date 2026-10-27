package grc

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

// GRC reminder intents are technical delivery rows. They never change the
// lifecycle of an evidence, an action, or a policy.
const (
	ReminderKindEvidenceExpiring = "EVIDENCE_EXPIRING"
	ReminderKindActionDue        = "ACTION_DUE"
	ReminderKindPolicyExpiring   = "POLICY_EXPIRING"

	ReminderStatusPending    = "PENDING"
	ReminderStatusProcessing = "PROCESSING"
	ReminderStatusDelivered  = "DELIVERED"
	ReminderStatusFailed     = "FAILED"

	// ReminderMaxAttempts is the GRC-13A bound. The appointment worker's limit
	// of 5 stays in that domain and is not reused here.
	ReminderMaxAttempts = 3

	// ReminderLeaseDefault matches the appointment worker stale-claim window
	// as an operational duration. It is not imported from that package.
	ReminderLeaseDefault = 15 * time.Minute

	// ReminderClaimBatch is the appointment worker's default claim batch.
	// One pass never claims more than this.
	ReminderClaimBatch = 25

	ReminderIntervalDefault = time.Hour
)

// GRCReminderIntent is one durable reminder for one recipient and one logical
// occurrence. EMAIL is the only channel and is not stored: there is no other
// channel to choose. The row does not record a staff actor.
type GRCReminderIntent struct {
	ID uint `gorm:"primaryKey" json:"id"`

	Kind   string `gorm:"size:32;not null;check:grc_reminder_kind_valid,kind IN ('EVIDENCE_EXPIRING','ACTION_DUE','POLICY_EXPIRING')" json:"kind"`
	Status string `gorm:"size:20;not null;index:idx_grc_reminder_claim,priority:1;index:idx_grc_reminder_lease,priority:1;check:grc_reminder_status_valid,status IN ('PENDING','PROCESSING','DELIVERED','FAILED')" json:"status"`

	EvidenceID          *uint              `gorm:"index;check:grc_reminder_one_target,(CASE WHEN evidence_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN remediation_action_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN governance_policy_id IS NOT NULL THEN 1 ELSE 0 END) = 1" json:"evidenceId"`
	Evidence            *Evidence          `gorm:"foreignKey:EvidenceID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	RemediationActionID *uint              `gorm:"index;check:grc_reminder_kind_target,(kind = 'EVIDENCE_EXPIRING' AND evidence_id IS NOT NULL AND remediation_action_id IS NULL AND governance_policy_id IS NULL) OR (kind = 'ACTION_DUE' AND remediation_action_id IS NOT NULL AND evidence_id IS NULL AND governance_policy_id IS NULL) OR (kind = 'POLICY_EXPIRING' AND governance_policy_id IS NOT NULL AND evidence_id IS NULL AND remediation_action_id IS NULL)" json:"remediationActionId"`
	RemediationAction   *RemediationAction `gorm:"foreignKey:RemediationActionID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	GovernancePolicyID  *uint              `gorm:"index" json:"governancePolicyId"`
	GovernancePolicy    *GovernancePolicy  `gorm:"foreignKey:GovernancePolicyID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`

	RecipientProfileID uint           `gorm:"not null;uniqueIndex:uidx_grc_reminder_recipient_occurrence" json:"recipientProfileId"`
	RecipientProfile   *staff.Profile `gorm:"foreignKey:RecipientProfileID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`

	OccurrenceKey string `gorm:"size:255;not null;uniqueIndex:uidx_grc_reminder_recipient_occurrence" json:"occurrenceKey"`

	ScheduledFor   time.Time  `gorm:"not null;index:idx_grc_reminder_claim,priority:2" json:"scheduledFor"`
	ClaimedAt      *time.Time `json:"claimedAt"`
	LeaseExpiresAt *time.Time `gorm:"index:idx_grc_reminder_lease,priority:2" json:"leaseExpiresAt"`

	AttemptCount  int        `gorm:"not null;default:0;check:grc_reminder_attempt_nonneg,attempt_count >= 0" json:"attemptCount"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
	DeliveredAt   *time.Time `gorm:"check:grc_reminder_timestamps,((status = 'DELIVERED') = (delivered_at IS NOT NULL)) AND ((status = 'PROCESSING') = (claimed_at IS NOT NULL AND lease_expires_at IS NOT NULL))" json:"deliveredAt"`

	LastError *string `gorm:"size:500" json:"lastError"`

	CreatedAt time.Time `gorm:"not null" json:"createdAt"`
	UpdatedAt time.Time `gorm:"not null" json:"updatedAt"`
}

func (GRCReminderIntent) TableName() string { return "grc_reminder_intents" }
