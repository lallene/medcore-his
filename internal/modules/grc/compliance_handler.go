package grc

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || value == 0 {
		fail(c, coreerrors.BadRequest(name+" invalide"))
		return 0, false
	}
	return uint(value), true
}

func (h *Handler) ListFrameworks(c *gin.Context) {
	page, limit := parsePage(c)

	filter := FrameworkFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Code:   strings.TrimSpace(c.Query("code")),
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}

	result, err := h.service.ListFrameworks(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetFramework(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindFramework(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateFramework(c *gin.Context) {
	var req FrameworkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Référentiel GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveFramework(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateFramework(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req FrameworkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Référentiel GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveFramework(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListRequirements(c *gin.Context) {
	page, limit := parsePage(c)

	filter := RequirementFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}

	var ok bool
	filter.FrameworkID, ok = parseOptionalUintQuery(c, "frameworkId")
	if !ok {
		return
	}
	filter.ParentRequirementID, ok = parseOptionalUintQuery(c, "parentRequirementId")
	if !ok {
		return
	}

	result, err := h.service.ListRequirements(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetRequirement(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindRequirement(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateRequirement(c *gin.Context) {
	var req RequirementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Exigence GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRequirement(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateRequirement(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req RequirementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Exigence GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRequirement(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListControls(c *gin.Context) {
	page, limit := parsePage(c)

	filter := ControlFilter{
		Search:   strings.TrimSpace(c.Query("search")),
		Category: strings.TrimSpace(c.Query("category")),
		Status:   strings.TrimSpace(c.Query("status")),
		Page:     page,
		Limit:    limit,
	}

	var ok bool
	filter.ServiceID, ok = parseOptionalUintQuery(c, "serviceId")
	if !ok {
		return
	}
	filter.OwnerProfileID, ok = parseOptionalUintQuery(c, "ownerProfileId")
	if !ok {
		return
	}

	result, err := h.service.ListControls(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetControl(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindControl(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateControl(c *gin.Context) {
	var req ControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Contrôle GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveControl(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateControl(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req ControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Contrôle GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveControl(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListControlImplementations(c *gin.Context) {
	page, limit := parsePage(c)

	filter := ControlImplementationFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}

	var ok bool
	filter.ControlID, ok = parseOptionalUintQuery(c, "controlId")
	if !ok {
		return
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

	result, err := h.service.ListControlImplementations(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetControlImplementation(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindControlImplementation(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateControlImplementation(c *gin.Context) {
	var req ControlImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Implémentation de contrôle invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveControlImplementation(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateControlImplementation(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req ControlImplementationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Implémentation de contrôle invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveControlImplementation(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) LinkRequirementControl(c *gin.Context) {
	var req RequirementControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Lien exigence-contrôle invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.LinkRequirementControl(actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UnlinkRequirementControl(c *gin.Context) {
	requirementID, ok := parseUintParam(c, "requirementId")
	if !ok {
		return
	}
	controlID, ok := parseUintParam(c, "controlId")
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	if err := h.service.UnlinkRequirementControl(actor, requirementID, controlID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) LinkControlRisk(c *gin.Context) {
	var req ControlRiskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Lien contrôle-risque invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.LinkControlRisk(actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UnlinkControlRisk(c *gin.Context) {
	controlID, ok := parseUintParam(c, "controlId")
	if !ok {
		return
	}
	riskID, ok := parseUintParam(c, "riskId")
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	if err := h.service.UnlinkControlRisk(actor, controlID, riskID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
