package insurance_receivables

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func fail(c *gin.Context, e error) {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		response.Error(c, app)
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
func user(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		fail(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}
func bind[T any](c *gin.Context) (T, bool) {
	var r T
	if e := c.ShouldBindJSON(&r); e != nil {
		fail(c, coreerrors.BadRequest("Requête invalide"))
		return r, false
	}
	return r, true
}
func filters(c *gin.Context) Filter {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	company, _ := strconv.ParseUint(c.Query("companyId"), 10, 64)
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	batch, _ := strconv.ParseUint(c.Query("batchId"), 10, 64)
	return Filter{Search: c.Query("search"), Status: c.Query("status"), DateFrom: c.Query("dateFrom"), DateTo: c.Query("dateTo"), Overdue: c.Query("overdue"), CompanyID: uint(company), PatientID: uint(patient), BatchID: uint(batch), Page: page, Limit: limit}
}
func (h *Handler) List(c *gin.Context) {
	x, e := h.service.List(filters(c))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Receivable(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.ReceivableDetail(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) FollowUp(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	r, ok := bind[FollowUpRequest](c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.AddFollowUp(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Patient(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	f := filters(c)
	f.PatientID = n
	x, e := h.service.List(f)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) KPIs(c *gin.Context) {
	x, e := h.service.KPIs()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Companies(c *gin.Context) {
	x, e := h.service.Companies()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Due(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	r, ok := bind[DueDateRequest](c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.SetDue(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Settlements(c *gin.Context) {
	company, _ := strconv.ParseUint(c.Query("companyId"), 10, 64)
	x, e := h.service.ListSettlements(uint(company))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Settlement(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.Settlement(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CreateSettlement(c *gin.Context) {
	r, ok := bind[SettlementRequest](c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.CreateSettlement(r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Allocate(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	r, ok := bind[AllocationRequest](c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.Allocate(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Post(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.Post(n, u)
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
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.Cancel(n, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Batches(c *gin.Context) {
	company, _ := strconv.ParseUint(c.Query("companyId"), 10, 64)
	x, e := h.service.ListBatches(uint(company))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Batch(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.Batch(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CreateBatch(c *gin.Context) {
	r, ok := bind[BatchRequest](c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.CreateBatch(r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) SubmitBatch(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	u, ok := user(c)
	if !ok {
		return
	}
	x, e := h.service.SubmitBatch(n, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
