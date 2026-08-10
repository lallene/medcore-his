package hospitalizations

import (
	"errors"
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func normalize(value string) string { return strings.TrimSpace(value) }

func (s *Service) CreateRoom(req CreateRoomRequest, author uint) (*Room, error) {
	room := Room{Code: normalize(req.Code), Name: normalize(req.Name), Department: normalize(req.Department), Floor: normalize(req.Floor), RoomType: normalize(req.RoomType), IsActive: true}
	room.CreatedBy, room.UpdatedBy = &author, &author
	if err := s.db.Create(&room).Error; err != nil {
		if isDuplicate(err) {
			return nil, coreerrors.Conflict("code de chambre déjà utilisé")
		}
		return nil, err
	}
	return &room, nil
}
func (s *Service) ListRooms() ([]Room, error) {
	var rooms []Room
	err := s.db.Order("department, code").Find(&rooms).Error
	return rooms, err
}
func (s *Service) FindRoom(id uint) (*Room, error) {
	var room Room
	err := s.db.First(&room, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("ROOM")
	}
	return &room, err
}
func (s *Service) UpdateRoom(id uint, req UpdateRoomRequest, author uint) (*Room, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var room Room
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&room, id).Error; err != nil {
			return err
		}
		if req.IsActive != nil && !*req.IsActive {
			var n int64
			if err := tx.Model(&BedAssignment{}).Joins("JOIN hospitalization_beds b ON b.id = hospitalization_bed_assignments.bed_id").Where("b.room_id = ? AND hospitalization_bed_assignments.released_at IS NULL", id).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				return coreerrors.Conflict("une chambre occupée ou réservée ne peut pas être désactivée")
			}
		}
		if req.Name != nil {
			room.Name = normalize(*req.Name)
		}
		if req.Department != nil {
			room.Department = normalize(*req.Department)
		}
		if req.Floor != nil {
			room.Floor = normalize(*req.Floor)
		}
		if req.RoomType != nil {
			room.RoomType = normalize(*req.RoomType)
		}
		if req.IsActive != nil {
			room.IsActive = *req.IsActive
		}
		room.UpdatedBy = &author
		return tx.Save(&room).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("ROOM")
	}
	if err != nil {
		return nil, err
	}
	return s.FindRoom(id)
}

func (s *Service) CreateBed(req CreateBedRequest, author uint) (*Bed, error) {
	room, err := s.FindRoom(req.RoomID)
	if err != nil {
		return nil, err
	}
	if !room.IsActive {
		return nil, coreerrors.Conflict("la chambre est inactive")
	}
	bed := Bed{RoomID: req.RoomID, Code: normalize(req.Code), Label: normalize(req.Label), BedType: normalize(req.BedType), Status: BedAvailable, IsActive: true}
	bed.CreatedBy, bed.UpdatedBy = &author, &author
	if err = s.db.Create(&bed).Error; err != nil {
		if isDuplicate(err) {
			return nil, coreerrors.Conflict("code de lit déjà utilisé")
		}
		return nil, err
	}
	return s.FindBed(bed.ID)
}
func (s *Service) FindBed(id uint) (*Bed, error) {
	var bed Bed
	err := s.db.Preload("Room").First(&bed, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("BED")
	}
	return &bed, err
}
func (s *Service) ListBeds(filter BedFilter) (*BedListResult, error) {
	q := s.db.Model(&Bed{}).Joins("Room")
	if filter.Department != "" {
		q = q.Where("LOWER(\"Room\".department)=LOWER(?)", filter.Department)
	}
	if filter.RoomID != nil {
		q = q.Where("hospitalization_beds.room_id=?", *filter.RoomID)
	}
	if filter.Status != "" {
		q = q.Where("hospitalization_beds.status=?", strings.ToUpper(filter.Status))
	}
	if filter.Active != nil {
		q = q.Where("hospitalization_beds.is_active=?", *filter.Active)
	}
	if filter.Available != nil && *filter.Available {
		q = q.Where("hospitalization_beds.status=? AND hospitalization_beds.is_active=? AND \"Room\".is_active=?", BedAvailable, true, true)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var beds []Bed
	if err := q.Preload("Room").Order("\"Room\".department, \"Room\".code, hospitalization_beds.code").Offset((filter.Page - 1) * filter.Limit).Limit(filter.Limit).Find(&beds).Error; err != nil {
		return nil, err
	}
	data := make([]BedOverview, 0, len(beds))
	for _, bed := range beds {
		var a BedAssignment
		err := s.db.Preload("Patient").Preload("Hospitalization").Where("bed_id=? AND released_at IS NULL", bed.ID).First(&a).Error
		entry := BedOverview{Bed: bed}
		if err == nil {
			entry.ActiveAssignment = &a
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		data = append(data, entry)
	}
	return &BedListResult{Data: data, Page: filter.Page, Limit: filter.Limit, Total: total, TotalPages: int((total + int64(filter.Limit) - 1) / int64(filter.Limit))}, nil
}
func (s *Service) UpdateBed(id uint, req UpdateBedRequest, author uint) (*Bed, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var bed Bed
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&bed, id).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&BedAssignment{}).Where("bed_id=? AND released_at IS NULL", id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 && (req.RoomID != nil || req.IsActive != nil && !*req.IsActive || req.Status != nil && strings.ToUpper(*req.Status) != bed.Status) {
			return coreerrors.Conflict("un lit occupé ou réservé ne peut pas être déplacé, désactivé ou changer de statut")
		}
		if req.RoomID != nil {
			var room Room
			if err := tx.First(&room, *req.RoomID).Error; err != nil {
				return coreerrors.NotFound("ROOM")
			}
			if !room.IsActive {
				return coreerrors.Conflict("la chambre est inactive")
			}
			bed.RoomID = *req.RoomID
		}
		if req.Label != nil {
			bed.Label = normalize(*req.Label)
		}
		if req.BedType != nil {
			bed.BedType = normalize(*req.BedType)
		}
		if req.Status != nil {
			v := strings.ToUpper(*req.Status)
			if v != BedAvailable && v != BedOutOfService {
				return coreerrors.BadRequest("seuls AVAILABLE et OUT_OF_SERVICE sont modifiables manuellement")
			}
			bed.Status = v
		}
		if req.IsActive != nil {
			bed.IsActive = *req.IsActive
		}
		bed.UpdatedBy = &author
		return tx.Save(&bed).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, coreerrors.NotFound("BED")
	}
	if err != nil {
		return nil, err
	}
	return s.FindBed(id)
}

func (s *Service) AssignBed(hospitalizationID, bedID, author uint) (*BedAssignment, error) {
	var created BedAssignment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		h, err := lockByID(tx, hospitalizationID)
		if err != nil {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
		if h.Status != StatusPlanned && h.Status != StatusAdmitted {
			return coreerrors.Conflict("ce séjour ne peut plus recevoir de lit")
		}
		var existing int64
		if err := tx.Model(&BedAssignment{}).Where("hospitalization_id=? AND released_at IS NULL", h.ID).Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			return coreerrors.Conflict("ce séjour possède déjà une affectation active")
		}
		bed, err := lockBed(tx, bedID)
		if err != nil {
			return coreerrors.NotFound("BED")
		}
		if !bed.IsActive || !bed.Room.IsActive || bed.Status != BedAvailable {
			return coreerrors.Conflict("ce lit n'est pas disponible")
		}
		kind, status, event, title := AssignmentReserved, BedReserved, "bed_reserved", "Lit réservé"
		if h.Status == StatusAdmitted {
			kind, status, event, title = AssignmentOccupied, BedOccupied, "bed_assigned", "Lit affecté"
		}
		now := time.Now()
		created = BedAssignment{HospitalizationID: h.ID, PatientID: h.PatientID, BedID: bed.ID, AssignedAt: now, AssignmentType: kind}
		created.CreatedBy, created.UpdatedBy = &author, &author
		if err := tx.Create(&created).Error; err != nil {
			if isDuplicate(err) {
				return coreerrors.Conflict("le lit ou le séjour possède déjà une affectation active")
			}
			return err
		}
		bed.Status = status
		bed.UpdatedBy = &author
		if err := tx.Save(bed).Error; err != nil {
			return err
		}
		return createBedTimeline(tx, h, event, title, fmt.Sprintf("%s — %s", bed.Room.Name, bed.Label), author, now)
	})
	if err != nil {
		return nil, err
	}
	return s.FindAssignment(created.ID)
}
func lockBed(tx *gorm.DB, id uint) (*Bed, error) {
	var bed Bed
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Room").First(&bed, id).Error
	return &bed, err
}
func (s *Service) FindAssignment(id uint) (*BedAssignment, error) {
	var a BedAssignment
	err := s.db.Preload("Bed.Room").Preload("Patient").Preload("Hospitalization").First(&a, id).Error
	return &a, err
}
func (s *Service) ListAssignments(hid uint) ([]BedAssignment, error) {
	var items []BedAssignment
	err := s.db.Preload("Bed.Room").Where("hospitalization_id=?", hid).Order("assigned_at DESC,id DESC").Find(&items).Error
	return items, err
}
func (s *Service) TransferBed(hid, bedID, author uint) (*BedAssignment, error) {
	var created BedAssignment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		h, err := lockByID(tx, hid)
		if err != nil {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
		if h.Status != StatusAdmitted {
			return coreerrors.Conflict("seul un séjour admis peut être transféré")
		}
		var current BedAssignment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("hospitalization_id=? AND released_at IS NULL", hid).First(&current).Error; err != nil {
			return coreerrors.Conflict("aucun lit actif à transférer")
		}
		target, err := lockBed(tx, bedID)
		if err != nil {
			return coreerrors.NotFound("BED")
		}
		if target.ID == current.BedID {
			return coreerrors.Conflict("le patient occupe déjà ce lit")
		}
		if !target.IsActive || !target.Room.IsActive || target.Status != BedAvailable {
			return coreerrors.Conflict("le lit cible n'est pas disponible")
		}
		old, err := lockBed(tx, current.BedID)
		if err != nil {
			return err
		}
		now := time.Now()
		current.ReleasedAt = &now
		current.UpdatedBy = &author
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		old.Status = BedAvailable
		old.UpdatedBy = &author
		if err := tx.Save(old).Error; err != nil {
			return err
		}
		created = BedAssignment{HospitalizationID: h.ID, PatientID: h.PatientID, BedID: target.ID, AssignedAt: now, AssignmentType: AssignmentOccupied}
		created.CreatedBy, created.UpdatedBy = &author, &author
		if err := tx.Create(&created).Error; err != nil {
			return err
		}
		target.Status = BedOccupied
		target.UpdatedBy = &author
		if err := tx.Save(target).Error; err != nil {
			return err
		}
		return createBedTimeline(tx, h, "bed_transferred", "Patient transféré", fmt.Sprintf("%s — %s vers %s — %s", old.Room.Name, old.Label, target.Room.Name, target.Label), author, now)
	})
	if err != nil {
		return nil, err
	}
	return s.FindAssignment(created.ID)
}
func (s *Service) ReleaseBed(hid, author uint) (*BedAssignment, error) {
	var released BedAssignment
	err := s.db.Transaction(func(tx *gorm.DB) error {
		h, err := lockByID(tx, hid)
		if err != nil {
			return coreerrors.NotFound("HOSPITALIZATION")
		}
		return releaseActiveAssignment(tx, h, author, time.Now(), "bed_released", "Lit libéré", &released)
	})
	if err != nil {
		return nil, err
	}
	return s.FindAssignment(released.ID)
}

func releaseActiveAssignment(tx *gorm.DB, h *Hospitalization, author uint, at time.Time, event, title string, out *BedAssignment) error {
	var a BedAssignment
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("hospitalization_id=? AND released_at IS NULL", h.ID).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return coreerrors.Conflict("aucune affectation active")
	}
	if err != nil {
		return err
	}
	bed, err := lockBed(tx, a.BedID)
	if err != nil {
		return err
	}
	a.ReleasedAt = &at
	a.UpdatedBy = &author
	if err := tx.Save(&a).Error; err != nil {
		return err
	}
	if bed.IsActive && bed.Room.IsActive {
		bed.Status = BedAvailable
	} else {
		bed.Status = BedOutOfService
	}
	bed.UpdatedBy = &author
	if err := tx.Save(bed).Error; err != nil {
		return err
	}
	if out != nil {
		*out = a
	}
	return createBedTimeline(tx, h, event, title, fmt.Sprintf("%s — %s", bed.Room.Name, bed.Label), author, at)
}
func createBedTimeline(tx *gorm.DB, h *Hospitalization, event, title, description string, author uint, at time.Time) error {
	ref := h.ID
	e := medical_records.MedicalTimelineEvent{MedicalRecordID: h.MedicalRecordID, PatientID: h.PatientID, EventType: event, Category: "hospitalization", Title: title, Description: description, ReferenceType: "hospitalization", ReferenceID: &ref, Severity: "info", EventDate: at, CreatedBy: author}
	return tx.Create(&e).Error
}
