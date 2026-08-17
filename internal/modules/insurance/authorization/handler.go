package authorization

import (
	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
	"strconv"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func id(c *gin.Context) (uint, error) {
	v, e := strconv.ParseUint(c.Param("id"), 10, 64)
	return uint(v), e
}
func user(c *gin.Context) (uint, error) {
	u, e := rbac.CurrentUserID(c)
	if e != nil {
		return 0, coreerrors.Unauthorized("Utilisateur JWT requis")
	}
	return u, nil
}
func (h *Handler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	company, _ := strconv.ParseUint(c.Query("companyId"), 10, 64)
	patient, _ := strconv.ParseUint(c.Query("patientId"), 10, 64)
	result, e := h.service.List(ListQuery{Search: c.Query("search"), Status: c.Query("status"), ReferenceType: c.Query("referenceType"), Service: c.Query("service"), CompanyID: uint(company), PatientID: uint(patient), DateFrom: c.Query("dateFrom"), DateTo: c.Query("dateTo"), Page: page, PageSize: size})
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC chargées", result)
}
func (h *Handler) Find(c *gin.Context) {
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	result, e := h.service.FindByID(n)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC chargée", result)
}
func (h *Handler) FindForAct(c *gin.Context) {
	patientID, patientErr := strconv.ParseUint(c.Query("patientId"), 10, 64)
	coverageID, coverageErr := strconv.ParseUint(c.Query("coverageId"), 10, 64)
	referenceID, referenceErr := strconv.ParseUint(c.Query("referenceId"), 10, 64)
	if patientErr != nil || coverageErr != nil || referenceErr != nil || patientID == 0 || coverageID == 0 || referenceID == 0 {
		response.Error(c, coreerrors.BadRequest("Identité d'acte invalide"))
		return
	}
	result, e := h.service.FindAuthorizationForAct(uint(patientID), uint(coverageID), c.Query("referenceType"), uint(referenceID))
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "Recherche PEC terminée", result)
}
func (h *Handler) EligibleActs(c *gin.Context) {
	patientID, patientErr := strconv.ParseUint(c.Query("patientId"), 10, 64)
	coverageID, coverageErr := strconv.ParseUint(c.Query("coverageId"), 10, 64)
	if patientErr != nil || coverageErr != nil || patientID == 0 || coverageID == 0 {
		response.Error(c, coreerrors.BadRequest("Patient ou couverture invalide"))
		return
	}
	result, e := h.service.EligibleActs(uint(patientID), uint(coverageID), c.Query("type"), c.Query("search"))
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "Actes éligibles chargés", result)
}
func (h *Handler) LinkAct(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	var req ActRequest
	if e = validator.Bind(c, &req); e != nil {
		response.Error(c, e)
		return
	}
	result, e := h.service.LinkAct(n, req, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "Acte couvert rattaché", result)
}
func (h *Handler) Create(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	var req CreateRequest
	if e = validator.Bind(c, &req); e != nil {
		response.Error(c, e)
		return
	}
	result, e := h.service.Create(req, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Created(c, "PEC créée", result)
}
func (h *Handler) Update(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	var req UpdateRequest
	if e = validator.Bind(c, &req); e != nil {
		response.Error(c, e)
		return
	}
	result, e := h.service.Update(n, req, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC modifiée", result)
}
func (h *Handler) Submit(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	var req SubmitRequest
	if e = validator.Bind(c, &req); e != nil {
		response.Error(c, e)
		return
	}
	result, e := h.service.Submit(n, req, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC envoyée", result)
}
func (h *Handler) Pending(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	result, e := h.service.MarkPending(n, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC en attente", result)
}
func (h *Handler) Decide(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	var req DecisionRequest
	if e = validator.Bind(c, &req); e != nil {
		response.Error(c, e)
		return
	}
	result, e := h.service.Decide(n, req, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "Décision enregistrée", result)
}
func (h *Handler) Cancel(c *gin.Context) {
	u, e := user(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	n, e := id(c)
	if e != nil {
		response.Error(c, coreerrors.BadRequest("ID PEC invalide"))
		return
	}
	result, e := h.service.Cancel(n, u)
	if e != nil {
		response.Error(c, e)
		return
	}
	response.Success(c, "PEC annulée", result)
}
