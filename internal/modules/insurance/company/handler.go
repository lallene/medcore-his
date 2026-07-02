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

// List godoc
//
//	@Summary		Lister les compagnies d'assurance
//	@Description	Retourne la liste des compagnies d'assurance.
//	@Tags			Insurance Companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Router			/insurance/companies [get]
func (h *Handler) List(c *gin.Context) {
	items, err := h.service.List()

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur chargement compagnies"))
		return
	}

	response.Success(c, "Compagnies chargées", ToSummaryList(items))
}

// FindByID godoc
//
//	@Summary		Détail compagnie d'assurance
//	@Description	Retourne les informations d’une compagnie d'assurance par son ID.
//	@Tags			Insurance Companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		int	true	"ID compagnie d'assurance"
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		400	{object}	openapi.ErrorResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Failure		404	{object}	openapi.ErrorResponse
//	@Router			/insurance/companies/{id} [get]
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

// Create godoc
//
//	@Summary		Créer une compagnie d'assurance
//	@Description	Crée une nouvelle compagnie d'assurance.
//	@Tags			Insurance Companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreateCompanyRequest	true	"Données compagnie"
//	@Success		201		{object}	openapi.SuccessResponse
//	@Failure		400		{object}	openapi.ErrorResponse
//	@Failure		401		{object}	openapi.ErrorResponse
//	@Router			/insurance/companies [post]
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

// Update godoc
//
//	@Summary		Modifier une compagnie d'assurance
//	@Description	Met à jour les informations d’une compagnie d'assurance existante.
//	@Tags			Insurance Companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int						true	"ID compagnie d'assurance"
//	@Param			request	body		UpdateCompanyRequest	true	"Données compagnie d'assurance"
//	@Success		200		{object}	openapi.SuccessResponse
//	@Failure		400		{object}	openapi.ErrorResponse
//	@Failure		401		{object}	openapi.ErrorResponse
//	@Failure		404		{object}	openapi.ErrorResponse
//	@Router			/insurance/companies/{id} [put]
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

// Delete godoc
//
//	@Summary		Supprimer une compagnie d'assurance
//	@Description	Supprime une compagnie d'assurance par son ID.
//	@Tags			Insurance Companies
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		int	true	"ID compagnie d'assurance"
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		400	{object}	openapi.ErrorResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Failure		404	{object}	openapi.ErrorResponse
//	@Router			/insurance/companies/{id} [delete]
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
