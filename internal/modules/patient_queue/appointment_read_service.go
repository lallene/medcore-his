package patient_queue

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

// AppointmentListFilter — GET /api/appointments query (LOT 23F.1 / 23H).
type AppointmentListFilter struct {
	From              time.Time
	To                time.Time
	ServiceID         *uint
	PractitionerID    *uint
	PatientID         *uint
	Status            string
	AppointmentTypeID *uint
	Page              int
	Limit             int
}

func (s *Service) canReadAppointments(a Access) bool {
	return a.Has("*") ||
		a.Has("schedule.read.all") ||
		a.Has("schedule.read.service") ||
		a.Has("schedule.read.own")
}

func validAppointmentStatus(st string) bool {
	switch st {
	case ApptScheduled, ApptArrived, ApptCheckedIn, ApptInProgress, ApptCompleted, ApptCancelled, ApptNoShow:
		return true
	default:
		return false
	}
}

func (s *Service) validateAppointmentListRange(from, to time.Time) error {
	from = from.UTC()
	to = to.UTC()
	if from.IsZero() || to.IsZero() {
		return coreerrors.BadRequest("from et to requis")
	}
	if !to.After(from) {
		return coreerrors.BadRequest("to doit être > from")
	}
	if to.Sub(from) > time.Duration(scheduling.MaxQueryRangeDays)*24*time.Hour {
		return coreerrors.BadRequest(fmt.Sprintf("plage maximale %d jours", scheduling.MaxQueryRangeDays))
	}
	return nil
}

// applyAppointmentReadScope restricts by schedule.read.own|service|all (union when combined).
func (s *Service) applyAppointmentReadScope(q *gorm.DB, a Access) (*gorm.DB, error) {
	if !s.canReadAppointments(a) {
		return nil, coreerrors.Forbidden("Lecture agenda non autorisée")
	}
	if s.canReadAllSchedules(a) {
		return q, nil
	}
	ownOnly := a.Has("schedule.read.own") && !a.Has("schedule.read.service")
	serviceRead := a.Has("schedule.read.service")
	if ownOnly && !serviceRead {
		return q.Where("a.expected_doctor_id = ?", a.UserID), nil
	}
	ids, err := s.scheduleScopeServiceIDs(a)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		return q, nil
	}
	if a.Has("schedule.read.own") {
		return q.Where("a.service_id IN ? OR a.expected_doctor_id = ?", ids, a.UserID), nil
	}
	return q.Where("a.service_id IN ?", ids), nil
}

func (s *Service) assertCanReadAppointment(appt Appointment, a Access) error {
	if !s.canReadAppointments(a) {
		return coreerrors.NotFound("Rendez-vous")
	}
	if s.canReadAllSchedules(a) {
		return nil
	}
	ownOnly := a.Has("schedule.read.own") && !a.Has("schedule.read.service")
	if ownOnly {
		if appt.ExpectedDoctorID != nil && *appt.ExpectedDoctorID == a.UserID {
			return nil
		}
		return coreerrors.NotFound("Rendez-vous")
	}
	if a.Has("schedule.read.service") {
		if err := s.assertScheduleServiceInScope(appt.ServiceID, a); err != nil {
			if a.Has("schedule.read.own") && appt.ExpectedDoctorID != nil && *appt.ExpectedDoctorID == a.UserID {
				return nil
			}
			return coreerrors.NotFound("Rendez-vous")
		}
		return nil
	}
	if a.Has("schedule.read.own") && appt.ExpectedDoctorID != nil && *appt.ExpectedDoctorID == a.UserID {
		return nil
	}
	return coreerrors.NotFound("Rendez-vous")
}

// appointmentEffectiveEndSQL — PostgreSQL expression matching ResolveAppointmentEnd.
func appointmentEffectiveEndSQL() string {
	mins := legacyFallbackMinutes()
	return fmt.Sprintf(`CASE
		WHEN a.scheduled_end_at IS NOT NULL THEN a.scheduled_end_at
		WHEN t.default_duration_minutes IS NOT NULL AND t.default_duration_minutes > 0
			THEN a.scheduled_at + (t.default_duration_minutes::text || ' minutes')::interval
		ELSE a.scheduled_at + interval '%d minutes'
	END`, mins)
}

// ListAppointments — GET /api/appointments with half-open range intersection [from, to).
func (s *Service) ListAppointments(f AppointmentListFilter, a Access) (*AppointmentListResponse, error) {
	if err := s.validateAppointmentListRange(f.From, f.To); err != nil {
		return nil, err
	}
	from, to := f.From.UTC(), f.To.UTC()
	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if f.Status != "" && !validAppointmentStatus(f.Status) {
		return nil, coreerrors.BadRequest("statut invalide")
	}

	// OWN-only cannot request another practitioner.
	if f.PractitionerID != nil && a.Has("schedule.read.own") && !a.Has("schedule.read.service") && !s.canReadAllSchedules(a) {
		if *f.PractitionerID != a.UserID {
			return nil, coreerrors.Forbidden("Lecture hors mon planning")
		}
	}
	if f.ServiceID != nil && !s.canReadAllSchedules(a) && a.Has("schedule.read.service") {
		if err := s.assertScheduleServiceInScope(*f.ServiceID, a); err != nil {
			return nil, coreerrors.NotFound("Service")
		}
	}

	endSQL := appointmentEffectiveEndSQL()
	q := s.db.Table("patient_queue_appointments AS a").
		Joins("LEFT JOIN patient_queue_appointment_types t ON t.id = a.appointment_type_id").
		Where("a.scheduled_at < ?", to).
		Where(endSQL+" > ?", from)

	var err error
	q, err = s.applyAppointmentReadScope(q, a)
	if err != nil {
		return nil, err
	}
	if f.ServiceID != nil {
		q = q.Where("a.service_id = ?", *f.ServiceID)
	}
	if f.PractitionerID != nil {
		q = q.Where("a.expected_doctor_id = ?", *f.PractitionerID)
	}
	if f.PatientID != nil {
		q = q.Where("a.patient_id = ?", *f.PatientID)
	}
	if f.Status != "" {
		q = q.Where("a.status = ?", f.Status)
	}
	if f.AppointmentTypeID != nil {
		q = q.Where("a.appointment_type_id = ?", *f.AppointmentTypeID)
	}

	var total int64
	if err := q.Session(&gorm.Session{}).Distinct("a.id").Count(&total).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}

	var rows []Appointment
	if err := q.Select("a.*").
		Order("a.scheduled_at ASC, a.id ASC").
		Offset((page - 1) * limit).Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return &AppointmentListResponse{
		Items: s.enrichAppointments(rows),
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

// GetAppointment — GET /api/appointments/:id
func (s *Service) GetAppointment(id uint, a Access) (*AppointmentDTO, error) {
	if !s.canReadAppointments(a) {
		return nil, coreerrors.Forbidden("Lecture agenda non autorisée")
	}
	var appt Appointment
	if err := s.db.First(&appt, id).Error; err != nil {
		return nil, coreerrors.NotFound("Rendez-vous")
	}
	if err := s.assertCanReadAppointment(appt, a); err != nil {
		return nil, err
	}
	dto := s.enrichAppointments([]Appointment{appt})[0]
	return &dto, nil
}

// ListAppointmentTypes — GET /api/appointment-types
func (s *Service) ListAppointmentTypes(serviceID *uint, active *bool, a Access) ([]AppointmentType, error) {
	if !s.canReadAppointments(a) {
		return nil, coreerrors.Forbidden("Lecture types de rendez-vous non autorisée")
	}
	q := s.db.Model(&AppointmentType{})
	if active != nil {
		q = q.Where("active = ?", *active)
	}
	if serviceID != nil {
		// Types may be global (service_id NULL) or service-scoped; include both for the service.
		q = q.Where("service_id IS NULL OR service_id = ?", *serviceID)
	}
	var rows []AppointmentType
	if err := q.Order("name ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, coreerrors.Internal(err.Error())
	}
	return rows, nil
}

// enrichAppointments batches patient/service/practitioner/type lookups (no N+1).
func (s *Service) enrichAppointments(rows []Appointment) []AppointmentDTO {
	out := make([]AppointmentDTO, 0, len(rows))
	if len(rows) == 0 {
		return out
	}
	patientIDs := map[uint]struct{}{}
	serviceIDs := map[uint]struct{}{}
	doctorIDs := map[uint]struct{}{}
	typeIDs := map[uint]struct{}{}
	for _, r := range rows {
		patientIDs[r.PatientID] = struct{}{}
		serviceIDs[r.ServiceID] = struct{}{}
		if r.ExpectedDoctorID != nil {
			doctorIDs[*r.ExpectedDoctorID] = struct{}{}
		}
		if r.AppointmentTypeID != nil {
			typeIDs[*r.AppointmentTypeID] = struct{}{}
		}
	}

	patientCode := map[uint]string{}
	patientName := map[uint]string{}
	if len(patientIDs) > 0 {
		ids := keysUint(patientIDs)
		type prow struct {
			ID   uint
			Code string
			Name string
		}
		var ps []prow
		_ = s.db.Raw(`SELECT id, COALESCE(code_patient,'') AS code, TRIM(CONCAT(COALESCE(prenoms,''),' ',COALESCE(nom,''))) AS name FROM patients WHERE id IN ?`, ids).Scan(&ps)
		for _, p := range ps {
			patientCode[p.ID] = p.Code
			patientName[p.ID] = strings.TrimSpace(p.Name)
		}
	}
	serviceName := map[uint]string{}
	if len(serviceIDs) > 0 {
		ids := keysUint(serviceIDs)
		type srow struct {
			ID   uint
			Name string
		}
		var ss []srow
		_ = s.db.Raw(`SELECT id, COALESCE(name,'') AS name FROM organization_services WHERE id IN ?`, ids).Scan(&ss)
		for _, x := range ss {
			serviceName[x.ID] = x.Name
		}
	}
	doctorName := map[uint]string{}
	if len(doctorIDs) > 0 {
		ids := keysUint(doctorIDs)
		type urow struct {
			ID   uint
			Name string
		}
		var us []urow
		_ = s.db.Raw(`SELECT id, COALESCE(name,'') AS name FROM users WHERE id IN ?`, ids).Scan(&us)
		for _, u := range us {
			doctorName[u.ID] = u.Name
		}
	}
	typeMeta := map[uint]AppointmentType{}
	typeDur := map[uint]int{}
	if len(typeIDs) > 0 {
		ids := keysUint(typeIDs)
		var ts []AppointmentType
		_ = s.db.Where("id IN ?", ids).Find(&ts)
		for _, t := range ts {
			typeMeta[t.ID] = t
			if t.DefaultDurationMinutes > 0 {
				typeDur[t.ID] = t.DefaultDurationMinutes
			}
		}
	}

	for _, r := range rows {
		d := AppointmentDTO{Appointment: r}
		d.PatientCode = patientCode[r.PatientID]
		d.PatientName = patientName[r.PatientID]
		d.ServiceName = serviceName[r.ServiceID]
		if r.ExpectedDoctorID != nil {
			d.ExpectedDoctorName = doctorName[*r.ExpectedDoctorID]
		}
		if r.AppointmentTypeID != nil {
			if t, ok := typeMeta[*r.AppointmentTypeID]; ok {
				d.AppointmentTypeCode = t.Code
				d.AppointmentTypeName = t.Name
			}
		}
		end := ResolveAppointmentEnd(r, typeDur)
		d.DurationMinutes = int(end.Sub(r.ScheduledAt.UTC()).Minutes())
		if d.DurationMinutes < 0 {
			d.DurationMinutes = 0
		}
		if r.ArrivedAt != nil {
			d.Punctuality = Punctuality(r.ScheduledAt, *r.ArrivedAt)
		}
		d.HasActiveTicket = r.QueueTicketID != nil
		out = append(out, d)
	}
	return out
}

func keysUint(m map[uint]struct{}) []uint {
	out := make([]uint, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	return out
}

func parseUintQuery(raw string) (*uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 {
		return nil, coreerrors.BadRequest("paramètre numérique invalide")
	}
	u := uint(n)
	return &u, nil
}

func parseBoolQuery(raw string) (*bool, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return nil, nil
	}
	switch raw {
	case "true", "1", "yes":
		v := true
		return &v, nil
	case "false", "0", "no":
		v := false
		return &v, nil
	default:
		return nil, coreerrors.BadRequest("paramètre active invalide")
	}
}
