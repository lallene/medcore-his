package voucher

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/dto"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"github.com/lallene/medcore-his/backend/internal/core/validator"
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
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
		response.Error(c, coreerrors.Internal("Erreur chargement bons"))
		return
	}

	response.Success(c, "Bons chargés", ToSummaryList(items))
}

func (h *Handler) FindByID(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID bon invalide"))
		return
	}

	item, err := h.service.FindByID(id)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Bon trouvé", ToResponse(item))
}

func (h *Handler) Create(c *gin.Context) {
	var req CreateVoucherRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Create(req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Created(c, "Bon créé avec succès", ToResponse(item))
}

func (h *Handler) Update(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID bon invalide"))
		return
	}

	var req UpdateVoucherRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	item, err := h.service.Update(id, req)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Bon modifié avec succès", ToResponse(item))
}

func (h *Handler) Delete(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID bon invalide"))
		return
	}

	if err := h.service.Delete(id); err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Bon supprimé avec succès", dto.DeleteResponse{
		ID: id,
	})
}

func (h *Handler) ApplyWorkflow(c *gin.Context) {
	id, err := parseID(c.Param("id"))

	if err != nil {
		response.Error(c, coreerrors.BadRequest("ID bon invalide"))
		return
	}

	var req WorkflowActionRequest

	if err := validator.Bind(c, &req); err != nil {
		response.Error(c, err)
		return
	}

	actor := workflow.Actor{
		UserID:      getUserID(c),
		Role:        getRole(c),
		Permissions: getPermissions(c),
	}

	item, err := h.service.ApplyWorkflow(id, req, actor)

	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, "Workflow appliqué avec succès", ToResponse(item))
}

func getUserID(c *gin.Context) uint {
	value, exists := c.Get(rbac.ContextUserID)

	if !exists {
		return 0
	}

	userID, ok := value.(uint)
	if !ok {
		return 0
	}

	return userID
}

func getRole(c *gin.Context) string {
	value, exists := c.Get(rbac.ContextRole)

	if !exists {
		return ""
	}

	role, ok := value.(string)
	if !ok {
		return ""
	}

	return role
}

func getPermissions(c *gin.Context) []string {
	value, exists := c.Get(rbac.ContextPermissions)

	if !exists {
		return []string{}
	}

	permissions, ok := value.([]string)
	if !ok {
		return []string{}
	}

	return permissions
}

func parseID(value string) (uint, error) {
	id, err := strconv.ParseUint(value, 10, 64)

	return uint(id), err
}
