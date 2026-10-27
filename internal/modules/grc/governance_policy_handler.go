package grc

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func (h *Handler) ListGovernancePolicies(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}
	filter := GovernancePolicyFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}
	for name, target := range map[string]**uint{
		"owningServiceId":   &filter.OwningServiceID,
		"ownerProfileId":    &filter.OwnerProfileID,
		"approverProfileId": &filter.ApproverProfileID,
		"frameworkId":       &filter.FrameworkID,
		"controlId":         &filter.ControlID,
	} {
		if *target, ok = parseOptionalUintQuery(c, name); !ok {
			return
		}
	}
	result, err := h.service.ListGovernancePolicies(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetGovernancePolicy(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	item, err := h.service.FindGovernancePolicy(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateGovernancePolicy(c *gin.Context) {
	var req GovernancePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Politique de gouvernance invalide"))
		return
	}
	// Create never accepts a client status. The service forces DRAFT.
	req.Status = ""
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.CreateGovernancePolicy(actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateGovernancePolicy(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req GovernancePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Politique de gouvernance invalide"))
		return
	}
	if rejectReservedStatus(c, req.Status, policyReservedStatuses) {
		return
	}
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := h.service.UpdateGovernancePolicy(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ApproveGovernancePolicy(c *gin.Context) {
	h.transitionGovernancePolicy(c, h.service.ApproveGovernancePolicy)
}

func (h *Handler) PublishGovernancePolicy(c *gin.Context) {
	h.transitionGovernancePolicy(c, h.service.PublishGovernancePolicy)
}

func (h *Handler) RetireGovernancePolicy(c *gin.Context) {
	h.transitionGovernancePolicy(c, h.service.RetireGovernancePolicy)
}

func (h *Handler) transitionGovernancePolicy(c *gin.Context, run func(uint, uint) (*GovernancePolicy, error)) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	actor, ok := currentUser(c)
	if !ok {
		return
	}
	item, err := run(id, actor)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
