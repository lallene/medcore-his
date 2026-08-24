package receivables

import (
	"errors"
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"strconv"
)

type Handler struct{ s *Service }

func NewHandler(s *Service) *Handler { return &Handler{s: s} }
func recFail(c *gin.Context, e error) {
	var a *coreerrors.AppError
	if errors.As(e, &a) {
		response.Error(c, a)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func recID(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		recFail(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}
func recUser(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		recFail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}
func filter(c *gin.Context) Filter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	min, _ := strconv.ParseInt(c.Query("minAmount"), 10, 64)
	max, _ := strconv.ParseInt(c.Query("maxAmount"), 10, 64)
	return Filter{Search: c.Query("search"), Status: c.Query("status"), Due: c.Query("due"), DateFrom: c.Query("dateFrom"), DateTo: c.Query("dateTo"), PatientID: uint(patient), MinAmount: min, MaxAmount: max, Page: page, Limit: limit}
}
func (h *Handler) List(c *gin.Context) {
	x, e := h.s.List(filter(c))
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Patient(c *gin.Context) {
	n, ok := recID(c)
	if !ok {
		return
	}
	f := filter(c)
	f.PatientID = n
	x, e := h.s.List(f)
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) KPIs(c *gin.Context) {
	x, e := h.s.KPIs()
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	n, ok := recID(c)
	if !ok {
		return
	}
	x, e := h.s.Detail(n)
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Due(c *gin.Context) {
	n, ok := recID(c)
	if !ok {
		return
	}
	var r DueDateRequest
	if c.ShouldBindJSON(&r) != nil {
		recFail(c, coreerrors.BadRequest("Échéance invalide"))
		return
	}
	u, ok := recUser(c)
	if !ok {
		return
	}
	x, e := h.s.SetDueDate(n, r, u)
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Follow(c *gin.Context) {
	n, ok := recID(c)
	if !ok {
		return
	}
	var r FollowUpRequest
	if c.ShouldBindJSON(&r) != nil {
		recFail(c, coreerrors.BadRequest("Relance invalide"))
		return
	}
	u, ok := recUser(c)
	if !ok {
		return
	}
	x, e := h.s.AddFollowUp(n, r, u)
	if e != nil {
		recFail(c, e)
		return
	}
	c.JSON(201, x)
}
