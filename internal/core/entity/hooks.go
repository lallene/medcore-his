package entity

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func (e *BaseEntity) BeforeCreate(tx *gorm.DB) error {
	if e.UUID == "" {
		e.UUID = uuid.NewString()
	}

	return nil
}

func (e *BaseEntity) BeforeUpdate(tx *gorm.DB) error {
	e.Version++

	return nil
}
