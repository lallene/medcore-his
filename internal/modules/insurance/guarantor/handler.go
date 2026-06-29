package guarantor

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
		response.Error(c, coreerrors.Internal("Erreur chargement garants"))
		return
	}

	response.Success(c, "Garants chargés", ToSummaryList(items))
}

func (h *Handler) FindByID(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID garant invalide"))
		return
	}

	item, err := h.service.FindByID(id)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Garant trouvé", ToResponse(item))
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateGuarantorRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Create(req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Garant créé avec succès", ToResponse(item))
}

func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID garant invalide"))
		return
	}

	var req UpdateGuarantorRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Update(id, req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Garant modifié avec succès", ToResponse(item))
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID garant invalide"))
		return
	}

	if err := h.service.Delete(id); err != nil {
		response.Error(c, coreerrors.Internal("Erreur suppression garant"))
		return
	}

	response.Success(c, "Garant supprimé avec succès", dto.DeleteResponse{
		ID: id,
	})
}

func parseID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, 64)

	return uint(id), err
}
