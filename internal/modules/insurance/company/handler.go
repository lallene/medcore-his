package company

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
		response.Error(c, coreerrors.Internal("Erreur chargement compagnies"))
		return
	}

	response.Success(c, "Compagnies chargées", ToSummaryList(items))
}

func (h *Handler) FindByID(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID compagnie invalide"))
		return
	}

	item, err := h.service.FindByID(id)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Compagnie trouvée", ToResponse(item))
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateCompanyRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Create(req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Compagnie créée avec succès", ToResponse(item))
}

func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID compagnie invalide"))
		return
	}

	var req UpdateCompanyRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Update(id, req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Compagnie modifiée avec succès", ToResponse(item))
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID compagnie invalide"))
		return
	}

	if err := h.service.Delete(id); err != nil {
		response.Error(c, coreerrors.Internal("Erreur suppression compagnie"))
		return
	}

	response.Success(c, "Compagnie supprimée avec succès", dto.DeleteResponse{
		ID: id,
	})
}

func parseID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, 64)

	return uint(id), err
}
