package patient_queue

import (
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// RescheduleAppointment — PATCH /api/appointments/:id/reschedule
func (h *Handler) RescheduleAppointment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r RescheduleAppointmentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Reschedule invalide"))
		return
	}
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		r.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.RescheduleAppointment(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, h.service.enrichAppointment(*x))
}

// CancelAppointment — POST /api/appointments/:id/cancel
func (h *Handler) CancelAppointment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CancelAppointmentRequest
	_ = c.ShouldBindJSON(&r)
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		r.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CancelAppointment(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, h.service.enrichAppointment(*x))
}

// MarkNoShowAuthoritative — POST /api/appointments/:id/no-show
func (h *Handler) MarkNoShowAuthoritative(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r NoShowAppointmentRequest
	_ = c.ShouldBindJSON(&r)
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		r.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.MarkNoShow(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, h.service.enrichAppointment(*x))
}

// MarkNoShow — legacy POST /api/queue/appointments/:id/no-show → same service
func (h *Handler) MarkNoShow(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r NoShowAppointmentRequest
	_ = c.ShouldBindJSON(&r)
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		r.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	if _, e := h.service.MarkNoShow(n, r, a); e != nil {
		fail(c, e)
		return
	}
	c.Status(204)
}
