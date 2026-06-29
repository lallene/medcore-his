package audit

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
)

type AuditLog struct {
	entity.BaseEntity

	Module   string `gorm:"size:100;index" json:"module"`
	Action   string `gorm:"size:50;index" json:"action"`
	RecordID uint   `gorm:"index" json:"recordId"`

	UserID *uint  `gorm:"index" json:"userId"`
	IP     string `gorm:"size:100" json:"ip"`
	Agent  string `gorm:"size:255" json:"agent"`

	OldValue string `gorm:"type:text" json:"oldValue"`
	NewValue string `gorm:"type:text" json:"newValue"`

	OccurredAt time.Time `gorm:"index" json:"occurredAt"`
}

func (AuditLog) TableName() string {
	return "audit_logs"
}
