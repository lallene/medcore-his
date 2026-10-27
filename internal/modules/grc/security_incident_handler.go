package grc

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func (h *Handler) ListSecurityIncidents(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := SecurityIncidentFilter{
		Search:   strings.TrimSpace(c.Query("search")),
		Status:   strings.TrimSpace(c.Query("status")),
		Severity: strings.TrimSpace(c.Query("severity")),
		Page:     page,
		Limit:    limit,
	}
	for name, target := range map[string]**uint{
		"riskId":          &filter.RiskID,
		"assetId":         &filter.AssetID,
		"owningServiceId": &filter.OwningServiceID,
		"ownerProfileId":  &filter.OwnerProfileID,
	} {
		if *target, ok = parseOptionalUintQuery(c, name); !ok {
			return
		}
	}

	result, err := h.service.ListSecurityIncidents(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetSecurityIncident(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindSecurityIncident(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateSecurityIncident(c *gin.Context) {
	var req SecurityIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Incident de sécurité invalide"))
		return
	}
	// Create never accepts a client status. A status field in the body is
	// ignored: the service forces OPEN.
	req.Status = ""

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.CreateSecurityIncident(actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateSecurityIncident(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req SecurityIncidentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Incident de sécurité invalide"))
		return
	}
	if rejectReservedStatus(c, req.Status, incidentReservedStatuses) {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.UpdateSecurityIncident(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// CloseSecurityIncident takes no body. closedAt is set by the service.
func (h *Handler) CloseSecurityIncident(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.CloseSecurityIncident(id, actor)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
