package patients

import "time"

type CreatePatientRequest struct {
	Nom             string  `json:"nom" binding:"required,min=2,max=100"`
	Prenoms         string  `json:"prenoms" binding:"max=150"`
	Sexe            string  `json:"sexe" binding:"omitempty,oneof=M F"`
	DateNaissance   string  `json:"dateNaissance"`
	Age             *int    `json:"age" binding:"omitempty,gte=0,lte=130"`
	Telephone       string  `json:"telephone" binding:"omitempty,min=8,max=30"`
	Quartier        string  `json:"quartier" binding:"max=150"`
	PersonneContact string  `json:"personneContact" binding:"max=150"`
	IsAssure        bool    `json:"isAssure"`
	TauxCouverture  float64 `json:"tauxCouverture" binding:"gte=0,lte=100"`
	MatriculeAssure string  `json:"matriculeAssure" binding:"max=100"`
}

type UpdatePatientRequest struct {
	Nom             string  `json:"nom"`
	Prenoms         string  `json:"prenoms"`
	Sexe            string  `json:"sexe"`
	DateNaissance   string  `json:"dateNaissance"`
	Age             *int    `json:"age"`
	Telephone       string  `json:"telephone"`
	Quartier        string  `json:"quartier"`
	PersonneContact string  `json:"personneContact"`
	IsAssure        bool    `json:"isAssure"`
	TauxCouverture  float64 `json:"tauxCouverture"`
	MatriculeAssure string  `json:"matriculeAssure"`
}

type PatientResponse struct {
	ID   uint   `json:"id"`
	UUID string `json:"uuid"`

	CodePatient   string `json:"codePatient"`
	NumeroDossier string `json:"numeroDossier"`

	Nom     string `json:"nom"`
	Prenoms string `json:"prenoms"`
	Sexe    string `json:"sexe"`

	DateNaissance *time.Time `json:"dateNaissance"`
	Age           *int       `json:"age"`

	Telephone       string `json:"telephone"`
	Quartier        string `json:"quartier"`
	PersonneContact string `json:"personneContact"`

	IsAssure        bool    `json:"isAssure"`
	TauxCouverture  float64 `json:"tauxCouverture"`
	MatriculeAssure string  `json:"matriculeAssure"`
}

type PatientSummary struct {
	ID uint `json:"id"`

	CodePatient   string `json:"codePatient"`
	NumeroDossier string `json:"numeroDossier"`

	Nom     string `json:"nom"`
	Prenoms string `json:"prenoms"`
	Sexe    string `json:"sexe"`

	DateNaissance *time.Time `json:"dateNaissance"`
	Age           *int       `json:"age"`

	Telephone       string `json:"telephone"`
	Quartier        string `json:"quartier"`
	PersonneContact string `json:"personneContact"`

	IsAssure        bool    `json:"isAssure"`
	TauxCouverture  float64 `json:"tauxCouverture"`
	MatriculeAssure string  `json:"matriculeAssure"`
}
