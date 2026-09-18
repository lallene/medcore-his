package imaging

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
	"gorm.io/gorm"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

func access(c *gin.Context) (Access, bool) {
	id, err := rbac.CurrentUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return Access{}, false
	}
	a := Access{UserID: id, Permissions: map[string]bool{}}
	if p, ok := c.Get(rbac.ContextPermissions); ok {
		if values, ok := p.([]string); ok {
			for _, v := range values {
				a.Permissions[v] = true
			}
		}
	}
	return a, true
}

func imagingOrderID(c *gin.Context) (uint, bool) {
	v, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || v == 0 {
		c.JSON(400, gin.H{"error": "identifiant de demande invalide"})
		return 0, false
	}
	return uint(v), true
}

func (h *Handler) List(c *gin.Context) {
	a, ok := access(c)
	if !ok {
		return
	}
	p := pagination.FromContext(c)
	f := ListFilter{Page: p.Page, Limit: p.Limit, Status: c.Query("status"), Priority: c.Query("priority"), Modality: c.Query("modality"), Service: c.Query("service"), Search: c.Query("search"), Date: c.Query("date")}
	if rawService := c.Query("serviceId"); rawService != "" {
		v, e := strconv.ParseUint(rawService, 10, 64)
		if e != nil || v == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "service invalide"})
			return
		}
		id := uint(v)
		f.ServiceID = &id
	}
	raw := c.Query("patientId")
	if raw == "" && c.FullPath() == "/api/patients/:id/imaging-orders" {
		raw = c.Param("id")
	}
	if raw != "" {
		v, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || v == 0 {
			c.JSON(400, gin.H{"error": "patientId invalide"})
			return
		}
		x := uint(v)
		f.PatientID = &x
	}
	if raw = c.Query("consultationId"); raw != "" {
		v, e := strconv.ParseUint(raw, 10, 64)
		if e != nil || v == 0 {
			c.JSON(400, gin.H{"error": "consultationId invalide"})
			return
		}
		x := uint(v)
		f.ConsultationID = &x
	}
	r, e := h.service.List(f, a)
	if e != nil {
		c.JSON(500, gin.H{"error": e.Error()})
		return
	}
	c.JSON(200, gin.H{"data": r.Data, "meta": gin.H{"page": r.Page, "limit": r.Limit, "total": r.Total, "totalPages": r.TotalPages}})
}

func (h *Handler) Get(c *gin.Context) {
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.Get(id, a) })
}
func (h *Handler) Schedule(c *gin.Context) {
	var req ScheduleRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "planification invalide"})
		return
	}
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.Schedule(id, a, req) })
}
func (h *Handler) Start(c *gin.Context) {
	var req StartRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "réalisation invalide"})
		return
	}
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.Start(id, a, req) })
}
func (h *Handler) Report(c *gin.Context) {
	var req ReportRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "compte rendu invalide"})
		return
	}
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.SaveReport(id, a, req) })
}
func (h *Handler) Validate(c *gin.Context) {
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.Validate(id, a) })
}
func (h *Handler) Cancel(c *gin.Context) {
	var req CancelRequest
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "motif obligatoire"})
		return
	}
	h.respond(c, func(id uint, a Access) (*Order, error) { return h.service.Cancel(id, a, req.Reason) })
}

func (h *Handler) respond(c *gin.Context, fn func(uint, Access) (*Order, error)) {
	id, ok := imagingOrderID(c)
	if !ok {
		return
	}
	a, ok := access(c)
	if !ok {
		return
	}
	o, e := fn(id, a)
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
