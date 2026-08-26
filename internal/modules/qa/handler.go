package qa

import (
	"errors"
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"strconv"
)

type Handler struct{ s *Service }

func NewHandler(s *Service) *Handler { return &Handler{s: s} }
func qaFail(c *gin.Context, e error) {
	var a *coreerrors.AppError
	if errors.As(e, &a) {
		response.Error(c, a)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func qaID(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		qaFail(c, coreerrors.BadRequest("Identifiant campagne invalide"))
		return 0, false
	}
	return uint(n), true
}
func qaFilter(c *gin.Context) Filter {
	p, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	l, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	return Filter{Environment: c.Query("environment"), Status: c.Query("status"), DateFrom: c.Query("dateFrom"), DateTo: c.Query("dateTo"), Suite: c.Query("suite"), Page: p, Limit: l}
}
func (h *Handler) List(c *gin.Context) {
	x, e := h.s.List(qaFilter(c))
	if e != nil {
		qaFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	id, ok := qaID(c)
	if !ok {
		return
	}
	x, e := h.s.Get(id)
	if e != nil {
		qaFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Results(c *gin.Context) {
	id, ok := qaID(c)
	if !ok {
		return
	}
	x, e := h.s.Results(id)
	if e != nil {
		qaFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) KPIs(c *gin.Context) {
	x, e := h.s.KPIs()
	if e != nil {
		qaFail(c, e)
		return
	}
	c.JSON(200, x)
}
