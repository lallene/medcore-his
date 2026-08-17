package authorization

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var allowedReferences = map[string]string{
	"CONSULTATION": "consultations", "LABORATORY": "laboratory_orders", "IMAGING": "imaging_orders",
	"HOSPITALIZATION": "hospitalizations", "MEDICATION": "consultation_prescriptions",
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func nonNegative(value *float64, name string) error {
	if value != nil && *value < 0 {
		return coreerrors.BadRequest(name + " ne peut pas être négatif")
	}
	return nil
}

func Calculate(requested float64, status string, rate, fixed, ceiling *float64) (float64, float64, error) {
	if requested < 0 {
		return 0, 0, coreerrors.BadRequest("Le montant demandé ne peut pas être négatif")
	}
	for value, name := range map[*float64]string{rate: "Le taux", fixed: "Le montant accordé", ceiling: "Le plafond"} {
		if err := nonNegative(value, name); err != nil {
			return 0, 0, err
		}
	}
	if rate != nil && *rate > 100 {
		return 0, 0, coreerrors.BadRequest("Le taux accordé doit être compris entre 0 et 100")
	}
	if status == StatusRejected {
		return 0, requested, nil
	}
	if rate == nil && fixed == nil {
		return 0, 0, coreerrors.BadRequest("Un taux ou un montant accordé est obligatoire")
	}
	insurance := requested
	if rate != nil {
		insurance = requested * *rate / 100
	}
	if fixed != nil && (rate == nil || *fixed < insurance) {
		insurance = *fixed
	}
	if ceiling != nil && *ceiling < insurance {
		insurance = *ceiling
	}
	insurance = math.Min(requested, math.Max(0, insurance))
	return math.Round(insurance*100) / 100, math.Round((requested-insurance)*100) / 100, nil
}

func parseDate(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, coreerrors.BadRequest("Date invalide")
	}
	return &t, nil
}

func (s *Service) validateReference(tx *gorm.DB, patientID uint, typ string, id uint) (string, error) {
	typ = strings.ToUpper(strings.TrimSpace(typ))
	table, ok := allowedReferences[typ]
	if !ok {
		return "", coreerrors.BadRequest("Type d'acte non pris en charge par le référentiel actuel")
	}
	var count int64
	if typ == "MEDICATION" {
		err := tx.Table("consultation_prescriptions p").Joins("JOIN consultations c ON c.id = p.consultation_id").Where("p.id = ? AND c.patient_id = ?", id, patientID).Count(&count).Error
		if err != nil {
			return "", err
		}
	} else {
		if err := tx.Table(table).Where("id = ? AND patient_id = ?", id, patientID).Count(&count).Error; err != nil {
			return "", err
		}
	}
	if count == 0 {
		return "", coreerrors.Conflict("L'acte n'existe pas ou appartient à un autre patient")
	}
	return typ, nil
}

func (s *Service) loadCoverage(tx *gorm.DB, patientID, coverageID uint) (*coverage.PatientCoverage, error) {
	var cov coverage.PatientCoverage
	if err := tx.Preload("Company").Preload("Guarantor").First(&cov, coverageID).Error; err != nil {
		return nil, coreerrors.NotFound("PATIENT_COVERAGE")
	}
	if cov.PatientID != patientID {
		return nil, coreerrors.Conflict("La couverture appartient à un autre patient")
	}
	if !cov.IsActive || (cov.ValidFrom != nil && cov.ValidFrom.After(time.Now())) || (cov.ValidTo != nil && cov.ValidTo.Before(time.Now())) {
		return nil, coreerrors.Conflict("La couverture n'est pas active")
	}
	return &cov, nil
}

func (s *Service) Create(req CreateRequest, userID uint) (*Response, error) {
	if err := nonNegative(req.RequestedAmount, "Le montant demandé"); err != nil {
		return nil, err
	}
	var createdID uint
	err := s.db.Transaction(func(tx *gorm.DB) error {
		lock := fnv.New64a()
		_, _ = fmt.Fprintf(lock, "%d:%d:%s:%d", req.PatientID, req.PatientCoverageID, strings.ToUpper(req.ReferenceType), req.ReferenceID)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(lock.Sum64())).Error; err != nil {
			return err
		}
		cov, err := s.loadCoverage(tx, req.PatientID, req.PatientCoverageID)
		if err != nil {
			return err
		}
		typ, err := s.validateReference(tx, req.PatientID, req.ReferenceType, req.ReferenceID)
		if err != nil {
			return err
		}
		var record medical_records.MedicalRecord
		if err := tx.Where("patient_id = ?", req.PatientID).First(&record).Error; err != nil {
			return coreerrors.NotFound("MEDICAL_RECORD")
		}
		var duplicate int64
		if err := tx.Model(&InsuranceAuthorization{}).Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND status <> ?", req.PatientID, cov.ID, typ, req.ReferenceID, StatusCancelled).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return coreerrors.Conflict("Une PEC active existe déjà pour cet acte et cette couverture")
		}
		if err := tx.Model(&InsuranceAuthorizationAct{}).Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND is_active", req.PatientID, cov.ID, typ, req.ReferenceID).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 {
			return coreerrors.Conflict("Cet acte est déjà couvert par une PEC existante")
		}
		now := time.Now()
		item := InsuranceAuthorization{AuthorizationNumber: fmt.Sprintf("TMP-%d-%d", userID, now.UnixNano()), PatientID: req.PatientID, MedicalRecordID: record.ID, PatientCoverageID: cov.ID, InsuranceCompanyID: cov.CompanyID, GuarantorID: cov.GuarantorID, ReferenceType: typ, ReferenceID: req.ReferenceID, Service: strings.TrimSpace(req.Service), RequestedAmount: req.RequestedAmount, RequestedAt: now, RequestedBy: userID, Status: StatusDraft, Comment: strings.TrimSpace(req.Comment), CreatedBy: userID, UpdatedBy: userID}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		item.AuthorizationNumber = fmt.Sprintf("PEC-%06d", item.ID)
		if err := tx.Model(&item).Update("authorization_number", item.AuthorizationNumber).Error; err != nil {
			return err
		}
		createdID = item.ID
		return s.timeline(tx, &item, "insurance_authorization_created", "Demande de PEC créée", userID)
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(createdID)
}

func (s *Service) Update(id uint, req UpdateRequest, userID uint) (*Response, error) {
	if err := nonNegative(req.RequestedAmount, "Le montant demandé"); err != nil {
		return nil, err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if item.Status != StatusDraft {
			return coreerrors.Conflict("Seule une PEC en brouillon peut être modifiée")
		}
		if req.PatientCoverageID != 0 && req.PatientCoverageID != item.PatientCoverageID {
			cov, err := s.loadCoverage(tx, item.PatientID, req.PatientCoverageID)
			if err != nil {
				return err
			}
			item.PatientCoverageID = cov.ID
			item.InsuranceCompanyID = cov.CompanyID
			item.GuarantorID = cov.GuarantorID
		}
		item.Service = strings.TrimSpace(req.Service)
		item.RequestedAmount = req.RequestedAmount
		item.Comment = strings.TrimSpace(req.Comment)
		item.UpdatedBy = userID
		return tx.Save(&item).Error
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(id)
}

func (s *Service) Submit(id uint, req SubmitRequest, userID uint) (*Response, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if item.Status != StatusDraft {
			return coreerrors.Conflict("La PEC ne peut plus être envoyée")
		}
		at, err := parseDate(req.SubmittedAt)
		if err != nil {
			return err
		}
		now := time.Now()
		if at == nil {
			at = &now
		}
		item.Status = StatusSubmitted
		item.SubmittedAt = at
		item.SubmittedBy = &userID
		item.ExternalReference = strings.TrimSpace(req.ExternalReference)
		item.UpdatedBy = userID
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		return s.timeline(tx, &item, "insurance_authorization_submitted", "Demande de PEC envoyée", userID)
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(id)
}

func (s *Service) MarkPending(id uint, userID uint) (*Response, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if item.Status != StatusSubmitted {
			return coreerrors.Conflict("Seule une PEC envoyée peut être placée en attente")
		}
		item.Status = StatusPending
		item.UpdatedBy = userID
		return tx.Save(&item).Error
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(id)
}

func (s *Service) Decide(id uint, req DecisionRequest, userID uint) (*Response, error) {
	status := strings.ToUpper(strings.TrimSpace(req.Status))
	if !finalStatuses[status] {
		return nil, coreerrors.BadRequest("Décision finale invalide")
	}
	if strings.TrimSpace(req.ExternalReference) == "" {
		return nil, coreerrors.BadRequest("La référence assureur est obligatoire")
	}
	decisionDate, err := parseDate(req.ExternalDecisionDate)
	if err != nil || decisionDate == nil {
		return nil, coreerrors.BadRequest("La date de décision est obligatoire")
	}
	if err := nonNegative(req.PatientAmount, "La part patient"); err != nil {
		return nil, err
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if finalStatuses[item.Status] || item.Status == StatusCancelled {
			return coreerrors.Conflict("La décision finale est immuable")
		}
		if item.Status != StatusSubmitted && item.Status != StatusPending {
			return coreerrors.Conflict("La PEC doit être envoyée avant décision")
		}
		if item.RequestedAmount == nil {
			return coreerrors.BadRequest("Le montant demandé est requis pour enregistrer une décision")
		}
		insuranceAmount, patientAmount, calcErr := Calculate(*item.RequestedAmount, status, req.ApprovedRate, req.ApprovedAmount, req.CeilingAmount)
		if calcErr != nil {
			return calcErr
		}
		if req.PatientAmount != nil && math.Abs(*req.PatientAmount-patientAmount) > 0.01 {
			return coreerrors.Conflict("La part patient ne correspond pas au calcul centralisé")
		}
		if status == StatusRejected && strings.TrimSpace(req.RejectionReason) == "" {
			return coreerrors.BadRequest("Le motif de refus est obligatoire")
		}
		item.Status = status
		item.ExternalReference = strings.TrimSpace(req.ExternalReference)
		item.ExternalDecisionDate = decisionDate
		item.ApprovedRate = req.ApprovedRate
		item.ApprovedAmount = req.ApprovedAmount
		item.InsuranceAmount = &insuranceAmount
		item.PatientAmount = &patientAmount
		item.CeilingAmount = req.CeilingAmount
		item.RejectionReason = strings.TrimSpace(req.RejectionReason)
		item.Comment = strings.TrimSpace(req.Comment)
		item.DecidedBy = &userID
		item.UpdatedBy = userID
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		event := map[string]string{StatusApproved: "insurance_authorization_approved", StatusPartiallyApproved: "insurance_authorization_partially_approved", StatusRejected: "insurance_authorization_rejected"}[status]
		return s.timeline(tx, &item, event, "Décision assureur enregistrée", userID)
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(id)
}

func (s *Service) Cancel(id uint, userID uint) (*Response, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if finalStatuses[item.Status] || item.Status == StatusCancelled {
			return coreerrors.Conflict("Une décision finale ne peut pas être annulée")
		}
		item.Status = StatusCancelled
		item.UpdatedBy = userID
		if err := tx.Save(&item).Error; err != nil {
			return err
		}
		if err := tx.Model(&InsuranceAuthorizationAct{}).Where("insurance_authorization_id=? AND is_active", item.ID).Update("is_active", false).Error; err != nil {
			return err
		}
		return s.timeline(tx, &item, "insurance_authorization_cancelled", "Demande de PEC annulée", userID)
	})
	if err != nil {
		return nil, err
	}
	return s.FindByID(id)
}

func (s *Service) timeline(tx *gorm.DB, item *InsuranceAuthorization, event, title string, userID uint) error {
	ref := item.ID
	return tx.Create(&medical_records.MedicalTimelineEvent{MedicalRecordID: item.MedicalRecordID, PatientID: item.PatientID, EventType: event, Category: "insurance", Title: title, Description: item.AuthorizationNumber, ReferenceType: "insurance_authorization", ReferenceID: &ref, Severity: "info", EventDate: time.Now(), CreatedBy: userID}).Error
}

func (s *Service) FindByID(id uint) (*Response, error) {
	rows, _, err := s.query(ListQuery{}, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, coreerrors.NotFound("INSURANCE_AUTHORIZATION")
	}
	return &rows[0], nil
}

func (s *Service) FindAuthorizationForAct(patientID, coverageID uint, referenceType string, referenceID uint) (*ActMatch, error) {
	typ := strings.ToUpper(strings.TrimSpace(referenceType))
	if _, ok := allowedReferences[typ]; !ok {
		return nil, coreerrors.BadRequest("Type d'acte non pris en charge par le référentiel actuel")
	}
	var direct InsuranceAuthorization
	err := s.db.Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND status <> ?", patientID, coverageID, typ, referenceID, StatusCancelled).First(&direct).Error
	if err == nil {
		item, findErr := s.FindByID(direct.ID)
		return &ActMatch{MatchType: "DIRECT", Authorization: item}, findErr
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var link InsuranceAuthorizationAct
	err = s.db.Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND is_active", patientID, coverageID, typ, referenceID).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &ActMatch{MatchType: "NONE"}, nil
	}
	if err != nil {
		return nil, err
	}
	item, findErr := s.FindByID(link.InsuranceAuthorizationID)
	return &ActMatch{MatchType: "COVERED", Authorization: item}, findErr
}

func (s *Service) EligibleActs(patientID, coverageID uint, referenceType, search string) ([]EligibleAct, error) {
	typ := strings.ToUpper(strings.TrimSpace(referenceType))
	if _, ok := allowedReferences[typ]; !ok {
		return nil, coreerrors.BadRequest("Type d'acte non pris en charge par le référentiel actuel")
	}
	var patientCount int64
	if err := s.db.Table("patients").Where("id=?", patientID).Count(&patientCount).Error; err != nil {
		return nil, err
	}
	if patientCount == 0 {
		return nil, coreerrors.NotFound("PATIENT")
	}
	if _, err := s.loadCoverage(s.db, patientID, coverageID); err != nil {
		return nil, err
	}
	var rows []EligibleAct
	queries := map[string]string{
		"CONSULTATION":    `SELECT 'CONSULTATION' reference_type,id reference_id,('#' || id || ' — ' || COALESCE(NULLIF(service,''),'Consultation')) label,COALESCE(doctor_name,'') secondary_label,created_at::text date,status FROM consultations WHERE patient_id=?`,
		"LABORATORY":      `SELECT 'LABORATORY' reference_type,o.id reference_id,(o.request_number || ' — ' || e.name) label,COALESCE(e.category,'') secondary_label,o.created_at::text date,o.status FROM laboratory_orders o JOIN medical_exams e ON e.id=o.medical_exam_id WHERE o.patient_id=?`,
		"IMAGING":         `SELECT 'IMAGING' reference_type,o.id reference_id,(o.order_number || ' — ' || e.name) label,COALESCE(o.modality,'') secondary_label,o.created_at::text date,o.status FROM imaging_orders o JOIN medical_exams e ON e.id=o.medical_exam_id WHERE o.patient_id=?`,
		"HOSPITALIZATION": `SELECT 'HOSPITALIZATION' reference_type,id reference_id,(admission_number || ' — ' || COALESCE(NULLIF(department,''),'Hospitalisation')) label,COALESCE(admission_reason,'') secondary_label,created_at::text date,status FROM hospitalizations WHERE patient_id=?`,
		"MEDICATION":      `SELECT 'MEDICATION' reference_type,cp.id reference_id,('Prescription #' || cp.id || ' — ' || cp.medication_name || CASE WHEN cp.dosage='' THEN '' ELSE ' ' || cp.dosage END) label,('Consultation #' || cp.consultation_id) secondary_label,cp.created_at::text date,'PRESCRIBED' status FROM consultation_prescriptions cp JOIN consultations c ON c.id=cp.consultation_id WHERE c.patient_id=?`,
	}
	if err := s.db.Raw(queries[typ], patientID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(search))
	result := make([]EligibleAct, 0, len(rows))
	for _, row := range rows {
		if needle != "" && !strings.Contains(strings.ToLower(row.Label+" "+row.SecondaryLabel), needle) {
			continue
		}
		match, err := s.FindAuthorizationForAct(patientID, coverageID, typ, row.ReferenceID)
		if err != nil {
			return nil, err
		}
		row.AuthorizationResolution = match.MatchType
		if match.Authorization != nil {
			row.ExistingAuthorizationID = match.Authorization.ID
			row.ExistingAuthorizationNumber = match.Authorization.AuthorizationNumber
		}
		result = append(result, row)
	}
	return result, nil
}

func (s *Service) LinkAct(id uint, req ActRequest, userID uint) (*ActResponse, error) {
	var result ActResponse
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var item InsuranceAuthorization
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, id).Error; err != nil {
			return coreerrors.NotFound("INSURANCE_AUTHORIZATION")
		}
		if item.Status == StatusCancelled {
			return coreerrors.Conflict("Une PEC annulée ne peut pas couvrir un nouvel acte")
		}
		typ, err := s.validateReference(tx, item.PatientID, req.ReferenceType, req.ReferenceID)
		if err != nil {
			return err
		}
		lock := fnv.New64a()
		_, _ = fmt.Fprintf(lock, "%d:%d:%s:%d", item.PatientID, item.PatientCoverageID, typ, req.ReferenceID)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(lock.Sum64())).Error; err != nil {
			return err
		}
		var direct int64
		if err := tx.Model(&InsuranceAuthorization{}).Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND status <> ?", item.PatientID, item.PatientCoverageID, typ, req.ReferenceID, StatusCancelled).Count(&direct).Error; err != nil {
			return err
		}
		if direct > 0 {
			return coreerrors.Conflict("Cet acte possède déjà sa propre PEC")
		}
		var existing InsuranceAuthorizationAct
		err = tx.Where("patient_id=? AND patient_coverage_id=? AND reference_type=? AND reference_id=? AND is_active", item.PatientID, item.PatientCoverageID, typ, req.ReferenceID).First(&existing).Error
		if err == nil {
			if existing.InsuranceAuthorizationID != item.ID {
				return coreerrors.Conflict("Cet acte est déjà couvert par une autre PEC")
			}
			result = ActResponse{InsuranceAuthorizationAct: existing}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		link := InsuranceAuthorizationAct{InsuranceAuthorizationID: item.ID, PatientID: item.PatientID, PatientCoverageID: item.PatientCoverageID, ReferenceType: typ, ReferenceID: req.ReferenceID, RelationType: RelationCovered, IsActive: true, CreatedBy: userID}
		if err := tx.Create(&link).Error; err != nil {
			return coreerrors.Conflict("Cet acte est déjà couvert par une PEC")
		}
		result = ActResponse{InsuranceAuthorizationAct: link}
		ref := item.ID
		description := fmt.Sprintf("%s couvre %s #%d", item.AuthorizationNumber, typ, req.ReferenceID)
		return tx.Create(&medical_records.MedicalTimelineEvent{MedicalRecordID: item.MedicalRecordID, PatientID: item.PatientID, EventType: "insurance_authorization_act_linked", Category: "insurance", Title: "Acte rattaché à une PEC", Description: description, ReferenceType: "insurance_authorization", ReferenceID: &ref, Severity: "info", EventDate: time.Now(), CreatedBy: userID}).Error
	})
	if err != nil {
		return nil, err
	}
	label, err := s.referenceLabel(result.ReferenceType, result.ReferenceID)
	if err != nil {
		return nil, err
	}
	result.ReferenceLabel = label
	return &result, nil
}

func (s *Service) referenceLabel(typ string, id uint) (string, error) {
	var label string
	var query *gorm.DB
	switch typ {
	case "IMAGING":
		query = s.db.Table("imaging_orders o").Select("e.name").Joins("JOIN medical_exams e ON e.id=o.medical_exam_id").Where("o.id=?", id).Scan(&label)
	case "LABORATORY":
		query = s.db.Table("laboratory_orders o").Select("e.name").Joins("JOIN medical_exams e ON e.id=o.medical_exam_id").Where("o.id=?", id).Scan(&label)
	default:
		label = fmt.Sprintf("%s #%d", typ, id)
		return label, nil
	}
	if query.Error != nil {
		return "", query.Error
	}
	if label == "" {
		label = fmt.Sprintf("%s #%d", typ, id)
	}
	return label, nil
}

func (s *Service) loadCoveredActs(rows []Response) error {
	for i := range rows {
		var links []InsuranceAuthorizationAct
		if err := s.db.Where("insurance_authorization_id=? AND is_active", rows[i].ID).Order("id").Find(&links).Error; err != nil {
			return err
		}
		rows[i].CoveredActs = make([]ActResponse, 0, len(links))
		for _, link := range links {
			label, err := s.referenceLabel(link.ReferenceType, link.ReferenceID)
			if err != nil {
				return err
			}
			rows[i].CoveredActs = append(rows[i].CoveredActs, ActResponse{InsuranceAuthorizationAct: link, ReferenceLabel: label})
		}
	}
	return nil
}
func (s *Service) List(q ListQuery) (*Page, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 100 {
		q.PageSize = 20
	}
	rows, total, err := s.query(q, 0)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []Response{}
	}
	return &Page{Items: rows, Page: q.Page, PageSize: q.PageSize, Total: total, TotalPages: int((total + int64(q.PageSize) - 1) / int64(q.PageSize))}, nil
}

func (s *Service) query(q ListQuery, id uint) ([]Response, int64, error) {
	base := s.db.Table("insurance_authorizations a").Joins("JOIN patients p ON p.id=a.patient_id").Joins("JOIN patient_coverages c ON c.id=a.patient_coverage_id").Joins("JOIN insurance_companies co ON co.id=a.insurance_company_id").Joins("JOIN insurance_guarantors g ON g.id=a.guarantor_id").Joins("LEFT JOIN imaging_orders io ON a.reference_type='IMAGING' AND io.id=a.reference_id").Joins("LEFT JOIN medical_exams ime ON ime.id=io.medical_exam_id").Joins("LEFT JOIN laboratory_orders lo ON a.reference_type='LABORATORY' AND lo.id=a.reference_id").Joins("LEFT JOIN medical_exams lme ON lme.id=lo.medical_exam_id")
	if id > 0 {
		base = base.Where("a.id=?", id)
	}
	if q.PatientID > 0 {
		base = base.Where("a.patient_id=?", q.PatientID)
	}
	if q.CompanyID > 0 {
		base = base.Where("a.insurance_company_id=?", q.CompanyID)
	}
	if q.Status != "" {
		base = base.Where("a.status=?", strings.ToUpper(q.Status))
	}
	if q.ReferenceType != "" {
		base = base.Where("a.reference_type=?", strings.ToUpper(q.ReferenceType))
	}
	if q.Service != "" {
		base = base.Where("a.service=?", q.Service)
	}
	if q.Search != "" {
		like := "%" + strings.ToLower(q.Search) + "%"
		base = base.Where("LOWER(a.authorization_number) LIKE ? OR LOWER(a.external_reference) LIKE ? OR LOWER(p.nom || ' ' || p.prenoms) LIKE ? OR LOWER(p.code_patient) LIKE ?", like, like, like, like)
	}
	if q.DateFrom != "" {
		base = base.Where("a.requested_at>=?", q.DateFrom)
	}
	if q.DateTo != "" {
		base = base.Where("a.requested_at<?::date + interval '1 day'", q.DateTo)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []Response
	selectSQL := "a.*, TRIM(p.nom || ' ' || p.prenoms) patient_name, p.code_patient patient_code, co.name company_name, c.member_number, c.coverage_rate contract_rate, g.name guarantor_name, COALESCE(ime.name, lme.name, a.reference_type || ' #' || a.reference_id) reference_label"
	query := base.Select(selectSQL).Order("a.created_at DESC")
	if id == 0 {
		query = query.Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	if err := s.loadCoveredActs(rows); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func IsConflict(err error) bool {
	var appErr *coreerrors.AppError
	return errors.As(err, &appErr) && appErr.Status == 409
}
