package patient_queue

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func (h *Handler) GetAvailability(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	q, err := parseAvailabilityQuery(c, true)
	if err != nil {
		fail(c, err)
		return
	}
	res, e := h.service.ComputeAvailability(q, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, res)
}

func (h *Handler) GetFirstAvailability(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	q, err := parseAvailabilityQuery(c, false)
	if err != nil {
		fail(c, err)
		return
	}
	if q.To.IsZero() {
		q.To = q.From.AddDate(0, 0, 7)
	}
	slot, e := h.service.FirstAvailable(q, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{
		"slot":         slot,
		"timezone":     scheduling.LocationName(),
		"snapshotNote": availabilitySnapshotNote,
		"readOnly":     true,
	})
}

// GetMyAvailability — JWT identity only; optional service among assigned services.
func (h *Handler) GetMyAvailability(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	q, err := parseAvailabilityQuery(c, true)
	if err != nil {
		fail(c, err)
		return
	}
	pid := a.UserID
	q.PractitionerID = &pid
	res, e := h.service.ComputeAvailability(q, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, res)
}

func parseAvailabilityQuery(c *gin.Context, requireTo bool) (AvailabilityQuery, error) {
	var q AvailabilityQuery
	sid, err := strconv.ParseUint(c.Query("serviceId"), 10, 64)
	if err != nil || sid == 0 {
		return q, coreerrors.BadRequest("serviceId requis")
	}
	q.ServiceID = uint(sid)

	if v := c.Query("practitionerId"); v != "" {
		n, e := strconv.ParseUint(v, 10, 64)
		if e != nil || n == 0 {
			return q, coreerrors.BadRequest("practitionerId invalide")
		}
		u := uint(n)
		q.PractitionerID = &u
	}
	if v := c.Query("appointmentTypeId"); v != "" {
		n, e := strconv.ParseUint(v, 10, 64)
		if e != nil || n == 0 {
			return q, coreerrors.BadRequest("appointmentTypeId invalide")
		}
		u := uint(n)
		q.AppointmentTypeID = &u
	}
	if v := c.Query("durationMinutes"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n <= 0 {
			return q, coreerrors.BadRequest("durationMinutes invalide")
		}
		q.DurationMinutes = &n
	}
	if v := c.Query("slotStepMinutes"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n <= 0 {
			return q, coreerrors.BadRequest("slotStepMinutes invalide")
		}
		q.SlotStepMinutes = &n
	}

	fromStr := c.Query("from")
	if fromStr == "" {
		return q, coreerrors.BadRequest("from requis")
	}
	from, e := time.Parse(time.RFC3339, fromStr)
	if e != nil {
		return q, coreerrors.BadRequest("from doit être RFC3339")
	}
	q.From = from

	toStr := c.Query("to")
	if toStr == "" {
		if requireTo {
			return q, coreerrors.BadRequest("to requis")
		}
		q.To = from.AddDate(0, 0, 7)
	} else {
		to, e := time.Parse(time.RFC3339, toStr)
		if e != nil {
			return q, coreerrors.BadRequest("to doit être RFC3339")
		}
		q.To = to
	}
	return q, nil
}
