package access

import "time"

// PermissionOverride stores a direct GRANT/DENY for a user.
type PermissionOverride struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	UserID     uint       `gorm:"not null;uniqueIndex:ux_staff_perm_override;index" json:"userId"`
	Permission string     `gorm:"size:120;not null;uniqueIndex:ux_staff_perm_override" json:"permission"`
	Effect     string     `gorm:"size:10;not null" json:"effect"` // GRANT | DENY
	Reason     string     `gorm:"size:500" json:"reason"`
	Active     bool       `gorm:"not null;default:true;index" json:"active"`
	CreatedBy  uint       `gorm:"not null" json:"createdBy"`
	UpdatedBy  uint       `gorm:"not null" json:"updatedBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	RemovedBy  *uint      `json:"removedBy"`
	RemovedAt  *time.Time `json:"removedAt"`
}

func (PermissionOverride) TableName() string { return "staff_permission_overrides" }

// MatrixOverride adjusts function→permission defaults without editing Go source.
type MatrixOverride struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	FunctionCode string     `gorm:"size:60;not null;uniqueIndex:ux_rbac_matrix_ov;index" json:"functionCode"`
	Permission   string     `gorm:"size:120;not null;uniqueIndex:ux_rbac_matrix_ov" json:"permission"`
	Effect       string     `gorm:"size:10;not null" json:"effect"` // GRANT | DENY
	Reason       string     `gorm:"size:500" json:"reason"`
	Active       bool       `gorm:"not null;default:true;index" json:"active"`
	CreatedBy    uint       `gorm:"not null" json:"createdBy"`
	UpdatedBy    uint       `gorm:"not null" json:"updatedBy"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	RemovedBy    *uint      `json:"removedBy"`
	RemovedAt    *time.Time `json:"removedAt"`
}

func (MatrixOverride) TableName() string { return "rbac_matrix_overrides" }

// AccessAuditEvent records RBAC administration changes.
type AccessAuditEvent struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TargetUserID uint      `gorm:"not null;index" json:"targetUserId"`
	ActorUserID  uint      `gorm:"not null;index" json:"actorUserId"`
	Action       string    `gorm:"size:60;not null;index" json:"action"`
	Permission   string    `gorm:"size:120" json:"permission"`
	OldValue     string    `gorm:"size:500" json:"oldValue"`
	NewValue     string    `gorm:"size:500" json:"newValue"`
	FunctionCode string    `gorm:"size:60" json:"functionCode"`
	ServiceID    *uint     `json:"serviceId"`
	Reason       string    `gorm:"size:500" json:"reason"`
	CreatedAt    time.Time `gorm:"not null;index" json:"createdAt"`
}

func (AccessAuditEvent) TableName() string { return "rbac_access_audit_events" }
