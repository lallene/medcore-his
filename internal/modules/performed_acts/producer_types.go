package performed_acts

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
)

// Bounded producer source types (LOT27D).
const (
	SourceConsultation = "CONSULTATION"
	SourceLaboratory   = "LABORATORY"
	SourceImaging      = "IMAGING"
)

// ValidSourceTypes lists producer source types implemented in LOT27D.
var ValidSourceTypes = map[string]bool{
	SourceConsultation: true,
	SourceLaboratory:   true,
	SourceImaging:      true,
}

// ConsultationClinicalKey is the ProducerMap.ClinicalKey for the default consultation act.
const ConsultationClinicalKey = ""

// ProducerMap is an explicit, auditable link from a clinical producer key to an ActCatalog entry.
// ClinicalKey:
//   - CONSULTATION: empty string = default consultation map
//   - LABORATORY / IMAGING: medical_exams.code (exact)
type ProducerMap struct {
	ID                uint              `gorm:"primaryKey" json:"id"`
	SourceType        string            `gorm:"size:40;not null;uniqueIndex:ux_act_producer_map,priority:1" json:"sourceType"`
	ClinicalKey       string            `gorm:"size:100;not null;default:'';uniqueIndex:ux_act_producer_map,priority:2" json:"clinicalKey"`
	ActCatalogEntryID uint              `gorm:"not null;index" json:"actCatalogEntryId"`
	ActCatalogEntry   act_catalog.Entry `gorm:"foreignKey:ActCatalogEntryID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT" json:"-"`
	IsActive          bool              `gorm:"not null;default:true;index" json:"isActive"`
	CreatedBy         uint              `gorm:"not null" json:"createdBy"`
	UpdatedBy         uint              `gorm:"not null" json:"updatedBy"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}

func (ProducerMap) TableName() string { return "act_catalog_producer_maps" }

// ProducerCreateRequest is the internal same-TX producer path.
type ProducerCreateRequest struct {
	SourceType        string
	SourceID          uint
	PatientID         uint
	ClinicalKey       string // "" for consultation default; medical_exams.code for lab/imaging
	ConsultationID    *uint
	AppointmentID     *uint
	HospitalizationID *uint
	PerformedAt       *time.Time // nil = now
	ActorID           uint
	Quantity          float64
}

// UpsertProducerMapRequest creates or updates an explicit producer→catalogue map.
type UpsertProducerMapRequest struct {
	SourceType        string `json:"sourceType" binding:"required"`
	ClinicalKey       string `json:"clinicalKey"`
	ActCatalogEntryID uint   `json:"actCatalogEntryId" binding:"required"`
	IsActive          *bool  `json:"isActive"`
}

// UpdateProducerMapRequest updates an existing map by ID.
type UpdateProducerMapRequest struct {
	ActCatalogEntryID uint  `json:"actCatalogEntryId" binding:"required"`
	IsActive          *bool `json:"isActive"`
}

// ProducerReadiness is the deployment readiness report for LOT27D producers.
type ProducerReadiness struct {
	Ready            bool                               `json:"ready"`
	ProducersEnabled bool                               `json:"producersEnabled"`
	Producers        map[string]ProducerReadinessDetail `json:"producers"`
	Missing          []ProducerMapGap                   `json:"missing"`
	Invalid          []ProducerMapGap                   `json:"invalid"`
}

// ProducerReadinessDetail summarizes one source type.
type ProducerReadinessDetail struct {
	Ready           bool             `json:"ready"`
	RequiredCount   int              `json:"requiredCount"`
	ConfiguredCount int              `json:"configuredCount"`
	Missing         []ProducerMapGap `json:"missing,omitempty"`
	Invalid         []ProducerMapGap `json:"invalid,omitempty"`
}

// ProducerMapGap describes a missing or invalid mapping requirement.
type ProducerMapGap struct {
	SourceType  string `json:"sourceType"`
	ClinicalKey string `json:"clinicalKey"`
	Reason      string `json:"reason"`
}
