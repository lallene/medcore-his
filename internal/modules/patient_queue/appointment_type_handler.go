package patient_queue

import (
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// CreateAppointmentTypeAdmin — POST /api/appointment-types
func (h *Handler) CreateAppointmentTypeAdmin(c *gin.Context) {
	var r CreateAppointmentTypeRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Type de rendez-vous invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CreateAppointmentType(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

// UpdateAppointmentTypeAdmin — PATCH /api/appointment-types/:id
func (h *Handler) UpdateAppointmentTypeAdmin(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	var r UpdateAppointmentTypeRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Type de rendez-vous invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.UpdateAppointmentType(id, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// DisableAppointmentTypeAdmin — DELETE /api/appointment-types/:id (soft-deactivate)
func (h *Handler) DisableAppointmentTypeAdmin(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.DisableAppointmentType(id, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
