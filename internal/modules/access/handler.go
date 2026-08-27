package access

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

func actorID(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		fail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}

func profileID(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}

func (h *Handler) KPIs(c *gin.Context) {
	x, e := h.service.KPIs()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) ListUsers(c *gin.Context) {
	f := listFilter{
		Search: c.Query("search"), Function: c.Query("function"), Status: c.Query("status"),
		Privilege: c.Query("privilege"), HasOverrides: c.Query("hasOverrides") == "true",
	}
	f.Page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	f.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "20"))
	if sid := c.Query("serviceId"); sid != "" {
		n, _ := strconv.ParseUint(sid, 10, 64)
		if n > 0 {
			u := uint(n)
			f.ServiceID = &u
		}
	}
	x, e := h.service.ListUsers(f)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) GetUser(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	x, e := h.service.GetUser(id)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) SetOverride(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	actor, ok := actorID(c)
	if !ok {
		return
	}
	var req OverrideRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Override invalide"))
		return
	}
	x, e := h.service.SetOverride(id, actor, req)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) ClearOverride(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	actor, ok := actorID(c)
	if !ok {
		return
	}
	perm := c.Param("permission")
	x, e := h.service.ClearOverride(id, actor, perm, c.Query("reason"))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) SetActive(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	actor, ok := actorID(c)
	if !ok {
		return
	}
	var req ActiveRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Payload invalide"))
		return
	}
	x, e := h.service.SetActive(id, actor, req)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) SetFunctions(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	actor, ok := actorID(c)
	if !ok {
		return
	}
	var req FunctionsRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Payload invalide"))
		return
	}
	x, e := h.service.SetFunctions(id, actor, req)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) SetServices(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	actor, ok := actorID(c)
	if !ok {
		return
	}
	var req ServicesRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Payload invalide"))
		return
	}
	x, e := h.service.SetServices(id, actor, req)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Matrix(c *gin.Context) {
	x, e := h.service.Matrix()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) ToggleMatrix(c *gin.Context) {
	actor, ok := actorID(c)
	if !ok {
		return
	}
	var req MatrixToggleRequest
	if c.ShouldBindJSON(&req) != nil {
		fail(c, coreerrors.BadRequest("Payload invalide"))
		return
	}
	if e := h.service.ToggleMatrix(actor, req); e != nil {
		fail(c, e)
		return
	}
	x, e := h.service.Matrix()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Permissions(c *gin.Context) {
	c.JSON(200, h.service.PermissionsCatalog())
}

func (h *Handler) Audit(c *gin.Context) {
	var target uint
	if id := c.Query("userId"); id != "" {
		n, _ := strconv.ParseUint(id, 10, 64)
		target = uint(n)
	} else if id := c.Param("id"); id != "" && c.FullPath() != "" {
		// optional path param on user audit
	}
	if pid := c.Param("id"); pid != "" && c.Request.URL.Path != "/api/access/audit" {
		n, _ := strconv.ParseUint(pid, 10, 64)
		if n > 0 {
			detail, e := h.service.GetUser(uint(n))
			if e != nil {
				fail(c, e)
				return
			}
			target = detail.UserID
		}
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	x, e := h.service.Audit(target, limit)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) Simulate(c *gin.Context) {
	id, ok := profileID(c)
	if !ok {
		return
	}
	x, e := h.service.Simulate(id)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
