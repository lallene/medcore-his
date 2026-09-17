package patient_queue

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// CreateAppointmentSeries — POST /api/appointment-series (LOT 23O-A).
func (h *Handler) CreateAppointmentSeries(c *gin.Context) {
	var r CreateAppointmentSeriesRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Série invalide"))
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
	x, reused, e := h.service.CreateAppointmentSeries(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	status := 201
	if reused {
		status = 200
	}
	c.JSON(status, x)
}

// GetAppointmentSeries — GET /api/appointment-series/:id (LOT 23O-A).
func (h *Handler) GetAppointmentSeries(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, coreerrors.BadRequest("id invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.GetAppointmentSeries(uint(id), a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// CancelAppointmentSeries — POST /api/appointment-series/:id/cancel (LOT 23O-B).
func (h *Handler) CancelAppointmentSeries(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, coreerrors.BadRequest("id invalide"))
		return
	}
	var r CancelAppointmentSeriesRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Annulation de série invalide"))
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
	x, e := h.service.CancelAppointmentSeries(uint(id), r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// CancelAppointmentSeriesFuture — POST /api/appointment-series/:id/cancel-future (LOT 23O-B).
func (h *Handler) CancelAppointmentSeriesFuture(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, coreerrors.BadRequest("id invalide"))
		return
	}
	var r CancelAppointmentSeriesFutureRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Annulation future de série invalide"))
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
	x, e := h.service.CancelAppointmentSeriesFuture(uint(id), r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

// UpdateAppointmentSeries — PATCH /api/appointment-series/:id (LOT 23O-C).
func (h *Handler) UpdateAppointmentSeries(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, coreerrors.BadRequest("id invalide"))
		return
	}
	var r UpdateAppointmentSeriesRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Modification de série invalide"))
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
	x, e := h.service.UpdateAppointmentSeries(uint(id), r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
