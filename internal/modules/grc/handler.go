package grc

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

type Handler struct {
	service *GRCService
}

func NewHandler(s *GRCService) *Handler {
	return &Handler{service: s}
}

func currentUser(c *gin.Context) (uint, bool) {
	userID, err := rbac.CurrentUserID(c)
	if err != nil {
		response.Error(c, coreerrors.Unauthorized("Utilisateur non authentifié"))
		return 0, false
	}
	return userID, true
}

func parseID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || value == 0 {
		response.Error(c, coreerrors.BadRequest("Identifiant invalide"))
		return 0, false
	}
	return uint(value), true
}

func fail(c *gin.Context, err error) {
	var appErr *coreerrors.AppError
	if errors.As(err, &appErr) {
		response.Error(c, appErr)
		return
	}
	response.Error(c, coreerrors.Internal("Erreur interne"))
}

func parseOptionalUintQuery(c *gin.Context, name string) (*uint, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}

	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		fail(c, coreerrors.BadRequest(name+" invalide"))
		return nil, false
	}

	id := uint(value)
	return &id, true
}

func parsePage(c *gin.Context) (int, int) {
	page := 1
	limit := 20

	if value, err := strconv.Atoi(c.DefaultQuery("page", "1")); err == nil {
		page = value
	}
	if value, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil {
		limit = value
	}

	return page, limit
}

func (h *Handler) ListAssets(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := AssetFilter{
		Search:      strings.TrimSpace(c.Query("search")),
		Type:        strings.TrimSpace(c.Query("type")),
		Criticality: strings.TrimSpace(c.Query("criticality")),
		Status:      strings.TrimSpace(c.Query("status")),
		Page:        page,
		Limit:       limit,
	}

	filter.ServiceID, ok = parseOptionalUintQuery(c, "serviceId")
	if !ok {
		return
	}
	filter.OwnerProfileID, ok = parseOptionalUintQuery(c, "ownerProfileId")
	if !ok {
		return
	}

	result, err := h.service.ListAssets(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetAsset(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindAsset(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateAsset(c *gin.Context) {
	var req AssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Actif GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAsset(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateAsset(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req AssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Actif GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAsset(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListRisks(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := RiskFilter{
		Search:        strings.TrimSpace(c.Query("search")),
		Status:        strings.TrimSpace(c.Query("status")),
		Treatment:     strings.TrimSpace(c.Query("treatment")),
		InherentLevel: strings.TrimSpace(c.Query("inherentLevel")),
		ResidualLevel: strings.TrimSpace(c.Query("residualLevel")),
		Page:          page,
		Limit:         limit,
	}

	filter.AssetID, ok = parseOptionalUintQuery(c, "assetId")
	if !ok {
		return
	}
	filter.ServiceID, ok = parseOptionalUintQuery(c, "serviceId")
	if !ok {
		return
	}
	filter.OwnerProfileID, ok = parseOptionalUintQuery(c, "ownerProfileId")
	if !ok {
		return
	}
	filter.ApproverProfileID, ok = parseOptionalUintQuery(c, "approverProfileId")
	if !ok {
		return
	}
	filter.ControlID, ok = parseOptionalUintQuery(c, "controlId")
	if !ok {
		return
	}

	result, err := h.service.ListRisks(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetRisk(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindRisk(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateRisk(c *gin.Context) {
	var req RiskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Risque GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRisk(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateRisk(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req RiskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Risque GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRisk(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
