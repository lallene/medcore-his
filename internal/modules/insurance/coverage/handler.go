package coverage

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/dto"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) List(c *gin.Context) {
	items, err := h.service.List()

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur chargement couvertures"))
		return
	}

	response.Success(c, "Couvertures chargées", ToSummaryList(items))
}

func (h *Handler) FindByID(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID couverture invalide"))
		return
	}

	item, err := h.service.FindByID(id)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Couverture trouvée", ToResponse(item))
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateCoverageRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Create(req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Couverture créée avec succès", ToResponse(item))
}

func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID couverture invalide"))
		return
	}

	var req UpdateCoverageRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Update(id, req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Couverture modifiée avec succès", ToResponse(item))
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID couverture invalide"))
		return
	}

	if err := h.service.Delete(id); err != nil {
		response.Error(c, coreerrors.Internal("Erreur suppression couverture"))
		return
	}

	response.Success(c, "Couverture supprimée avec succès", dto.DeleteResponse{
		ID: id,
	})
}

func (h *Handler) FindByPatient(c *gin.Context) {
	patientID, err := parseID(c.Param("patientId"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID patient invalide"))
		return
	}

	items, err := h.service.FindActiveByPatient(patientID)

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur chargement couvertures patient"))
		return
	}

	response.Success(c, "Couvertures patient chargées", ToSummaryList(items))
}

func parseID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	return uint(id), err
}
