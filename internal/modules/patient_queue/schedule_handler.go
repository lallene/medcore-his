package patient_queue

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func (h *Handler) CreateSchedule(c *gin.Context) {
	var r CreateWorkingScheduleRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Planning invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CreateWorkingSchedule(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

func (h *Handler) ListSchedules(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	f := scheduleFilter(c)
	rows, total, e := h.service.ListWorkingSchedules(f, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{"items": rows, "total": total, "page": f.Page, "limit": f.Limit})
}

func (h *Handler) ListMySchedules(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	activeOnly := c.DefaultQuery("active", "true") != "false"
	rows, e := h.service.ListMyWorkingSchedules(a, activeOnly)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{"items": rows})
}

func (h *Handler) GetSchedule(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.GetWorkingSchedule(id, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) UpdateSchedule(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	var r UpdateWorkingScheduleRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Mise à jour planning invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.UpdateWorkingSchedule(id, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) DeleteSchedule(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.DisableWorkingSchedule(id, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) CreateScheduleException(c *gin.Context) {
	var r CreateScheduleExceptionRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Exception invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CreateScheduleException(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

func (h *Handler) ListScheduleExceptions(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	f := exceptionFilter(c)
	rows, total, e := h.service.ListScheduleExceptions(f, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{"items": rows, "total": total, "page": f.Page, "limit": f.Limit})
}

func (h *Handler) GetScheduleException(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.GetScheduleException(id, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) UpdateScheduleException(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	var r UpdateScheduleExceptionRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Mise à jour exception invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.UpdateScheduleException(id, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) DeleteScheduleException(c *gin.Context) {
	id, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CancelScheduleException(id, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func scheduleFilter(c *gin.Context) ScheduleFilter {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	f := ScheduleFilter{Page: p, Limit: l}
	if v := c.Query("practitionerId"); v != "" {
		if n, e := strconv.ParseUint(v, 10, 64); e == nil {
			u := uint(n)
			f.PractitionerID = &u
		}
	}
	if v := c.Query("serviceId"); v != "" {
		if n, e := strconv.ParseUint(v, 10, 64); e == nil {
			u := uint(n)
			f.ServiceID = &u
		}
	}
	if v := c.Query("weekday"); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			f.Weekday = &n
		}
	}
	if v := c.Query("active"); v != "" {
		b := v == "true" || v == "1"
		f.Active = &b
	}
	if v := c.Query("date"); v != "" {
		if t, e := time.Parse("2006-01-02", v); e == nil {
			f.ValidOn = &t
		}
	}
	return f
}

func exceptionFilter(c *gin.Context) ExceptionFilter {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	f := ExceptionFilter{Page: p, Limit: l}
	if v := c.Query("practitionerId"); v != "" {
		if n, e := strconv.ParseUint(v, 10, 64); e == nil {
			u := uint(n)
			f.PractitionerID = &u
		}
	}
	if v := c.Query("serviceId"); v != "" {
		if n, e := strconv.ParseUint(v, 10, 64); e == nil {
			u := uint(n)
			f.ServiceID = &u
		}
	}
	if v := c.Query("type"); v != "" {
		f.Type = &v
	}
	if v := c.Query("active"); v != "" {
		b := v == "true" || v == "1"
		f.Active = &b
	}
	if v := c.Query("from"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			f.From = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, e := time.Parse(time.RFC3339, v); e == nil {
			f.To = &t
		}
	}
	return f
}
