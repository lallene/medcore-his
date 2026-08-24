package laboratory

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func user(c *gin.Context) (uint, bool) {
	id, e := rbac.CurrentUserID(c)
	if e != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": e.Error()})
		return 0, false
	}
	return id, true
}
func orderID(c *gin.Context) (uint, bool) {
	v, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || v == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "identifiant de demande invalide"})
		return 0, false
	}
	return uint(v), true
}
func (h *Handler) List(c *gin.Context) {
	u, ok := user(c)
	if !ok {
		return
	}
	p := pagination.FromContext(c)
	f := ListFilter{Page: p.Page, Limit: p.Limit, Status: c.Query("status"), Priority: c.Query("priority"), Category: c.Query("category"), Search: c.Query("search")}
	if raw := c.Query("serviceId"); raw != "" {
		v, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || v == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "service invalide"})
			return
		}
		id := uint(v)
		f.ServiceID = &id
	}
	rawPatientID := c.Query("patientId")
	if rawPatientID == "" && c.FullPath() == "/api/patients/:id/laboratory-orders" {
		rawPatientID = c.Param("id")
	}
	if raw := rawPatientID; raw != "" {
		v, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || v == 0 {
			c.JSON(400, gin.H{"error": "patientId invalide"})
			return
		}
		x := uint(v)
		f.PatientID = &x
	}
	if raw := c.Query("consultationId"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "consultationId invalide"})
			return
		}
		x := uint(v)
		f.ConsultationID = &x
	}
	r, e := h.service.List(f, u)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"data": r.Data, "meta": gin.H{"page": r.Page, "limit": r.Limit, "total": r.Total, "totalPages": r.TotalPages}})
}
func (h *Handler) Get(c *gin.Context) {
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.Get(id, u) })
}
func (h *Handler) PrepareSample(c *gin.Context) {
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.PrepareSample(id, u) })
}
func (h *Handler) Collect(c *gin.Context) {
	var req CollectRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "prélèvement invalide"})
		return
	}
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.Collect(id, u, req) })
}
func (h *Handler) Start(c *gin.Context) {
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.Start(id, u) })
}
func (h *Handler) Results(c *gin.Context) {
	var req EnterResultsRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "résultats invalides"})
		return
	}
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.EnterResults(id, u, req) })
}
func (h *Handler) Validate(c *gin.Context) {
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.Validate(id, u) })
}
func (h *Handler) Cancel(c *gin.Context) {
	var req CancelRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "motif obligatoire"})
		return
	}
	h.respond(c, func(id, u uint) (*Order, error) { return h.service.Cancel(id, u, req.Reason) })
}
func (h *Handler) respond(c *gin.Context, fn func(uint, uint) (*Order, error)) {
	id, ok := orderID(c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	o, e := fn(id, u)
	if e != nil {
		switch {
		case errors.Is(e, gorm.ErrRecordNotFound):
			c.JSON(404, gin.H{"error": "demande introuvable"})
		case errors.Is(e, ErrInvalidTransition), errors.Is(e, ErrValidated):
			c.JSON(409, gin.H{"error": e.Error()})
		default:
			c.JSON(500, gin.H{"error": e.Error()})
		}
		return
	}
	c.JSON(200, o)
}
