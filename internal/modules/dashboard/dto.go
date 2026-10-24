package dashboard

import "github.com/lallene/medcore-his/backend/internal/core/period"

// DashboardFilter : service et période civile [from,to) partagés (core/period).
type DashboardFilter = period.Filter

type DashboardResponse struct {
	// Activité de la période/service filtrés ([from,to), dates civiles).
	Consultations     int64 `json:"consultations" gorm:"-"`
	Hospitalizations  int64 `json:"hospitalizations" gorm:"-"`
	PharmacyPurchases int64 `json:"pharmacyPurchases" gorm:"-"`
	PatientsSeen      int64 `json:"patientsSeen" gorm:"-"`
	QueueTickets      int64 `json:"queueTickets" gorm:"-"`
	// Montant brut (FCFA) des factures émises sur la période, attribué au service
	// de chaque ligne (consultation ou hospitalisation d'origine).
	BilledAmount int64 `json:"billedAmount" gorm:"-"`
	// Factures créées sur la période dont le solde patient / assurance reste dû
	// (mêmes règles que les écrans Créances patients et Créances assurances).
	PatientReceivables   int64 `json:"patientReceivables" gorm:"-"`
	InsuranceReceivables int64 `json:"insuranceReceivables" gorm:"-"`
	// Remboursements exécutés sur la période ; nil quand un service est filtré,
	// un remboursement de solde patient n'étant rattaché à aucun service.
	Refunds *int64 `json:"refunds" gorm:"-"`
	// Actes réalisés (non annulés) : examens de laboratoire, imagerie, et
	// procédures/autres actes.
	LabExams     int64 `json:"labExams" gorm:"-"`
	ImagingExams int64 `json:"imagingExams" gorm:"-"`
	OtherActs    int64 `json:"otherActs" gorm:"-"`

	// Référentiels globaux (non filtrés) : patients, couvertures, compagnies, garants.
	// Vouchers/Validated/Pending/Rejected/Workflow suivent le filtre (bons créés
	// sur la période, service de la consultation d'origine).
	Patients         int64          `json:"patients" gorm:"column:patients"`
	Insured          int64          `json:"insured" gorm:"column:insured"`
	Companies        int64          `json:"companies" gorm:"column:companies"`
	Guarantors       int64          `json:"guarantors" gorm:"column:guarantors"`
	Vouchers         int64          `json:"vouchers" gorm:"column:vouchers"`
	Validated        int64          `json:"validated" gorm:"column:validated"`
	Pending          int64          `json:"pending" gorm:"column:pending"`
	Rejected         int64          `json:"rejected" gorm:"column:rejected"`
	Workflow         WorkflowStats  `json:"workflow" gorm:"-"`
	RecentActivities []ActivityItem `json:"recentActivities" gorm:"-"`
}

type WorkflowStats struct {
	Draft      int64 `json:"draft" gorm:"column:draft"`
	Submitted  int64 `json:"submitted" gorm:"column:submitted"`
	Controlled int64 `json:"controlled" gorm:"column:controlled"`
	Validated  int64 `json:"validated" gorm:"column:validated"`
	Rejected   int64 `json:"rejected" gorm:"column:rejected"`
	Cancelled  int64 `json:"cancelled" gorm:"column:cancelled"`
}

type ActivityItem struct {
	Time        string `json:"time"`
	Description string `json:"description"`
	Type        string `json:"type"`
}
