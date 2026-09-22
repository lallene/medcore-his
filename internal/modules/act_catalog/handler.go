package act_catalog

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
		Search:   strings.TrimSpace(c.Query("search")),
		Category: strings.TrimSpace(c.Query("category")),
	}
	if page, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil {
		f.Page = page
	}
	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil {
		f.Limit = limit
	}
	if raw := strings.TrimSpace(c.Query("active")); raw != "" {
		switch strings.ToLower(raw) {
		case "true", "1":
			v := true
			f.Active = &v
		case "false", "0":
			v := false
			f.Active = &v
		default:
			fail(c, coreerrors.BadRequest("Filtre active invalide"))
			return
		}
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
		fail(c, coreerrors.BadRequest("Acte catalogue invalide"))
		return
	}
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.Create(req, userID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, item)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req UpdateRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Acte catalogue invalide"))
		return
	}
	userID, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.Update(id, req, userID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, item)
}
