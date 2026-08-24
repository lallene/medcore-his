package hospitalizations

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func idParam(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		response.Error(c, coreerrors.BadRequest("identifiant invalide"))
		return 0, false
	}
	return uint(value), true
}
func authorID(c *gin.Context) (uint, bool) {
	id, err := rbac.CurrentUserID(c)
	if err != nil {
		response.Error(c, err)
		return 0, false
	}
	return id, true
}

func (h *Handler) Create(c *gin.Context) {
	author, ok := authorID(c)
	if !ok {
		return
	}
	var req CreateRequest
	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}
	item, created, err := h.service.Create(req, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	if created {
		response.Created(c, "Hospitalisation créée", item)
	} else {
		response.Success(c, "Hospitalisation déjà existante", item)
	}
}
func (h *Handler) FindByID(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	item, err := h.service.FindByID(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Hospitalisation trouvée", item)
}
func (h *Handler) FindByConsultation(c *gin.Context) {
	id, ok := idParam(c, "consultationId")
	if !ok {
		return
	}
	item, err := h.service.FindByConsultation(id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Hospitalisation trouvée", item)
}
func (h *Handler) List(c *gin.Context) {
	p := pagination.FromContext(c)
	filter := ListFilter{Page: p.Page, Limit: p.Limit, Status: strings.ToUpper(c.Query("status")), Department: c.Query("department")}
	if raw := c.Query("serviceId"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "service invalide"})
			return
		}
		id := uint(value)
		filter.ServiceID = &id
	}
	if raw := c.Query("patientId"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || value == 0 {
			response.Error(c, coreerrors.BadRequest("patientId invalide"))
			return
		}
		patientID := uint(value)
		filter.PatientID = &patientID
	}
	for raw, target := range map[string]**time.Time{"from": &filter.From, "to": &filter.To} {
		if value := c.Query(raw); value != "" {
			parsed, err := time.Parse("2006-01-02", value)
			if err != nil {
				response.Error(c, coreerrors.BadRequest(raw+" invalide"))
				return
			}
			*target = &parsed
		}
	}
	if filter.To != nil {
		endExclusive := filter.To.AddDate(0, 0, 1)
		filter.To = &endExclusive
	}
	result, err := h.service.List(filter)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.SuccessWithMeta(c, "Hospitalisations chargées", result.Data, pagination.Meta{Page: result.Page, Limit: result.Limit, Total: result.Total, TotalPages: result.TotalPages})
}
func (h *Handler) ListByPatient(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	query.Set("patientId", strconv.FormatUint(uint64(id), 10))
	c.Request.URL.RawQuery = query.Encode()
	h.List(c)
}
func (h *Handler) Admit(c *gin.Context) {
	h.withRequest(c, func() any { return &AdmitRequest{} }, func(id, author uint, raw any) (*Hospitalization, error) {
		return h.service.Admit(id, *raw.(*AdmitRequest), author)
	}, "Patient admis")
}
func (h *Handler) Discharge(c *gin.Context) {
	h.withRequest(c, func() any { return &DischargeRequest{} }, func(id, author uint, raw any) (*Hospitalization, error) {
		return h.service.Discharge(id, *raw.(*DischargeRequest), author)
	}, "Sortie enregistrée")
}
func (h *Handler) Cancel(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	item, err := h.service.Cancel(id, author)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, "Hospitalisation annulée", item)
}
func (h *Handler) withRequest(c *gin.Context, request func() any, action func(uint, uint, any) (*Hospitalization, error), message string) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	author, ok := authorID(c)
	if !ok {
		return
	}
	req := request()
	if err := c.ShouldBindJSON(req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := action(id, author, req)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Success(c, message, item)
}
