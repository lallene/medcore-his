package staff

import (
	"errors"
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func staffFail(c *gin.Context, e error) {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		response.Error(c, app)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func staffID(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		staffFail(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}
func staffActor(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		staffFail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	x, e := h.service.List(Filter{Search: c.Query("search"), Function: c.Query("function"), Specialty: c.Query("specialty"), Active: c.Query("active"), Page: page, Limit: limit})
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	id, ok := staffID(c)
	if !ok {
		return
	}
	x, e := h.service.Get(id)
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Create(c *gin.Context) {
	var r UpsertRequest
	if e := c.ShouldBindJSON(&r); e != nil {
		staffFail(c, coreerrors.BadRequest("Requête invalide"))
		return
	}
	u, ok := staffActor(c)
	if !ok {
		return
	}
	x, e := h.service.Upsert(0, r, u)
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Update(c *gin.Context) {
	id, ok := staffID(c)
	if !ok {
		return
	}
	var r UpsertRequest
	if e := c.ShouldBindJSON(&r); e != nil {
		staffFail(c, coreerrors.BadRequest("Requête invalide"))
		return
	}
	u, ok := staffActor(c)
	if !ok {
		return
	}
	x, e := h.service.Upsert(id, r, u)
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Audit(c *gin.Context) {
	id, ok := staffID(c)
	if !ok {
		return
	}
	x, e := h.service.Audit(id)
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Catalog(c *gin.Context) { c.JSON(200, CatalogData()) }
func (h *Handler) Users(c *gin.Context) {
	x, e := h.service.Users()
	if e != nil {
		staffFail(c, e)
		return
	}
	c.JSON(200, x)
}
