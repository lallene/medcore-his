package entity

import (
	"time"

	"gorm.io/gorm"
)

type BaseEntity struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	UUID      string         `gorm:"type:varchar(80);uniqueIndex" json:"uuid"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
	CreatedBy *uint          `json:"createdBy"`
	UpdatedBy *uint          `json:"updatedBy"`
	Version   uint           `gorm:"default:1" json:"version"`
}
