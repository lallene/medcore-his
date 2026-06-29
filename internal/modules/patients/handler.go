package patients

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/dto"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
	"github.com/lallene/medcore-his/backend/internal/shared/pagination"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// List godoc
//
//	@Summary		Lister les patients
//	@Description	Retourne la liste des patients enregistrés.
//	@Tags			Patients
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Route
func (h *Handler) List(c *gin.Context) {
	p := pagination.FromContext(c)
	search := c.Query("search")

	result, err := h.service.ListPaginated(p.Page, p.Limit, search)

	if err != nil {
		response.Error(c, coreerrors.Internal("Erreur chargement patients"))
		return
	}

	response.SuccessWithMeta(
		c,
		"Patients chargés",
		ToSummaryList(result.Data),
		map[string]any{
			"page":       result.Page,
			"limit":      result.Limit,
			"total":      result.Total,
			"totalPages": result.TotalPages,
		},
	)
}

// FindByID godoc
//
//	@Summary		Détail patient
//	@Description	Retourne les informations d’un patient par son ID.
//	@Tags			Patients
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		int	true	"ID patient"
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		400	{object}	openapi.ErrorResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Failure		404	{object}	openapi.ErrorResponse
//	@Router			/patients/{id} [get]
func (h *Handler) FindByID(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(
			c,
			coreerrors.NotFound("PATIENT"),
		)
		return
	}

	patient, err := h.service.FindByID(id)

	if err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"Patient introuvable",
				err.Error(),
			),
		)
		return
	}

	response.Success(c, "Patient trouvé", ToResponse(patient))
}

// Create godoc
//
//	@Summary		Créer un patient
//	@Description	Crée un nouveau patient dans MedCore.
//	@Tags			Patients
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		CreatePatientRequest	true	"Données patient"
//	@Success		201		{object}	openapi.SuccessResponse
//	@Failure		400		{object}	openapi.ErrorResponse
//	@Failure		401		{object}	openapi.ErrorResponse
//	@Router			/patients [post]
func (h *Handler) Create(c *gin.Context) {
	var req CreatePatientRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	patient, err := h.service.Create(req)

	if err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"Erreur création patient",
				err.Error(),
			),
		)
		return
	}

	response.Created(c, "Patient créé avec succès", ToResponse(patient))
}

// Update godoc
//
//	@Summary		Modifier un patient
//	@Description	Met à jour les informations d’un patient existant.
//	@Tags			Patients
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		int						true	"ID patient"
//	@Param			request	body		UpdatePatientRequest	true	"Données patient"
//	@Success		200		{object}	openapi.SuccessResponse
//	@Failure		400		{object}	openapi.ErrorResponse
//	@Failure		401		{object}	openapi.ErrorResponse
//	@Failure		404		{object}	openapi.ErrorResponse
//	@Router			/patients/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"ID patient invalide",
				"Le paramètre id doit être un nombre valide.",
			),
		)
		return
	}

	var req UpdatePatientRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	patient, err := h.service.Update(id, req)

	if err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"Erreur modification patient",
				err.Error(),
			),
		)
		return
	}

	response.Success(c, "Patient modifié avec succès", ToResponse(patient))
}

// Delete godoc
//
//	@Summary		Supprimer un patient
//	@Description	Supprime un patient par son ID.
//	@Tags			Patients
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		int	true	"ID patient"
//	@Success		200	{object}	openapi.SuccessResponse
//	@Failure		400	{object}	openapi.ErrorResponse
//	@Failure		401	{object}	openapi.ErrorResponse
//	@Failure		404	{object}	openapi.ErrorResponse
//	@Router			/patients/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"ID patient invalide",
				"Le paramètre id doit être un nombre valide.",
			),
		)
		return
	}

	if err := h.service.Delete(id); err != nil {
		response.Error(
			c,
			coreerrors.Validation(
				"Erreur suppression patient",
				err.Error(),
			),
		)
		return
	}

	response.Success(
		c,
		"Patient supprimé avec succès",
		dto.DeleteResponse{
			ID: id,
		},
	)
}

func parseID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, 64)

	return uint(id), err
}
