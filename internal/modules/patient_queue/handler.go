package patient_queue

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

func fail(c *gin.Context, e error) {
	var a *coreerrors.AppError
	if errors.As(e, &a) {
		response.Error(c, a)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}

func id(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}

func patientID(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("patientId"), 10, 64)
	if e != nil || n == 0 {
		fail(c, coreerrors.BadRequest("Patient invalide"))
		return 0, false
	}
	return uint(n), true
}

func access(c *gin.Context) (Access, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		fail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return Access{}, false
	}
	a := Access{UserID: u, Permissions: map[string]bool{}}
	if p, ok := c.Get(rbac.ContextPermissions); ok {
		if values, ok := p.([]string); ok {
			for _, v := range values {
				a.Permissions[v] = true
			}
		}
	}
	return a, true
}

func (h *Handler) enrich(a *Access) {
	var sid *uint
	h.service.db.Raw("SELECT primary_service_id FROM staff_profiles WHERE user_id=? AND active", a.UserID).Scan(&sid)
	a.ServiceID = sid
}

func filter(c *gin.Context) Filter {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	return Filter{
		Search:   c.Query("search"),
		Stage:    c.Query("stage"),
		Status:   c.Query("status"),
		Priority: c.Query("priority"),
		Source:   c.Query("source"),
		Service:  c.Query("service"),
		Doctor:   c.Query("doctor"),
		Page:     p,
		Limit:    l,
	}
}

func (h *Handler) CreateAppointment(c *gin.Context) {
	var r CreateAppointmentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Rendez-vous invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CreateAppointment(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

func (h *Handler) ListAppointmentsToday(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	x, e := h.service.ListAppointmentsToday(a, p, l)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) CheckInAppointment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r AppointmentCheckInRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Check-in invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CheckInAppointment(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

func (h *Handler) CheckInWalkIn(c *gin.Context) {
	var r WalkInCheckInRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Check-in invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CheckInWalkIn(r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}

func (h *Handler) MarkNoShow(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	if e := h.service.MarkNoShow(n, a.UserID, a); e != nil {
		fail(c, e)
		return
	}
	c.Status(204)
}

func (h *Handler) List(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.List(filter(c), a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Get(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Get(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) TakeTriage(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.TakeTriage(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) CompleteTriage(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CompleteTriageRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Triage invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.CompleteTriage(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) TakeDoctor(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r TakeDoctorRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Prise en charge invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.TakeDoctor(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Complete(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Complete(n, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Cancel(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CancelRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Annulation invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.Cancel(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) SetPriority(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r PriorityRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Priorité invalide"))
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.SetPriority(n, r, a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) KPIs(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	h.enrich(&a)
	x, e := h.service.KPIs(a)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) EvaluateFinance(c *gin.Context) {
	n, ok := patientID(c)
	if !ok {
		return
	}
	status, e := h.service.EvaluateFinance(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, gin.H{"patientId": n, "financeStatus": status})
}
