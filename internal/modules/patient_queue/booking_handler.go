package patient_queue

import (
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// BookAppointment — authoritative transactional booking (LOT 23D).
// POST /api/appointments
func (h *Handler) BookAppointment(c *gin.Context) {
	var r BookAppointmentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Réservation invalide"))
		return
	}
	// Prefer Idempotency-Key header when body key omitted (safe client retry).
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		r.IdempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, reused, e := h.service.BookAppointment(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	status := 201
	if reused {
		status = 200
	}
	c.JSON(status, h.service.enrichAppointment(*x))
}
