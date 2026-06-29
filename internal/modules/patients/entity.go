package patients

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
)

type Patient struct {
	entity.BaseEntity

	CodePatient   string `gorm:"uniqueIndex;size:50" json:"codePatient"`
	NumeroDossier string `gorm:"uniqueIndex;size:50" json:"numeroDossier"`

	Nom           string     `gorm:"size:100;not null" json:"nom"`
	Prenoms       string     `gorm:"size:150" json:"prenoms"`
	Sexe          string     `gorm:"size:10" json:"sexe"`
	DateNaissance *time.Time `json:"dateNaissance"`
	Age           *int       `json:"age"`

	Telephone       string `gorm:"size:50;index" json:"telephone"`
	Quartier        string `gorm:"size:150" json:"quartier"`
	PersonneContact string `gorm:"size:150" json:"personneContact"`

	IsAssure        bool    `gorm:"default:false" json:"isAssure"`
	TauxCouverture  float64 `gorm:"default:0" json:"tauxCouverture"`
	MatriculeAssure string  `gorm:"size:100" json:"matriculeAssure"`
}

func (Patient) TableName() string {
	return "patients"
}
