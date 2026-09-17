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
