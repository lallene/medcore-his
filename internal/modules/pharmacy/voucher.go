package pharmacy

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	VoucherStatusPending   = "PENDING"
	VoucherStatusPartial   = "PARTIAL"
	VoucherStatusCompleted = "COMPLETED"
	VoucherStatusCancelled = "CANCELLED"
)

type VoucherFilter struct {
	Search, Status, Service, Prescriber, Date string
	Page, PageSize                            int
}

type PharmacyVoucherLineResponse struct {
	ID                 uint    `json:"id"`
	PrescriptionID     uint    `json:"prescriptionId"`
	PresentationID     uint    `json:"presentationId"`
	CommercialName     string  `json:"commercialName"`
	GenericName        string  `json:"genericName"`
	Dosage             string  `json:"dosage"`
	Form               string  `json:"form"`
	Route              string  `json:"route"`
	Unit               string  `json:"unit"`
	PrescribedQuantity float64 `json:"prescribedQuantity"`
	DispensedQuantity  float64 `json:"dispensedQuantity"`
	RemainingQuantity  float64 `json:"remainingQuantity"`
	AvailableQuantity  float64 `json:"availableQuantity"`
	StockStatus        string  `json:"stockStatus"`
	Status             string  `json:"status"`
}

type PharmacyVoucherResponse struct {
	ID             uint                          `json:"id"`
	Number         string                        `json:"number"`
	ConsultationID uint                          `json:"consultationId"`
	PatientID      uint                          `json:"patientId"`
	PatientName    string                        `json:"patientName"`
	PatientCode    string                        `json:"patientCode"`
	Service        string                        `json:"service"`
	Prescriber     string                        `json:"prescriber"`
	IsInsured      bool                          `json:"isInsured"`
	InsuranceName  string                        `json:"insuranceName,omitempty"`
	Status         string                        `json:"status"`
	LineCount      int                           `json:"lineCount"`
	Lines          []PharmacyVoucherLineResponse `json:"lines,omitempty"`
	CreatedAt      time.Time                     `json:"createdAt"`
}

type PharmacyVoucherListResponse struct {
	Items    []PharmacyVoucherResponse `json:"items"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"pageSize"`
	Total    int64                     `json:"total"`
}

// MaterializeVoucher synchronizes the operational voucher with the current
// internal prescriptions. It must be called inside the prescription transaction.
func MaterializeVoucher(tx *gorm.DB, consultationID uint, authorID *uint) error {
	var context struct{ PatientID uint }
	if err := tx.Table("consultations").Select("patient_id").Where("id = ?", consultationID).First(&context).Error; err != nil {
		return err
	}
	var prescriptions []struct{ ID uint }
	if err := tx.Table("consultation_prescriptions").Select("id").Where("consultation_id = ? AND presentation_id IS NOT NULL AND quantity > 0", consultationID).Find(&prescriptions).Error; err != nil {
		return err
	}
	var voucher PharmacyVoucher
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("consultation_id = ?", consultationID).First(&voucher).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if len(prescriptions) == 0 {
			return nil
		}
		voucher = PharmacyVoucher{Number: fmt.Sprintf("TMP-%d", consultationID), ConsultationID: consultationID, PatientID: context.PatientID, CreatedByID: authorID}
		if err := tx.Create(&voucher).Error; err != nil {
			return err
		}
		number := fmt.Sprintf("PHB-%06d", voucher.ID)
		if err := tx.Model(&voucher).Update("number", number).Error; err != nil {
			return err
		}
		voucher.Number = number
	} else if err != nil {
		return err
	}
	if len(prescriptions) == 0 {
		now := time.Now()
		if err := tx.Where("voucher_id = ?", voucher.ID).Delete(&PharmacyVoucherLine{}).Error; err != nil {
			return err
		}
		return tx.Model(&voucher).Updates(map[string]interface{}{"cancelled_at": now, "cancelled_by_id": authorID, "cancellation_reason": "Toutes les prescriptions ont été retirées"}).Error
	}
	if voucher.CancelledAt != nil {
		if err := tx.Model(&voucher).Updates(map[string]interface{}{"cancelled_at": nil, "cancelled_by_id": nil, "cancellation_reason": ""}).Error; err != nil {
			return err
		}
	}
	ids := make([]uint, 0, len(prescriptions))
	for _, prescription := range prescriptions {
		ids = append(ids, prescription.ID)
		line := PharmacyVoucherLine{VoucherID: voucher.ID, PrescriptionID: prescription.ID}
		if err := tx.Where("prescription_id = ?", prescription.ID).FirstOrCreate(&line).Error; err != nil {
			return err
		}
	}
	return tx.Where("voucher_id = ? AND prescription_id NOT IN ?", voucher.ID, ids).Delete(&PharmacyVoucherLine{}).Error
}

func BackfillVouchers(db *gorm.DB) error {
	var ids []uint
	if err := db.Table("consultations c").Distinct("c.id").Joins("JOIN consultation_prescriptions cp ON cp.consultation_id = c.id AND cp.presentation_id IS NOT NULL AND cp.quantity > 0").Pluck("c.id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := db.Transaction(func(tx *gorm.DB) error { return MaterializeVoucher(tx, id, nil) }); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) FindVouchers(filter VoucherFilter) ([]PharmacyVoucher, int64, error) {
	query := r.db.Model(&PharmacyVoucher{}).
		Joins("JOIN consultations c ON c.id = pharmacy_vouchers.consultation_id").
		Joins("JOIN patients p ON p.id = pharmacy_vouchers.patient_id")
	if filter.Search != "" {
		like := "%" + strings.ToLower(filter.Search) + "%"
		query = query.Where("LOWER(pharmacy_vouchers.number) LIKE ? OR LOWER(CONCAT(p.nom,' ',p.prenoms)) LIKE ? OR LOWER(p.code_patient) LIKE ? OR LOWER(c.doctor_name) LIKE ? OR EXISTS (SELECT 1 FROM pharmacy_voucher_lines vl JOIN consultation_prescriptions cp ON cp.id=vl.prescription_id JOIN medication_presentations mp ON mp.id=cp.presentation_id JOIN medications m ON m.id=mp.medication_id WHERE vl.voucher_id=pharmacy_vouchers.id AND LOWER(m.name) LIKE ?)", like, like, like, like, like)
	}
	if filter.Service != "" {
		query = query.Where("c.service = ?", filter.Service)
	}
	if filter.Prescriber != "" {
		query = query.Where("c.doctor_name = ?", filter.Prescriber)
	}
	if filter.Date != "" {
		query = query.Where("DATE(pharmacy_vouchers.created_at) = ?", filter.Date)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var vouchers []PharmacyVoucher
	err := query.Order("pharmacy_vouchers.created_at DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&vouchers).Error
	return vouchers, total, err
}

func (r *Repository) FindVoucherByID(id uint) (*PharmacyVoucher, error) {
	var voucher PharmacyVoucher
	if err := r.db.Preload("Lines").First(&voucher, id).Error; err != nil {
		return nil, err
	}
	return &voucher, nil
}

func (s *Service) voucherResponse(voucher *PharmacyVoucher, withLines bool) (*PharmacyVoucherResponse, error) {
	var context struct{ PatientName, PatientCode, Service, Prescriber string }
	if err := s.repo.db.Table("consultations c").Select("CONCAT(p.nom,' ',p.prenoms) patient_name, p.code_patient patient_code, c.service, c.doctor_name prescriber").Joins("JOIN patients p ON p.id=c.patient_id").Where("c.id = ?", voucher.ConsultationID).Scan(&context).Error; err != nil {
		return nil, err
	}
	var insurance struct{ Name string }
	now := time.Now()
	_ = s.repo.db.Table("patient_coverages pc").Select("ic.name").Joins("JOIN insurance_companies ic ON ic.id=pc.company_id").Where("pc.patient_id=? AND pc.is_active=? AND (pc.valid_from IS NULL OR pc.valid_from<=?) AND (pc.valid_to IS NULL OR pc.valid_to>=?)", voucher.PatientID, true, now, now).Order("pc.is_principal DESC").Limit(1).Scan(&insurance).Error
	response := &PharmacyVoucherResponse{ID: voucher.ID, Number: voucher.Number, ConsultationID: voucher.ConsultationID, PatientID: voucher.PatientID, PatientName: context.PatientName, PatientCode: context.PatientCode, Service: context.Service, Prescriber: context.Prescriber, IsInsured: insurance.Name != "", InsuranceName: insurance.Name, CreatedAt: voucher.CreatedAt}
	if len(voucher.Lines) == 0 {
		_ = s.repo.db.Where("voucher_id = ?", voucher.ID).Find(&voucher.Lines).Error
	}
	dispensable, err := s.repo.FindDispensableQuantities(now)
	if err != nil {
		return nil, err
	}
	allComplete, anyDispensed := len(voucher.Lines) > 0, false
	for _, line := range voucher.Lines {
		prescription, err := s.repo.FindConsultationPrescriptionByID(line.PrescriptionID)
		if err != nil {
			return nil, err
		}
		presentation, err := s.repo.FindPresentationByID(*prescription.PresentationID)
		if err != nil {
			return nil, err
		}
		dispensed, err := s.repo.SumDispensedQuantityForPrescription(line.PrescriptionID)
		if err != nil {
			return nil, err
		}
		remaining := prescription.Quantity - dispensed
		if remaining < 0 {
			remaining = 0
		}
		lineStatus := VoucherStatusPending
		if dispensed > 0 && remaining > 0 {
			lineStatus = VoucherStatusPartial
		}
		if remaining <= 0 {
			lineStatus = VoucherStatusCompleted
		}
		if dispensed > 0 {
			anyDispensed = true
		}
		if remaining > 0 {
			allComplete = false
		}
		available := dispensable[presentation.ID]
		stockStatus := "OUT_OF_STOCK"
		stock, stockErr := s.repo.FindStockByPresentationID(presentation.ID)
		if stockErr == nil && available > 0 {
			stockStatus = "AVAILABLE"
			if available <= stock.AlertThreshold {
				stockStatus = "LOW_STOCK"
			}
		}
		if withLines {
			response.Lines = append(response.Lines, PharmacyVoucherLineResponse{ID: line.ID, PrescriptionID: line.PrescriptionID, PresentationID: presentation.ID, CommercialName: presentation.Medication.Name, GenericName: presentation.Medication.GenericName, Dosage: presentation.Dosage, Form: presentation.Form, Route: presentation.Route, Unit: presentation.Unit, PrescribedQuantity: prescription.Quantity, DispensedQuantity: dispensed, RemainingQuantity: remaining, AvailableQuantity: available, StockStatus: stockStatus, Status: lineStatus})
		}
	}
	response.LineCount = len(voucher.Lines)
	response.Status = VoucherStatusPending
	if voucher.CancelledAt != nil {
		response.Status = VoucherStatusCancelled
	} else if allComplete {
		response.Status = VoucherStatusCompleted
	} else if anyDispensed {
		response.Status = VoucherStatusPartial
	}
	return response, nil
}

func (s *Service) GetVouchers(filter VoucherFilter) (*PharmacyVoucherListResponse, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	vouchers, total, err := s.repo.FindVouchers(filter)
	if err != nil {
		return nil, err
	}
	items := make([]PharmacyVoucherResponse, 0, len(vouchers))
	for i := range vouchers {
		item, err := s.voucherResponse(&vouchers[i], false)
		if err != nil {
			return nil, err
		}
		if filter.Status == "" || item.Status == filter.Status {
			items = append(items, *item)
		}
	}
	return &PharmacyVoucherListResponse{Items: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func (s *Service) GetVoucher(id uint) (*PharmacyVoucherResponse, error) {
	voucher, err := s.repo.FindVoucherByID(id)
	if err != nil {
		return nil, err
	}
	return s.voucherResponse(voucher, true)
}
