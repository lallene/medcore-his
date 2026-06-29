package workflow

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
)

type History struct {
	entity.BaseEntity

	WorkflowName string `gorm:"size:100;index" json:"workflowName"`
	EntityName   string `gorm:"size:100;index" json:"entityName"`
	EntityID     uint   `gorm:"index" json:"entityId"`

	FromState string `gorm:"size:100" json:"fromState"`
	ToState   string `gorm:"size:100" json:"toState"`
	Action    string `gorm:"size:100;index" json:"action"`

	UserID *uint  `gorm:"index" json:"userId"`
	Role   string `gorm:"size:100" json:"role"`
	Reason string `gorm:"type:text" json:"reason"`

	OccurredAt time.Time `gorm:"index" json:"occurredAt"`
}

func (History) TableName() string {
	return "workflow_histories"
}
