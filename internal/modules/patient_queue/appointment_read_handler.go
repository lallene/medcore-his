package patient_queue

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// ListAppointments — GET /api/appointments?from=&to=
func (h *Handler) ListAppointments(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	fromRaw := c.Query("from")
	toRaw := c.Query("to")
	if fromRaw == "" || toRaw == "" {
		fail(c, coreerrors.BadRequest("from et to requis"))
		return
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		fail(c, coreerrors.BadRequest("from invalide (RFC3339)"))
		return
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		fail(c, coreerrors.BadRequest("to invalide (RFC3339)"))
		return
	}
	svcID, err := parseUintQuery(c.Query("serviceId"))
	if err != nil {
		fail(c, err)
		return
	}
	pracID, err := parseUintQuery(c.Query("practitionerId"))
	if err != nil {
		fail(c, err)
		return
	}
	typeID, err := parseUintQuery(c.Query("appointmentTypeId"))
	if err != nil {
		fail(c, err)
		return
	}
	patientID, err := parseUintQuery(c.Query("patientId"))
	if err != nil {
		fail(c, err)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	f := AppointmentListFilter{
		From:              from,
		To:                to,
		ServiceID:         svcID,
		PractitionerID:    pracID,
		PatientID:         patientID,
		Status:            c.Query("status"),
		AppointmentTypeID: typeID,
		Page:              page,
		Limit:             limit,
	}
	x, e := h.service.ListAppointments(f, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// GetAppointment — GET /api/appointments/:id
func (h *Handler) GetAppointment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.GetAppointment(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// ListAppointmentTypes — GET /api/appointment-types
func (h *Handler) ListAppointmentTypes(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	svcID, err := parseUintQuery(c.Query("serviceId"))
	if err != nil {
		fail(c, err)
		return
	}
	active, err := parseBoolQuery(c.Query("active"))
	if err != nil {
		fail(c, err)
		return
	}
	rows, e := h.service.ListAppointmentTypes(svcID, active, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{"items": rows})
}
