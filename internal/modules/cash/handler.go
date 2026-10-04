package cash

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct{ s *Service }

func NewHandler(s *Service) *Handler { return &Handler{s: s} }
func uid(c *gin.Context) (uint, bool) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		response.Error(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return u, true
}
func num(c *gin.Context) (uint, bool) {
	n, e := strconv.ParseUint(c.Param("id"), 10, 64)
	if e != nil || n == 0 {
		response.Error(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(n), true
}
func bad(c *gin.Context, e error) {
	var a *coreerrors.AppError
	if errors.As(e, &a) {
		response.Error(c, a)
	} else {
		response.Error(c, coreerrors.Internal(e.Error()))
	}
}
func (h *Handler) Registers(c *gin.Context) {
	x, e := h.s.Registers()
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CreateRegister(c *gin.Context) {
	var r RegisterRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Caisse invalide"))
		return
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.SaveRegister(0, r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) UpdateRegister(c *gin.Context) {
	var r RegisterRequest
	n, ok := num(c)
	if !ok {
		return
	}
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Caisse invalide"))
		return
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.SaveRegister(n, r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func perms(c *gin.Context) []string {
	v, ok := c.Get(rbac.ContextPermissions)
	if !ok {
		return nil
	}
	p, _ := v.([]string)
	return p
}

func (h *Handler) Open(c *gin.Context) {
	var r OpenRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Ouverture invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.Open(r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Current(c *gin.Context) {
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.Current(u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Sessions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	regID, _ := strconv.ParseUint(c.Query("cashRegisterId"), 10, 64)
	x, e := h.s.ListSessions(SessionListFilter{
		Status:         c.Query("status"),
		CashRegisterID: uint(regID),
		DateFrom:       c.Query("dateFrom"),
		DateTo:         c.Query("dateTo"),
		Page:           page,
		Limit:          limit,
	})
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Get(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.Get(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Pay(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	var r PaymentRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Paiement invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.Pay(n, r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) Close(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	var r CloseRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Clôture invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	canCloseAny := rbac.HasAnyPermission(perms(c), "cash.session.close_any", "*")
	x, e := h.s.Close(n, r, u, canCloseAny)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Receipts(c *gin.Context) {
	n, _ := strconv.ParseUint(c.Query("sessionId"), 10, 64)
	x, e := h.s.Receipts(uint(n))
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Journal(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.Receipts(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CreateMovement(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	var r MovementRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Mouvement de caisse invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.CreateMovement(n, r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) ListMovements(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.ListMovements(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) GetMovement(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.GetMovement(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) Receipt(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.Receipt(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) CorrectionEligibility(c *gin.Context) {
	n, e := strconv.ParseUint(c.Param("paymentId"), 10, 64)
	if e != nil || n == 0 {
		bad(c, coreerrors.BadRequest("Identifiant invalide"))
		return
	}
	x, err := h.s.CorrectionEligibilityForPayment(uint(n))
	if err != nil {
		bad(c, err)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ExecuteCorrection(c *gin.Context) {
	var r ExecuteCorrectionRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Exécution de correction invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.ExecutePostCloseCorrection(r, u)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(201, x)
}
func (h *Handler) GetCorrection(c *gin.Context) {
	n, ok := num(c)
	if !ok {
		return
	}
	x, e := h.s.GetCorrectionExecution(n)
	if e != nil {
		bad(c, e)
		return
	}
	c.JSON(200, x)
}
