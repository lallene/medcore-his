package cash

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
func (h *Handler) Open(c *gin.Context) {
	var r OpenRequest
	if c.ShouldBindJSON(&r) != nil {
		bad(c, coreerrors.BadRequest("Ouverture invalide"))
		return
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
	x, e := h.s.Sessions()
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
	u, ok := uid(c)
	if !ok {
		return
	}
	x, e := h.s.Close(n, r, u)
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
