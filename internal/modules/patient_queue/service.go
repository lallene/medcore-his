package patient_queue

import (
	"fmt"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) has(a Access, p string) bool { return a.Has(p) }

func (s *Service) scopeServiceIDs(a Access) ([]uint, error) {
	if a.Has("queue.read.all") || a.Has("*") {
		return nil, nil // unrestricted
	}
	ids := map[uint]bool{}
	if a.ServiceID != nil {
		ids[*a.ServiceID] = true
	}
	var assigned []uint
	s.db.Raw(`SELECT service_id FROM staff_service_assignments WHERE profile_id IN (
		SELECT id FROM staff_profiles WHERE user_id=? AND active) AND active`, a.UserID).Scan(&assigned)
	for _, id := range assigned {
		ids[id] = true
	}
	if len(ids) == 0 {
		return nil, coreerrors.Forbidden("Aucun service assigné pour la file patient")
	}
	out := make([]uint, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out, nil
}

func (s *Service) applyServiceScope(q *gorm.DB, a Access, col string) (*gorm.DB, error) {
	ids, err := s.scopeServiceIDs(a)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return q, nil
	}
	return q.Where(col+" IN ?", ids), nil
}

// assertCanAccessService enforces service perimeter for reads/mutations.
// queue.read.all / * → allowed. No assigned service → Forbidden (never global fallback).
// Out-of-scope → NotFound to avoid leaking resource existence (ticketing convention).
func (s *Service) assertCanAccessService(serviceID uint, a Access) error {
	if a.Has("queue.read.all") || a.Has("*") {
		return nil
	}
	ids, err := s.scopeServiceIDs(a)
	if err != nil {
		return err
	}
	if ids == nil {
		return nil
	}
	for _, id := range ids {
		if id == serviceID {
			return nil
		}
	}
	return coreerrors.NotFound("Ticket")
}

func (s *Service) assertCanAccessTicket(t Ticket, a Access) error {
	return s.assertCanAccessService(t.ServiceID, a)
}

// isDoctorOnlyReader is true when the actor has physician queue read without
// reception/triage/global overrides. Such actors may only see post-triage stages.
func (s *Service) isDoctorOnlyReader(a Access) bool {
	if a.Has("queue.read.all") || a.Has("*") {
		return false
	}
	if a.Has("queue.reception.read") || a.Has("queue.triage.read") {
		return false
	}
	return a.Has("queue.doctor.read")
}

// doctorReadableStages — WAITING_DOCTOR / DOCTOR_IN_PROGRESS (+ COMPLETED for history).
var doctorReadableStages = []string{StageWaitingDoctor, StageDoctorInProgress, StageCompleted}

func doctorCanReadStage(stage string) bool {
	for _, s := range doctorReadableStages {
		if s == stage {
			return true
		}
	}
	return false
}

func isPreTriageStage(stage string) bool {
	return stage == StageReception || stage == StageWaitingTriage || stage == StageTriageInProgress
}

// assertDoctorStageReadable rejects pre-triage stages for doctor-only readers (404, no leak).
func (s *Service) assertDoctorStageReadable(t Ticket, a Access) error {
	if !s.isDoctorOnlyReader(a) {
		return nil
	}
	if doctorCanReadStage(t.Stage) {
		return nil
	}
	return coreerrors.NotFound("Ticket")
}

// applyDoctorListStageFilter forces post-triage stages for doctor-only List queries.
// Explicit pre-triage stage filters are refused with 400 (not silently emptied).
func (s *Service) applyDoctorListStageFilter(q *gorm.DB, f Filter, a Access) (*gorm.DB, error) {
	if !s.isDoctorOnlyReader(a) {
		if f.Stage != "" {
			return q.Where("stage = ?", f.Stage), nil
		}
		return q, nil
	}
	if f.Stage != "" {
		if isPreTriageStage(f.Stage) || !doctorCanReadStage(f.Stage) {
			return nil, coreerrors.BadRequest("Étape non autorisée pour la lecture médecin")
		}
		return q.Where("stage = ?", f.Stage), nil
	}
	// Default: active post-triage only (COMPLETED requires an explicit stage/status filter).
	return q.Where("stage IN ?", []string{StageWaitingDoctor, StageDoctorInProgress}), nil
}

func (s *Service) loadTicketForMutation(id uint, a Access) (*Ticket, error) {
	var t Ticket
	if err := s.db.First(&t, id).Error; err != nil {
		return nil, coreerrors.NotFound("Ticket")
	}
	if err := s.assertCanAccessTicket(t, a); err != nil {
		return nil, err
	}
	return &t, nil
}

// serviceScopeSQL returns an optional " AND <col> IN ?" fragment and args for raw KPI queries.
func (s *Service) serviceScopeSQL(a Access, col string) (string, []any, error) {
	ids, err := s.scopeServiceIDs(a)
	if err != nil {
		return "", nil, err
	}
	if ids == nil {
		return "", nil, nil
	}
	return " AND " + col + " IN ?", []any{ids}, nil
}

func (s *Service) EvaluateFinance(patientID uint) (string, error) {
	var bal int64
	err := s.db.Raw(`
		SELECT COALESCE(SUM(GREATEST(i.patient_amount - COALESCE(pay.paid,0), 0)), 0)
		FROM billing_invoices i
		LEFT JOIN (
			SELECT invoice_id, SUM(amount) paid FROM billing_payments GROUP BY invoice_id
		) pay ON pay.invoice_id = i.id
		WHERE i.patient_id = ? AND i.status IN ('ISSUED','PARTIALLY_PAID','OVERDUE')
	`, patientID).Scan(&bal).Error
	if err != nil {
		return "", coreerrors.Internal(err.Error())
	}
	var pending int64
	_ = s.db.Raw(`
		SELECT COUNT(*) FROM billing_invoices
		WHERE patient_id = ? AND coverage_pending = true AND status NOT IN ('CANCELLED','VOID')
	`, patientID).Scan(&pending)
	if pending > 0 && bal == 0 {
		return FinanceInsurancePending, nil
	}
	if bal > 0 {
		return FinancePaymentRequired, nil
	}
	return FinanceClear, nil
}

func (s *Service) nextReference(tx *gorm.DB) (string, error) {
	year := time.Now().UTC().Year()
	key := fmt.Sprintf("patient-queue-%d", year)
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", key).Error; err != nil {
		return "", err
	}
	prefix := fmt.Sprintf("Q-%d-", year)
	var n int64
	if err := tx.Model(&Ticket{}).Where("reference LIKE ?", prefix+"%").Count(&n).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%06d", prefix, n+1), nil
}

func (s *Service) writeHistory(tx *gorm.DB, ticketID, actor uint, from, to, event, reason string) error {
	h := History{
		TicketID:    ticketID,
		ActorUserID: actor,
		FromStage:   from,
		ToStage:     to,
		EventType:   event,
		Reason:      reason,
		CreatedAt:   time.Now().UTC(),
	}
	return tx.Create(&h).Error
}

func (s *Service) CreateAppointment(r CreateAppointmentRequest, a Access) (*Appointment, error) {
	if r.PatientID == 0 || r.ServiceID == 0 {
		return nil, coreerrors.BadRequest("patient et service requis")
	}
	if err := s.assertServiceInScope(r.ServiceID, a); err != nil {
		return nil, err
	}
	appt := Appointment{
		PatientID:        r.PatientID,
		ServiceID:        r.ServiceID,
		ExpectedDoctorID: r.ExpectedDoctorID,
		ScheduledAt:      r.ScheduledAt.UTC(),
		Reason:           strings.TrimSpace(r.Reason),
		Status:           ApptScheduled,
		CreatedBy:        a.UserID,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	if err := s.db.Create(&appt).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return &appt, nil
}

// assertServiceInScope is used when the actor explicitly chooses a serviceId (walk-in / create RDV).
func (s *Service) assertServiceInScope(serviceID uint, a Access) error {
	if a.Has("queue.read.all") || a.Has("*") {
		return nil
	}
	ids, err := s.scopeServiceIDs(a)
	if err != nil {
		return err
	}
	if ids == nil {
		return nil
	}
	for _, id := range ids {
		if id == serviceID {
			return nil
		}
	}
	return coreerrors.Forbidden("Service hors périmètre")
}

func (s *Service) ListAppointmentsToday(a Access, page, limit int) (*AppointmentListResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	start := time.Now().UTC().Truncate(24 * time.Hour)
	end := start.Add(24 * time.Hour)
	q := s.db.Model(&Appointment{}).Where("scheduled_at >= ? AND scheduled_at < ?", start, end)
	q, err := s.applyServiceScope(q, a, "service_id")
	if err != nil {
		return nil, err
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	var rows []Appointment
	if err := q.Order("scheduled_at ASC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	items := make([]AppointmentDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.enrichAppointment(row))
	}
	return &AppointmentListResponse{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func (s *Service) enrichAppointment(a Appointment) AppointmentDTO {
	d := AppointmentDTO{Appointment: a}
	_ = s.db.Raw(`SELECT code_patient FROM patients WHERE id=?`, a.PatientID).Scan(&d.PatientCode)
	_ = s.db.Raw(`SELECT TRIM(CONCAT(prenoms,' ',nom)) FROM patients WHERE id=?`, a.PatientID).Scan(&d.PatientName)
	_ = s.db.Raw(`SELECT name FROM organization_services WHERE id=?`, a.ServiceID).Scan(&d.ServiceName)
	if a.ExpectedDoctorID != nil {
		_ = s.db.Raw(`SELECT COALESCE(name,'') FROM users WHERE id=?`, *a.ExpectedDoctorID).Scan(&d.ExpectedDoctorName)
	}
	if a.ArrivedAt != nil {
		d.Punctuality = Punctuality(a.ScheduledAt, *a.ArrivedAt)
	}
	d.HasActiveTicket = a.QueueTicketID != nil
	return d
}

func (s *Service) MarkNoShow(appointmentID uint, actor uint, a Access) error {
	if !s.has(a, "queue.checkin") && !s.has(a, "queue.cancel") && !s.has(a, "*") {
		return coreerrors.Forbidden("Permission refusée")
	}
	var appt Appointment
	if err := s.db.First(&appt, appointmentID).Error; err != nil {
		return coreerrors.NotFound("Rendez-vous")
	}
	if err := s.assertCanAccessService(appt.ServiceID, a); err != nil {
		return coreerrors.NotFound("Rendez-vous")
	}
	if appt.Status == ApptCheckedIn || appt.QueueTicketID != nil {
		return coreerrors.Conflict("Rendez-vous déjà check-in")
	}
	now := time.Now().UTC()
	appt.Status = ApptNoShow
	appt.UpdatedAt = now
	return s.db.Save(&appt).Error
}

func (s *Service) CheckInAppointment(appointmentID uint, r AppointmentCheckInRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.checkin") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission check-in requise")
	}
	if !r.IdentityConfirmed {
		return nil, coreerrors.BadRequest("Confirmation d'identité requise")
	}
	var out *Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var appt Appointment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&appt, appointmentID).Error; err != nil {
			return coreerrors.NotFound("Rendez-vous")
		}
		if err := s.assertCanAccessService(appt.ServiceID, a); err != nil {
			return coreerrors.NotFound("Rendez-vous")
		}
		if appt.QueueTicketID != nil || appt.Status == ApptCheckedIn {
			return coreerrors.Conflict("Check-in déjà effectué pour ce rendez-vous")
		}
		var active int64
		tx.Model(&Ticket{}).Where("patient_id=? AND status=?", appt.PatientID, StatusActive).Count(&active)
		if active > 0 {
			return coreerrors.Conflict("Le patient a déjà un parcours actif")
		}
		fin, err := s.EvaluateFinance(appt.PatientID)
		if err != nil {
			return err
		}
		if (fin == FinancePaymentRequired || fin == FinanceBlocked) && !r.FinanceOverride {
			return coreerrors.Conflict("Paiement requis avant check-in (finance=" + fin + ")")
		}
		if r.FinanceOverride && !s.has(a, "queue.checkin") && !s.has(a, "*") {
			return coreerrors.Forbidden("Override finance non autorisé")
		}
		prio := r.Priority
		if prio == "" {
			prio = PriorityNormal
		}
		if PriorityRank(prio) == 99 {
			return coreerrors.BadRequest("Priorité invalide")
		}
		ref, err := s.nextReference(tx)
		if err != nil {
			return coreerrors.Internal(err.Error())
		}
		now := time.Now().UTC()
		arrived := now
		if appt.ArrivedAt != nil {
			arrived = *appt.ArrivedAt
		} else {
			appt.ArrivedAt = &now
			appt.Status = ApptArrived
		}
		t := Ticket{
			Reference:           ref,
			PatientID:           appt.PatientID,
			AppointmentID:       &appt.ID,
			Source:              SourceAppointment,
			ServiceID:           appt.ServiceID,
			ExpectedDoctorID:    appt.ExpectedDoctorID,
			ArrivedAt:           arrived,
			CheckedInAt:         now,
			Stage:               StageWaitingTriage,
			Status:              StatusActive,
			Priority:            prio,
			FinanceStatus:       fin,
			FinanceOverride:     r.FinanceOverride,
			FinanceOverrideNote: r.FinanceOverrideNote,
			IdentityConfirmed:   true,
			Version:             1,
			CreatedBy:           a.UserID,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := tx.Create(&t).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		appt.Status = ApptCheckedIn
		appt.CheckedInAt = &now
		appt.QueueTicketID = &t.ID
		appt.UpdatedAt = now
		if err := tx.Save(&appt).Error; err != nil {
			return err
		}
		if err := s.writeHistory(tx, t.ID, a.UserID, StageReception, StageWaitingTriage, "CHECK_IN", "appointment"); err != nil {
			return err
		}
		if r.FinanceOverride {
			_ = s.writeHistory(tx, t.ID, a.UserID, StageReception, StageWaitingTriage, "FINANCE_OVERRIDE", r.FinanceOverrideNote)
		}
		out = &t
		return nil
	})
	return out, err
}

func (s *Service) CheckInWalkIn(r WalkInCheckInRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.checkin") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission check-in requise")
	}
	if !r.IdentityConfirmed {
		return nil, coreerrors.BadRequest("Confirmation d'identité requise")
	}
	if r.PatientID == 0 || r.ServiceID == 0 {
		return nil, coreerrors.BadRequest("patient et service requis")
	}
	if err := s.assertServiceInScope(r.ServiceID, a); err != nil {
		return nil, err
	}
	var out *Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var active int64
		tx.Model(&Ticket{}).Where("patient_id=? AND status=?", r.PatientID, StatusActive).Count(&active)
		if active > 0 {
			return coreerrors.Conflict("Le patient a déjà un parcours actif")
		}
		fin, err := s.EvaluateFinance(r.PatientID)
		if err != nil {
			return err
		}
		if (fin == FinancePaymentRequired || fin == FinanceBlocked) && !r.FinanceOverride {
			return coreerrors.Conflict("Paiement requis avant check-in (finance=" + fin + ")")
		}
		prio := r.Priority
		if prio == "" {
			prio = PriorityNormal
		}
		ref, err := s.nextReference(tx)
		if err != nil {
			return coreerrors.Internal(err.Error())
		}
		now := time.Now().UTC()
		t := Ticket{
			Reference:           ref,
			PatientID:           r.PatientID,
			Source:              SourceWalkIn,
			ServiceID:           r.ServiceID,
			ExpectedDoctorID:    r.ExpectedDoctorID,
			ArrivedAt:           now,
			CheckedInAt:         now,
			Stage:               StageWaitingTriage,
			Status:              StatusActive,
			Priority:            prio,
			FinanceStatus:       fin,
			FinanceOverride:     r.FinanceOverride,
			FinanceOverrideNote: r.FinanceOverrideNote,
			IdentityConfirmed:   true,
			Version:             1,
			CreatedBy:           a.UserID,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := tx.Create(&t).Error; err != nil {
			return coreerrors.Internal(err.Error())
		}
		if err := s.writeHistory(tx, t.ID, a.UserID, StageReception, StageWaitingTriage, "CHECK_IN", "walk_in:"+r.Reason); err != nil {
			return err
		}
		out = &t
		return nil
	})
	return out, err
}

func (s *Service) List(f Filter, a Access) (*ListResponse, error) {
	if !s.has(a, "queue.reception.read") && !s.has(a, "queue.triage.read") && !s.has(a, "queue.doctor.read") &&
		!s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture file refusée")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 50
	}
	q := s.db.Model(&Ticket{})
	q, err := s.applyServiceScope(q, a, "service_id")
	if err != nil {
		return nil, err
	}
	q, err = s.applyDoctorListStageFilter(q, f, a)
	if err != nil {
		return nil, err
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	} else {
		q = q.Where("status = ?", StatusActive)
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	if f.Service != "" {
		q = q.Where("service_id = ?", f.Service)
	}
	if f.Doctor != "" {
		q = q.Where("expected_doctor_id = ?", f.Doctor)
	}
	if f.Search != "" {
		like := "%" + strings.TrimSpace(f.Search) + "%"
		q = q.Where(`patient_id IN (SELECT id FROM patients WHERE code_patient ILIKE ? OR nom ILIKE ? OR prenoms ILIKE ?) OR reference ILIKE ?`, like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	var rows []Ticket
	// Deterministic order: priority rank, then arrived_at
	order := `CASE priority WHEN 'URGENT' THEN 1 WHEN 'HIGH' THEN 2 WHEN 'NORMAL' THEN 3 WHEN 'LOW' THEN 4 ELSE 99 END ASC, arrived_at ASC`
	if err := q.Order(order).Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	items := make([]TicketDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.enrichTicket(row))
	}
	return &ListResponse{Items: items, Total: total, Page: f.Page, Limit: f.Limit}, nil
}

func (s *Service) enrichTicket(t Ticket) TicketDTO {
	d := TicketDTO{Ticket: t}
	var demog struct {
		Code      string
		Name      string
		Sex       string
		Phone     string
		Dob       *time.Time
	}
	_ = s.db.Raw(`
		SELECT code_patient AS code,
			TRIM(CONCAT(COALESCE(prenoms,''),' ',COALESCE(nom,''))) AS name,
			COALESCE(sexe,'') AS sex,
			COALESCE(telephone,'') AS phone,
			date_naissance AS dob
		FROM patients WHERE id=?`, t.PatientID).Scan(&demog)
	d.PatientCode = demog.Code
	d.PatientName = strings.TrimSpace(demog.Name)
	d.PatientSex = demog.Sex
	d.PatientPhone = demog.Phone
	if demog.Dob != nil && !demog.Dob.IsZero() {
		iso := demog.Dob.Format("2006-01-02")
		d.PatientDob = &iso
		years := int(time.Now().UTC().Sub(*demog.Dob).Hours() / (24 * 365.25))
		if years >= 0 && years < 150 {
			d.PatientAgeYears = &years
		}
	}
	_ = s.db.Raw(`SELECT name FROM organization_services WHERE id=?`, t.ServiceID).Scan(&d.ServiceName)
	if t.ExpectedDoctorID != nil {
		_ = s.db.Raw(`SELECT COALESCE(name,'') FROM users WHERE id=?`, *t.ExpectedDoctorID).Scan(&d.ExpectedDoctorName)
	}
	if t.DoctorTakenBy != nil {
		_ = s.db.Raw(`SELECT COALESCE(name,'') FROM users WHERE id=?`, *t.DoctorTakenBy).Scan(&d.DoctorTakenByName)
	}
	end := time.Now().UTC()
	if t.Stage == StageCompleted || t.Status == StatusCompleted {
		end = t.UpdatedAt
	}
	d.WaitMinutes = int(end.Sub(t.ArrivedAt).Minutes())
	if t.AppointmentID != nil {
		var appt struct {
			Scheduled time.Time
			Reason    string
		}
		_ = s.db.Raw(`SELECT scheduled_at AS scheduled, COALESCE(reason,'') AS reason FROM patient_queue_appointments WHERE id=?`, *t.AppointmentID).Scan(&appt)
		if !appt.Scheduled.IsZero() {
			iso := appt.Scheduled.UTC().Format(time.RFC3339)
			d.AppointmentTime = &iso
			d.Punctuality = Punctuality(appt.Scheduled, t.ArrivedAt)
		}
		d.Reason = strings.TrimSpace(appt.Reason)
	}
	if d.Reason == "" {
		var histReason string
		_ = s.db.Raw(`
			SELECT reason FROM patient_queue_history
			WHERE ticket_id=? AND event_type='CHECK_IN'
			ORDER BY created_at ASC LIMIT 1`, t.ID).Scan(&histReason)
		histReason = strings.TrimSpace(histReason)
		if strings.HasPrefix(histReason, "walk_in:") {
			d.Reason = strings.TrimSpace(strings.TrimPrefix(histReason, "walk_in:"))
		} else if histReason != "" && histReason != "appointment" {
			d.Reason = histReason
		}
	}
	if t.VitalSignsID != nil {
		d.VitalSigns = s.loadVitalSummary(*t.VitalSignsID)
	}
	return d
}

func (s *Service) loadVitalSummary(id uint) *VitalSummary {
	var row struct {
		ID               uint
		TemperatureC     *float64
		SystolicBP       *int
		DiastolicBP      *int
		HeartRate        *int
		OxygenSaturation *float64
		WeightKg         *float64
		HeightCm         *float64
		MeasuredAt       *time.Time
	}
	err := s.db.Raw(`
		SELECT id, temperature_c, systolic_bp, diastolic_bp, heart_rate,
			oxygen_saturation, weight_kg, height_cm, measured_at
		FROM vital_signs WHERE id=?`, id).Scan(&row).Error
	if err != nil || row.ID == 0 {
		return nil
	}
	v := &VitalSummary{
		ID:               row.ID,
		TemperatureC:     row.TemperatureC,
		SystolicBP:       row.SystolicBP,
		DiastolicBP:      row.DiastolicBP,
		HeartRate:        row.HeartRate,
		OxygenSaturation: row.OxygenSaturation,
		WeightKg:         row.WeightKg,
		HeightCm:         row.HeightCm,
	}
	if row.MeasuredAt != nil {
		iso := row.MeasuredAt.UTC().Format(time.RFC3339)
		v.MeasuredAt = &iso
	}
	if row.TemperatureC != nil && (*row.TemperatureC >= 38.0 || *row.TemperatureC <= 35.0) {
		v.AbnormalTemp = true
	}
	if (row.SystolicBP != nil && (*row.SystolicBP >= 140 || *row.SystolicBP <= 90)) ||
		(row.DiastolicBP != nil && *row.DiastolicBP >= 90) {
		v.AbnormalBP = true
	}
	if row.HeartRate != nil && (*row.HeartRate >= 100 || *row.HeartRate <= 50) {
		v.AbnormalHR = true
	}
	if row.OxygenSaturation != nil && *row.OxygenSaturation < 95 {
		v.AbnormalSpO2 = true
	}
	return v
}

func (s *Service) loadClinicalSnippets(patientID uint) (allergies, histories []ClinicalSnippet) {
	type allergyRow struct {
		Name     string
		Severity string
	}
	var al []allergyRow
	_ = s.db.Raw(`
		SELECT allergen_name AS name, COALESCE(severity,'') AS severity
		FROM allergies WHERE patient_id=? AND is_active = true
		ORDER BY updated_at DESC LIMIT 8`, patientID).Scan(&al)
	for _, a := range al {
		allergies = append(allergies, ClinicalSnippet{Label: a.Name, Severity: a.Severity})
	}
	type histRow struct {
		Title string
	}
	var hs []histRow
	_ = s.db.Raw(`
		SELECT title FROM medical_histories
		WHERE patient_id=? AND COALESCE(status,'active') IN ('active','')
		ORDER BY updated_at DESC LIMIT 8`, patientID).Scan(&hs)
	for _, h := range hs {
		histories = append(histories, ClinicalSnippet{Label: h.Title})
	}
	return allergies, histories
}

// DoctorWorklist returns only post-triage tickets (WAITING_DOCTOR / DOCTOR_IN_PROGRESS).
// Triage stages are never included — server-side guarantee for the physician worklist.
func (s *Service) DoctorWorklist(f Filter, a Access) (*DoctorWorklistResponse, error) {
	if !s.has(a, "queue.doctor.read") && !s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture file médecin refusée")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 50
	}
	q := s.db.Model(&Ticket{}).Where("status = ? AND stage IN ?", StatusActive, []string{StageWaitingDoctor, StageDoctorInProgress})
	q, err := s.applyServiceScope(q, a, "service_id")
	if err != nil {
		return nil, err
	}
	if f.Priority != "" {
		q = q.Where("priority = ?", f.Priority)
	}
	if f.Stage != "" {
		if f.Stage != StageWaitingDoctor && f.Stage != StageDoctorInProgress {
			return nil, coreerrors.BadRequest("Étape non autorisée sur la file médecin")
		}
		q = q.Where("stage = ?", f.Stage)
	}
	if f.Search != "" {
		like := "%" + strings.TrimSpace(f.Search) + "%"
		q = q.Where(`patient_id IN (SELECT id FROM patients WHERE code_patient ILIKE ? OR nom ILIKE ? OR prenoms ILIKE ?) OR reference ILIKE ?`, like, like, like, like)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	var rows []Ticket
	order := `CASE priority WHEN 'URGENT' THEN 1 WHEN 'HIGH' THEN 2 WHEN 'NORMAL' THEN 3 WHEN 'LOW' THEN 4 ELSE 99 END ASC, arrived_at ASC`
	if err := q.Order(order).Offset((f.Page - 1) * f.Limit).Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	items := make([]TicketDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.enrichTicket(row))
	}
	kpis, err := s.doctorWorklistKPIs(a)
	if err != nil {
		return nil, err
	}
	return &DoctorWorklistResponse{Items: items, Total: total, Page: f.Page, Limit: f.Limit, KPIs: *kpis}, nil
}

func (s *Service) doctorWorklistKPIs(a Access) (*DoctorWorklistKPIs, error) {
	base := s.db.Model(&Ticket{}).Where("status = ?", StatusActive)
	base, err := s.applyServiceScope(base, a, "service_id")
	if err != nil {
		return nil, err
	}
	k := &DoctorWorklistKPIs{}
	q := base.Session(&gorm.Session{})
	q.Where("stage = ?", StageWaitingDoctor).Count(&k.ToTreat)
	q = base.Session(&gorm.Session{})
	q.Where("stage = ? AND priority = ?", StageWaitingDoctor, PriorityUrgent).Count(&k.Urgent)
	q = base.Session(&gorm.Session{})
	q.Where("stage = ?", StageDoctorInProgress).Count(&k.InConsultation)

	scopeSQL, scopeArgs, err := s.serviceScopeSQL(a, "service_id")
	if err != nil {
		return nil, err
	}
	avgSQL := `
		SELECT AVG(EXTRACT(EPOCH FROM (NOW() - arrived_at))/60.0)
		FROM patient_queue_tickets
		WHERE status=? AND stage=?` + scopeSQL
	avgArgs := append([]any{StatusActive, StageWaitingDoctor}, scopeArgs...)
	var avg *float64
	s.db.Raw(avgSQL, avgArgs...).Scan(&avg)
	if avg != nil {
		k.AvgWaitMinutes = *avg
	}
	start := time.Now().UTC().Truncate(24 * time.Hour)
	done := s.db.Model(&Ticket{}).Where("status=? AND stage=? AND updated_at >= ?", StatusCompleted, StageCompleted, start)
	done, err = s.applyServiceScope(done, a, "service_id")
	if err != nil {
		return nil, err
	}
	done.Count(&k.CompletedToday)
	avgConsultSQL := `
		SELECT AVG(EXTRACT(EPOCH FROM (updated_at - COALESCE(doctor_taken_at, checked_in_at)))/60.0)
		FROM patient_queue_tickets
		WHERE status=? AND stage=? AND updated_at >= ?` + scopeSQL
	consultArgs := append([]any{StatusCompleted, StageCompleted, start}, scopeArgs...)
	var avgC *float64
	s.db.Raw(avgConsultSQL, consultArgs...).Scan(&avgC)
	if avgC != nil {
		k.AvgConsultationMinutes = *avgC
	}
	var last time.Time
	lastQ := s.db.Model(&Ticket{}).Select("updated_at").
		Where("status=? AND stage=?", StatusCompleted, StageCompleted).
		Order("updated_at DESC")
	lastQ, err = s.applyServiceScope(lastQ, a, "service_id")
	if err != nil {
		return nil, err
	}
	_ = lastQ.Limit(1).Scan(&last)
	if !last.IsZero() {
		iso := last.UTC().Format(time.RFC3339)
		k.LastCompletedAt = &iso
	}
	return k, nil
}

func (s *Service) Get(id uint, a Access) (*DetailResponse, error) {
	var t Ticket
	if err := s.db.First(&t, id).Error; err != nil {
		return nil, coreerrors.NotFound("Ticket")
	}
	if err := s.assertCanReadTicket(t, a); err != nil {
		return nil, err
	}
	var hist []History
	s.db.Where("ticket_id=?", id).Order("created_at ASC").Find(&hist)
	allergies, histories := s.loadClinicalSnippets(t.PatientID)
	return &DetailResponse{
		Ticket:    s.enrichTicket(t),
		History:   hist,
		Allergies: allergies,
		Histories: histories,
	}, nil
}

func (s *Service) assertCanReadTicket(t Ticket, a Access) error {
	if err := s.assertCanAccessTicket(t, a); err != nil {
		return err
	}
	return s.assertDoctorStageReadable(t, a)
}

func (s *Service) TakeTriage(id uint, a Access) (*Ticket, error) {
	if !s.has(a, "queue.triage.update") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission triage requise")
	}
	if _, err := s.loadTicketForMutation(id, a); err != nil {
		return nil, err
	}
	var out Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Ticket{}).
			Where("id=? AND stage=? AND status=? AND triage_taken_by IS NULL", id, StageWaitingTriage, StatusActive).
			Updates(map[string]any{
				"stage":           StageTriageInProgress,
				"triage_taken_by": a.UserID,
				"triage_taken_at": time.Now().UTC(),
				"version":         gorm.Expr("version + 1"),
				"updated_at":      time.Now().UTC(),
			})
		if res.Error != nil {
			return coreerrors.Internal(res.Error.Error())
		}
		if res.RowsAffected == 0 {
			return coreerrors.Conflict("Patient déjà pris en charge ou non disponible pour triage")
		}
		if err := tx.First(&out, id).Error; err != nil {
			return err
		}
		return s.writeHistory(tx, id, a.UserID, StageWaitingTriage, StageTriageInProgress, "TRIAGE_TAKE", "")
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) CompleteTriage(id uint, r CompleteTriageRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.triage.update") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission triage requise")
	}
	if _, err := s.loadTicketForMutation(id, a); err != nil {
		return nil, err
	}
	var out Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var t Ticket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&t, id).Error; err != nil {
			return coreerrors.NotFound("Ticket")
		}
		if t.Stage != StageTriageInProgress || t.Status != StatusActive {
			return coreerrors.Conflict("Ticket non en triage")
		}
		if t.TriageTakenBy == nil || *t.TriageTakenBy != a.UserID {
			if !a.Has("*") && !a.Has("queue.read.all") {
				return coreerrors.Forbidden("Seul l'agent qui a pris en charge peut valider")
			}
		}
		if r.VitalSignsID != nil {
			if err := s.validateVitalSignsForTicket(tx, t, *r.VitalSignsID); err != nil {
				return err
			}
			t.VitalSignsID = r.VitalSignsID
		}
		now := time.Now().UTC()
		t.Stage = StageWaitingDoctor
		t.TriageCompletedBy = &a.UserID
		t.TriageCompletedAt = &now
		t.Version++
		t.UpdatedAt = now
		if err := tx.Save(&t).Error; err != nil {
			return err
		}
		if err := s.writeHistory(tx, id, a.UserID, StageTriageInProgress, StageWaitingDoctor, "TRIAGE_COMPLETE", ""); err != nil {
			return err
		}
		out = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// validateVitalSignsForTicket ensures vital_signs.id exists and patient_id matches the ticket.
// Encounter/consultation binding is only enforced when both sides carry a consultation_id.
var allowedClinicalDispositions = map[string]bool{
	"DISCHARGED":   true,
	"HOSPITALIZED": true,
	"OBSERVATION":  true,
	"TRANSFERRED":  true,
	"REFERRED":     true,
	"OTHER":        true,
}

func validateClinicalDisposition(d string) error {
	if d == "" {
		return nil
	}
	if !allowedClinicalDispositions[d] {
		return coreerrors.BadRequest("Disposition médicale invalide")
	}
	return nil
}

func (s *Service) activateConsultationTx(tx *gorm.DB, consultationID uint, doctorUserID uint) error {
	now := time.Now().UTC()
	var status string
	if err := tx.Raw(`SELECT status FROM consultations WHERE id=?`, consultationID).Scan(&status).Error; err != nil {
		return coreerrors.Internal(err.Error())
	}
	switch status {
	case consultations.ConsultationStatusCompleted, consultations.ConsultationStatusCancelled:
		return nil
	case consultations.ConsultationStatusInProgress:
		return nil
	case consultations.ConsultationStatusDraft, "":
		return tx.Model(&consultations.Consultation{}).Where("id=?", consultationID).Updates(map[string]any{
			"status":     consultations.ConsultationStatusInProgress,
			"started_at": now,
			"updated_at": now,
		}).Error
	default:
		return nil
	}
}

func (s *Service) linkVitalSignsToConsultationTx(tx *gorm.DB, vitalID, consultationID, patientID uint) error {
	res := tx.Exec(`
		UPDATE vital_signs SET consultation_id=?, updated_at=NOW()
		WHERE id=? AND patient_id=? AND (consultation_id IS NULL OR consultation_id=?)`,
		consultationID, vitalID, patientID, consultationID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return coreerrors.Forbidden("Constantes hors patient/consultation du ticket")
	}
	return nil
}

func (s *Service) completeConsultationTx(tx *gorm.DB, consultationID uint, disposition, dispositionNote string, authorID uint) error {
	var status string
	if err := tx.Raw(`SELECT status FROM consultations WHERE id=?`, consultationID).Scan(&status).Error; err != nil {
		return coreerrors.Internal(err.Error())
	}
	if status == consultations.ConsultationStatusCompleted || status == consultations.ConsultationStatusCancelled {
		return nil
	}
	now := time.Now().UTC()
	if err := tx.Model(&consultations.Consultation{}).Where("id=?", consultationID).Updates(map[string]any{
		"status":       consultations.ConsultationStatusCompleted,
		"completed_at": now,
		"updated_at":   now,
	}).Error; err != nil {
		return err
	}
	if disposition != "" || dispositionNote != "" {
		var soapID uint
		err := tx.Raw(`SELECT id FROM consultation_soaps WHERE consultation_id=?`, consultationID).Scan(&soapID).Error
		if err == nil && soapID > 0 {
			updates := map[string]any{"updated_at": now, "updated_by": authorID}
			if disposition != "" {
				updates["disposition"] = disposition
			}
			if dispositionNote != "" {
				updates["patient_advice"] = dispositionNote
			}
			_ = tx.Table("consultation_soaps").Where("consultation_id=?", consultationID).Updates(updates).Error
		} else {
			_ = tx.Exec(`
				INSERT INTO consultation_soaps(consultation_id, disposition, patient_advice, created_by, updated_by, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?)`,
				consultationID, disposition, dispositionNote, authorID, authorID, now, now).Error
		}
	}
	return nil
}

func (s *Service) assertDoctorCanComplete(t Ticket, a Access) error {
	if a.Has("*") || a.Has("queue.read.all") {
		return nil
	}
	if t.DoctorTakenBy == nil {
		return coreerrors.Conflict("Prise en charge non initiée")
	}
	if *t.DoctorTakenBy != a.UserID {
		return coreerrors.Forbidden("Seul le médecin en charge peut clôturer la prise en charge")
	}
	return nil
}

func (s *Service) validateVitalSignsForTicket(tx *gorm.DB, t Ticket, vitalID uint) error {
	var row struct {
		ID             uint
		PatientID      uint
		ConsultationID *uint
	}
	err := tx.Table("vital_signs").Select("id, patient_id, consultation_id").Where("id=?", vitalID).Take(&row).Error
	if err != nil {
		return coreerrors.NotFound("Constantes")
	}
	if row.PatientID != t.PatientID {
		return coreerrors.Forbidden("Constantes hors patient du ticket")
	}
	if t.ConsultationID != nil && row.ConsultationID != nil && *row.ConsultationID != *t.ConsultationID {
		return coreerrors.Forbidden("Constantes hors consultation du ticket")
	}
	return nil
}

func (s *Service) TakeDoctor(id uint, r TakeDoctorRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.doctor.take") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission médecin requise")
	}
	if _, err := s.loadTicketForMutation(id, a); err != nil {
		return nil, err
	}
	var out Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Ticket{}).
			Where("id=? AND stage=? AND status=? AND doctor_taken_by IS NULL", id, StageWaitingDoctor, StatusActive).
			Updates(map[string]any{
				"stage":           StageDoctorInProgress,
				"doctor_taken_by": a.UserID,
				"doctor_taken_at": time.Now().UTC(),
				"version":         gorm.Expr("version + 1"),
				"updated_at":      time.Now().UTC(),
			})
		if res.Error != nil {
			return coreerrors.Internal(res.Error.Error())
		}
		if res.RowsAffected == 0 {
			return coreerrors.Conflict("Patient déjà pris en charge par un médecin")
		}
		if err := tx.First(&out, id).Error; err != nil {
			return err
		}
		if r.CreateConsultation {
			var doctorName string
			_ = tx.Raw(`SELECT COALESCE(name,'') FROM users WHERE id=?`, a.UserID).Scan(&doctorName)
			var serviceName string
			_ = tx.Raw(`SELECT COALESCE(name,'') FROM organization_services WHERE id=?`, out.ServiceID).Scan(&serviceName)
			sid := out.ServiceID
			now := time.Now().UTC()
			if out.ConsultationID != nil {
				if err := s.activateConsultationTx(tx, *out.ConsultationID, a.UserID); err != nil {
					return err
				}
			} else {
				c := consultations.Consultation{
					PatientID:  out.PatientID,
					DoctorName: doctorName,
					Service:    serviceName,
					ServiceID:  &sid,
					Status:     consultations.ConsultationStatusInProgress,
					StartedAt:  &now,
					Diagnosis:  "Parcours file " + out.Reference,
				}
				if err := tx.Create(&c).Error; err != nil {
					return coreerrors.Internal(err.Error())
				}
				if err := tx.Model(&Ticket{}).Where("id=?", out.ID).Update("consultation_id", c.ID).Error; err != nil {
					return err
				}
				out.ConsultationID = &c.ID
			}
		}
		if out.ConsultationID != nil && out.VitalSignsID != nil {
			if err := s.linkVitalSignsToConsultationTx(tx, *out.VitalSignsID, *out.ConsultationID, out.PatientID); err != nil {
				return err
			}
		}
		if out.AppointmentID != nil {
			_ = tx.Model(&Appointment{}).Where("id=?", *out.AppointmentID).Updates(map[string]any{
				"status":     ApptInProgress,
				"updated_at": time.Now().UTC(),
			}).Error
		}
		return s.writeHistory(tx, id, a.UserID, StageWaitingDoctor, StageDoctorInProgress, "DOCTOR_TAKE", "")
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) Complete(id uint, r CompleteRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.doctor.take") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission refusée")
	}
	if err := validateClinicalDisposition(r.Disposition); err != nil {
		return nil, err
	}
	if _, err := s.loadTicketForMutation(id, a); err != nil {
		return nil, err
	}
	var out Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var t Ticket
		if err := tx.First(&t, id).Error; err != nil {
			return coreerrors.NotFound("Ticket")
		}
		if t.Stage == StageCompleted && t.Status == StatusCompleted {
			out = t
			return nil
		}
		if err := s.assertDoctorCanComplete(t, a); err != nil {
			return err
		}
		if err := AssertTransition(t.Stage, StageCompleted); err != nil {
			return coreerrors.Conflict(err.Error())
		}
		if t.ConsultationID != nil {
			if err := s.completeConsultationTx(tx, *t.ConsultationID, r.Disposition, r.DispositionNote, a.UserID); err != nil {
				return err
			}
		}
		from := t.Stage
		now := time.Now().UTC()
		t.Stage = StageCompleted
		t.Status = StatusCompleted
		t.Version++
		t.UpdatedAt = now
		if err := tx.Save(&t).Error; err != nil {
			return err
		}
		if t.AppointmentID != nil {
			_ = tx.Model(&Appointment{}).Where("id=?", *t.AppointmentID).Updates(map[string]any{
				"status": ApptCompleted, "updated_at": now,
			}).Error
		}
		if err := s.writeHistory(tx, id, a.UserID, from, StageCompleted, "COMPLETED", r.Disposition); err != nil {
			return err
		}
		out = t
		return nil
	})
	return &out, err
}

// GetByConsultationID returns the queue ticket linked to a consultation (reverse lookup).
func (s *Service) GetByConsultationID(consultationID uint, a Access) (*TicketDTO, error) {
	if !s.has(a, "queue.doctor.read") && !s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture file refusée")
	}
	var t Ticket
	if err := s.db.Where("consultation_id = ?", consultationID).First(&t).Error; err != nil {
		return nil, coreerrors.NotFound("Ticket")
	}
	if err := s.assertCanAccessTicket(t, a); err != nil {
		return nil, err
	}
	d := s.enrichTicket(t)
	return &d, nil
}

// GetActiveTicketForPatient returns the active doctor-stage ticket for a patient, if any.
func (s *Service) GetActiveTicketForPatient(patientID uint, a Access) (*TicketDTO, error) {
	if !s.has(a, "queue.doctor.read") && !s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") &&
		!s.has(a, "patients.360.read") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture file refusée")
	}
	var t Ticket
	err := s.db.Where(
		"patient_id=? AND status=? AND stage IN ?",
		patientID, StatusActive, []string{StageWaitingDoctor, StageDoctorInProgress},
	).Order("arrived_at DESC").First(&t).Error
	if err != nil {
		return nil, coreerrors.NotFound("Ticket")
	}
	if err := s.assertCanAccessTicket(t, a); err != nil {
		return nil, err
	}
	d := s.enrichTicket(t)
	return &d, nil
}

func (s *Service) Cancel(id uint, r CancelRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.cancel") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission annulation requise")
	}
	if _, err := s.loadTicketForMutation(id, a); err != nil {
		return nil, err
	}
	var out Ticket
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var t Ticket
		if err := tx.First(&t, id).Error; err != nil {
			return coreerrors.NotFound("Ticket")
		}
		if t.Status != StatusActive && t.Status != StatusOnHold {
			return coreerrors.Conflict("Ticket non annulable")
		}
		from := t.Stage
		now := time.Now().UTC()
		t.Stage = StageCancelled
		t.Status = StatusCancelled
		t.Version++
		t.UpdatedAt = now
		if err := tx.Save(&t).Error; err != nil {
			return err
		}
		if err := s.writeHistory(tx, id, a.UserID, from, StageCancelled, "CANCELLED", r.Reason); err != nil {
			return err
		}
		out = t
		return nil
	})
	return &out, err
}

func (s *Service) SetPriority(id uint, r PriorityRequest, a Access) (*Ticket, error) {
	if !s.has(a, "queue.priority.update") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Permission priorité requise")
	}
	if PriorityRank(r.Priority) == 99 {
		return nil, coreerrors.BadRequest("Priorité invalide")
	}
	t, err := s.loadTicketForMutation(id, a)
	if err != nil {
		return nil, err
	}
	old := t.Priority
	t.Priority = r.Priority
	t.Version++
	t.UpdatedAt = time.Now().UTC()
	if err := s.db.Save(t).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	_ = s.writeHistory(s.db, id, a.UserID, t.Stage, t.Stage, "PRIORITY", old+"→"+r.Priority+": "+r.Reason)
	return t, nil
}

func (s *Service) KPIs(a Access) (*KPIs, error) {
	if !s.has(a, "queue.reception.read") && !s.has(a, "queue.read.service") && !s.has(a, "queue.read.all") && !s.has(a, "*") {
		return nil, coreerrors.Forbidden("Lecture KPI refusée")
	}
	start := time.Now().UTC().Truncate(24 * time.Hour)
	scoped := func() (*gorm.DB, error) {
		q := s.db.Model(&Ticket{})
		return s.applyServiceScope(q, a, "service_id")
	}
	k := &KPIs{}
	q, err := scoped()
	if err != nil {
		return nil, err
	}
	q.Where("checked_in_at >= ?", start).Count(&k.ArrivedToday)
	if q, err = scoped(); err != nil {
		return nil, err
	}
	q.Where("status=? AND stage=?", StatusActive, StageReception).Count(&k.WaitingReception)
	if q, err = scoped(); err != nil {
		return nil, err
	}
	q.Where("status=? AND stage IN ?", StatusActive, []string{StageWaitingTriage, StageTriageInProgress}).Count(&k.WaitingTriage)
	if q, err = scoped(); err != nil {
		return nil, err
	}
	q.Where("status=? AND stage=?", StatusActive, StageWaitingDoctor).Count(&k.WaitingDoctor)
	if q, err = scoped(); err != nil {
		return nil, err
	}
	q.Where("status=? AND stage=?", StatusActive, StageDoctorInProgress).Count(&k.InProgress)
	if q, err = scoped(); err != nil {
		return nil, err
	}
	q.Where("status=? AND updated_at >= ?", StatusCompleted, start).Count(&k.CompletedToday)
	scopeSQL, scopeArgs, err := s.serviceScopeSQL(a, "service_id")
	if err != nil {
		return nil, err
	}
	avgSQL := `
		SELECT AVG(EXTRACT(EPOCH FROM (COALESCE(triage_completed_at, NOW()) - arrived_at))/60.0)
		FROM patient_queue_tickets WHERE status=? AND arrived_at >= ?` + scopeSQL
	avgArgs := append([]any{StatusActive, start}, scopeArgs...)
	var avg *float64
	s.db.Raw(avgSQL, avgArgs...).Scan(&avg)
	if avg != nil {
		k.AvgWaitMinutes = *avg
	}
	lateSQL := `
		SELECT COUNT(*) FROM patient_queue_tickets t
		JOIN patient_queue_appointments a ON a.id = t.appointment_id
		WHERE t.arrived_at > a.scheduled_at + interval '15 minutes' AND t.checked_in_at >= ?` +
		strings.Replace(scopeSQL, "service_id", "t.service_id", 1)
	lateArgs := append([]any{start}, scopeArgs...)
	s.db.Raw(lateSQL, lateArgs...).Scan(&k.LateAppointments)
	return k, nil
}
