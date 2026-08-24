package organization

import (
	"errors"
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"net/http"
	"strconv"
)

type Handler struct{ service *OrganizationService }

func NewHandler(s *OrganizationService) *Handler { return &Handler{service: s} }
func id(c *gin.Context) uint                     { v, _ := strconv.ParseUint(c.Param("id"), 10, 64); return uint(v) }
func actor(c *gin.Context) uint                  { v, _ := rbac.CurrentUserID(c); return v }
func fail(c *gin.Context, e error) {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		response.Error(c, app)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func (h *Handler) Catalog(c *gin.Context) {
	x, e := h.service.Catalog(c.Query("active") != "all")
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) Departments(c *gin.Context) {
	x, e := h.service.Departments()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) Services(c *gin.Context) {
	x, e := h.service.Services(c.Query("active") != "all")
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) Service(c *gin.Context) {
	x, e := h.service.FindService(id(c))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) SaveDepartment(c *gin.Context) {
	var r DepartmentRequest
	if e := c.ShouldBindJSON(&r); e != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": e.Error()})
		return
	}
	x, e := h.service.SaveDepartment(id(c), actor(c), r)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
func (h *Handler) SaveService(c *gin.Context) {
	var r ServiceRequest
	if e := c.ShouldBindJSON(&r); e != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": e.Error()})
		return
	}
	x, e := h.service.SaveService(id(c), actor(c), r)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(http.StatusOK, x)
}
