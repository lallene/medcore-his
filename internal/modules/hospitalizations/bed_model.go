package hospitalizations

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/entity"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

const (
	BedAvailable    = "AVAILABLE"
	BedOccupied     = "OCCUPIED"
	BedReserved     = "RESERVED"
	BedOutOfService = "OUT_OF_SERVICE"

	AssignmentReserved = "RESERVED"
	AssignmentOccupied = "OCCUPIED"
)

type Room struct {
	entity.BaseEntity
	Code              string `gorm:"size:50;not null;uniqueIndex" json:"code"`
	Name              string `gorm:"size:150;not null" json:"name"`
	Department        string `gorm:"size:150;not null;index" json:"department"`
	Floor             string `gorm:"size:80" json:"floor"`
	RoomType          string `gorm:"size:80;not null" json:"roomType"`
	IsActive          bool   `gorm:"not null;default:true;index" json:"isActive"`
	BedCount          int64  `gorm:"-" json:"bedCount"`
	AvailableBedCount int64  `gorm:"-" json:"availableBedCount"`
	OccupiedBedCount  int64  `gorm:"-" json:"occupiedBedCount"`
	ReservedBedCount  int64  `gorm:"-" json:"reservedBedCount"`
	OutOfServiceCount int64  `gorm:"-" json:"outOfServiceBedCount"`
}

func (Room) TableName() string { return "hospitalization_rooms" }

type Bed struct {
	entity.BaseEntity
	RoomID   uint   `gorm:"not null;index" json:"roomId"`
	Room     Room   `gorm:"foreignKey:RoomID" json:"room"`
	Code     string `gorm:"size:50;not null;uniqueIndex" json:"code"`
	Label    string `gorm:"size:150;not null" json:"label"`
	BedType  string `gorm:"size:80;not null" json:"bedType"`
	Status   string `gorm:"size:30;not null;default:AVAILABLE;index;check:bed_status_valid,status IN ('AVAILABLE','OCCUPIED','RESERVED','OUT_OF_SERVICE')" json:"status"`
	IsActive bool   `gorm:"not null;default:true;index" json:"isActive"`
}

func (Bed) TableName() string { return "hospitalization_beds" }

type BedAssignment struct {
	entity.BaseEntity
	HospitalizationID uint             `gorm:"not null;index" json:"hospitalizationId"`
	Hospitalization   Hospitalization  `gorm:"foreignKey:HospitalizationID" json:"hospitalization"`
	PatientID         uint             `gorm:"not null;index" json:"patientId"`
	Patient           patients.Patient `gorm:"foreignKey:PatientID" json:"patient"`
	BedID             uint             `gorm:"not null;index" json:"bedId"`
	Bed               Bed              `gorm:"foreignKey:BedID" json:"bed"`
	AssignedAt        time.Time        `gorm:"not null;index" json:"assignedAt"`
	ReleasedAt        *time.Time       `gorm:"index" json:"releasedAt"`
	AssignmentType    string           `gorm:"size:20;not null;check:bed_assignment_type_valid,assignment_type IN ('RESERVED','OCCUPIED')" json:"assignmentType"`
}

func (BedAssignment) TableName() string { return "hospitalization_bed_assignments" }
