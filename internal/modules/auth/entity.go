package auth

import "github.com/lallene/medcore-his/backend/internal/core/entity"

type User struct {
	entity.BaseEntity

	Name         string `gorm:"size:150;not null" json:"name"`
	Email        string `gorm:"size:150;uniqueIndex;not null" json:"email"`
	PasswordHash string `gorm:"size:255;not null" json:"-"`
	Role         string `gorm:"size:50;default:admin" json:"role"`
	IsActive     bool   `gorm:"default:true" json:"isActive"`
}

func (User) TableName() string {
	return "users"
}
