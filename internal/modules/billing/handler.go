package billing

import (
	"errors"
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"net/http"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func current(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		response.Error(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}
func id(c *gin.Context) (uint, bool) {
	v, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || v == 0 {
		response.Error(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(v), true
}
func fail(c *gin.Context, e error) {
	var app *coreerrors.AppError
	if errors.As(e, &app) {
		response.Error(c, app)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func (h *Handler) Tariffs(c *gin.Context) {
	x, e := h.service.ListTariffs()
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CreateTariff(c *gin.Context) {
	var r TariffRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Tarif invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.CreateTariff(r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) UpdateTariff(c *gin.Context) {
	var r TariffRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Tarif invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.UpdateTariff(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Billable(c *gin.Context) {
	n, e := strconv.ParseUint(c.Query("patientId"), 10, 64)
	if e != nil || n == 0 {
		fail(c, coreerrors.BadRequest("patientId invalide"))
		return
	}
	x, e := h.service.BillableActs(uint(n))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ActStatus(c *gin.Context) {
	patient, e := strconv.ParseUint(c.Query("patientId"), 10, 64)
	if e != nil || patient == 0 {
		fail(c, coreerrors.BadRequest("patientId invalide"))
		return
	}
	reference, e := strconv.ParseUint(c.Query("referenceId"), 10, 64)
	if e != nil || reference == 0 {
		fail(c, coreerrors.BadRequest("referenceId invalide"))
		return
	}
	x, e := h.service.ActStatus(uint(patient), c.Query("actType"), uint(reference))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Create(c *gin.Context) {
	var r CreateInvoiceRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Facture invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.CreateInvoice(r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	x, e := h.service.List(ListFilter{Page: page, Limit: limit, PatientID: uint(patient), Status: c.Query("status"), Search: c.Query("search")})
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.GetInvoice(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Issue(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.Issue(n, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Pay(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r PaymentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Paiement invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.Pay(n, r, u)
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
	var r CancelRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Motif obligatoire"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.Cancel(n, r.Reason, u)
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
	c.JSON(http.StatusOK, x)
}
