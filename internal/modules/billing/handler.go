package billing

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
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
		response.Error(c, coreerrors.Internal("Erreur interne"))
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
func handlerPerms(c *gin.Context) []string {
	raw, _ := c.Get(rbac.ContextPermissions)
	p, _ := raw.([]string)
	return p
}

func exposePayerPhone(c *gin.Context) bool {
	return rbac.HasAnyPermission(handlerPerms(c), "billing.payer.read", "*")
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
	if !exposePayerPhone(c) {
		for i := range x.Data {
			RedactPayerContacts(&x.Data[i])
		}
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
	if !exposePayerPhone(c) {
		RedactPayerContacts(x)
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
	// Prefer Idempotency-Key header (canonical retry contract); fall back to body.
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
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
	if !exposePayerPhone(c) {
		RedactPayerContacts(x)
	}
	c.JSON(200, x)
}
func (h *Handler) ReversePayment(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r ReversePaymentRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Contrepassation invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.ReversePayment(n, r, u)
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
func (h *Handler) IssueCreditNote(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CreditNoteRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Avoir invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.IssueCreditNote(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) GetCreditNote(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	x, e := h.service.GetCreditNote(n)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) GetCreditSummary(c *gin.Context) {
	holder, _ := strconv.ParseUint(c.Query("holderPartyId"), 10, 64)
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	x, e := h.service.GetCreditSummary(uint(holder), uint(patient))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ListCreditLedger(c *gin.Context) {
	holder, _ := strconv.ParseUint(c.Query("holderPartyId"), 10, 64)
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	x, e := h.service.ListCreditLedger(uint(holder), uint(patient))
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ListPatientCreditBalances(c *gin.Context) {
	pid, e := strconv.ParseUint(c.Param("patientId"), 10, 64)
	if e != nil || pid == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return
	}
	x, err := h.service.ListPatientCreditBalances(uint(pid))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) GetFinancialStatement(c *gin.Context) {
	pid, e := strconv.ParseUint(c.Param("patientId"), 10, 64)
	if e != nil || pid == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return
	}
	x, err := h.service.GetFinancialStatement(uint(pid), exposePayerPhone(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ListFinancialHistory(c *gin.Context) {
	pid, e := strconv.ParseUint(c.Param("patientId"), 10, 64)
	if e != nil || pid == 0 {
		fail(c, coreerrors.BadRequest("Identifiant invalide"))
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	invoiceID, _ := strconv.ParseUint(c.Query("invoiceId"), 10, 64)
	holderID, _ := strconv.ParseUint(c.Query("holderPartyId"), 10, 64)
	x, err := h.service.ListFinancialHistory(FinancialHistoryFilter{
		PatientID:  uint(pid),
		Page:       page,
		Limit:      limit,
		DateFrom:   strings.TrimSpace(c.Query("dateFrom")),
		DateTo:     strings.TrimSpace(c.Query("dateTo")),
		EventType:  strings.TrimSpace(c.Query("eventType")),
		InvoiceID:  uint(invoiceID),
		HolderID:   uint(holderID),
		IncludePII: exposePayerPhone(c),
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ApplyCredit(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CreditApplicationRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Application de crédit invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.ApplyCredit(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
func (h *Handler) ReverseCreditApplication(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r CreditApplicationReversalRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Annulation d'application invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.ReverseCreditApplication(n, r, u)
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

func (h *Handler) RequestRefund(c *gin.Context) {
	var r RefundRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Demande de remboursement invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.RequestRefund(r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) ApproveRefund(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r RefundDecisionRequest
	_ = c.ShouldBindJSON(&r)
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.ApproveRefund(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) RejectRefund(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r RefundDecisionRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Rejet invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.RejectRefund(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) CancelRefund(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r RefundDecisionRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Annulation invalide"))
		return
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.CancelRefund(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}

func (h *Handler) GetRefund(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	includeRail := exposePayerPhone(c)
	x, e := h.service.GetRefundWithExecution(n, includeRail)
	if e != nil {
		fail(c, e)
		return
	}
	if !includeRail {
		x.BeneficiaryPhone = ""
	}
	c.JSON(200, x)
}

func (h *Handler) ExecuteRefund(c *gin.Context) {
	n, ok := id(c)
	if !ok {
		return
	}
	var r RefundExecuteRequest
	if c.ShouldBindJSON(&r) != nil {
		fail(c, coreerrors.BadRequest("Exécution de remboursement invalide"))
		return
	}
	if header := strings.TrimSpace(c.GetHeader("Idempotency-Key")); header != "" {
		r.IdempotencyKey = header
	}
	u, ok := current(c)
	if !ok {
		return
	}
	x, e := h.service.ExecuteRefund(n, r, u)
	if e != nil {
		fail(c, e)
		return
	}
	if !exposePayerPhone(c) {
		x.BeneficiaryPhone = ""
		if x.Execution != nil {
			x.Execution.BeneficiaryRailRef = ""
		}
	}
	c.JSON(200, x)
}

func (h *Handler) ListRefunds(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	patientID, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	holderID, _ := strconv.ParseUint(c.Query("holderPartyId"), 10, 64)
	x, e := h.service.ListRefunds(RefundListFilter{
		Page:          page,
		Limit:         limit,
		Status:        strings.TrimSpace(c.Query("status")),
		PatientID:     uint(patientID),
		HolderPartyID: uint(holderID),
		ReasonCode:    strings.TrimSpace(c.Query("reasonCode")),
		DateFrom:      strings.TrimSpace(c.Query("dateFrom")),
		DateTo:        strings.TrimSpace(c.Query("dateTo")),
		IncludePII:    exposePayerPhone(c),
	})
	if e != nil {
		fail(c, e)
		return
	}
	c.JSON(200, x)
}
