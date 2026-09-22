package performed_acts

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct {
	service *Service
}

func NewHandler(s *Service) *Handler {
	return &Handler{service: s}
}

func currentUser(c *gin.Context) (uint, bool) {
	u, err := rbac.CurrentUserID(c)
	if err != nil {
		response.Error(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}

func parseID(c *gin.Context) (uint, bool) {
	v, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || v == 0 {
		response.Error(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(v), true
}

func fail(c *gin.Context, err error) {
	var app *coreerrors.AppError
	if errors.As(err, &app) {
		response.Error(c, app)
		return
	}
	response.Error(c, coreerrors.Internal(err.Error()))
}

func (h *Handler) List(c *gin.Context) {
	f := ListFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Category: strings.TrimSpace(c.Query("category")),
	}
	if raw := strings.TrimSpace(c.Query("patientId")); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v == 0 {
			fail(c, coreerrors.BadRequest("patientId invalide"))
			return
		}
		f.PatientID = uint(v)
	}
	if raw := strings.TrimSpace(c.Query("consultationId")); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || v == 0 {
			fail(c, coreerrors.BadRequest("consultationId invalide"))
			return
		}
		f.ConsultationID = uint(v)
	}
	f.PerformedFrom = strings.TrimSpace(c.Query("from"))
	f.PerformedTo = strings.TrimSpace(c.Query("to"))
	if page, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil {
		f.Page = page
	}
	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil {
		f.Limit = limit
	}

	page, err := h.service.List(f)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, page)
}

func (h *Handler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	item, err := h.service.GetByID(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, item)
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Acte réalisé invalide"))
		return
	}
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.Create(req, actor)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, item)
}

func (h *Handler) Void(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req VoidRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Annulation invalide"))
		return
	}
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.Void(id, req, actor)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, item)
}
