package grc

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// parseStrictPage rejects malformed page/limit values with 400; bounds are
// still normalized by the service. The historical parsePage used by
// GRC-01A/B is left unchanged.
func parseStrictPage(c *gin.Context) (int, int, bool) {
	values := [2]int{1, 20}
	for i, name := range []string{"page", "limit"} {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			fail(c, coreerrors.BadRequest(name+" invalide"))
			return 0, 0, false
		}
		values[i] = value
	}
	return values[0], values[1], true
}

// parseOptionalBoolQuery accepts exactly "true" or "false".
func parseOptionalBoolQuery(c *gin.Context, name string) (*bool, bool) {
	switch raw := strings.TrimSpace(c.Query(name)); raw {
	case "":
		return nil, true
	case "true", "false":
		value := raw == "true"
		return &value, true
	default:
		fail(c, coreerrors.BadRequest(name+" invalide"))
		return nil, false
	}
}

// parseOptionalTimeQuery requires an RFC3339 instant with its offset; a
// civil date without timezone is rejected. The instant is converted to UTC so
// comparisons do not depend on the caller's offset.
func parseOptionalTimeQuery(c *gin.Context, name string) (*time.Time, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}

	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		fail(c, coreerrors.BadRequest(name+" invalide"))
		return nil, false
	}
	value = value.UTC()
	return &value, true
}

func (h *Handler) ListEvidence(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := EvidenceFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Type:   strings.TrimSpace(c.Query("type")),
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}

	if filter.ControlImplementationID, ok = parseOptionalUintQuery(c, "controlImplementationId"); !ok {
		return
	}
	if filter.OwnerProfileID, ok = parseOptionalUintQuery(c, "ownerProfileId"); !ok {
		return
	}
	if filter.OwningServiceID, ok = parseOptionalUintQuery(c, "owningServiceId"); !ok {
		return
	}
	if filter.Expired, ok = parseOptionalBoolQuery(c, "expired"); !ok {
		return
	}
	if filter.ExpiresBefore, ok = parseOptionalTimeQuery(c, "expiresBefore"); !ok {
		return
	}

	result, err := h.service.ListEvidence(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetEvidence(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindEvidence(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateEvidence(c *gin.Context) {
	var req EvidenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Preuve GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveEvidence(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateEvidence(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req EvidenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Preuve GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveEvidence(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListRemediationActions(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := RemediationActionFilter{
		Search:   strings.TrimSpace(c.Query("search")),
		Priority: strings.TrimSpace(c.Query("priority")),
		Status:   strings.TrimSpace(c.Query("status")),
		Page:     page,
		Limit:    limit,
	}

	if filter.ControlImplementationID, ok = parseOptionalUintQuery(c, "controlImplementationId"); !ok {
		return
	}
	if filter.RiskID, ok = parseOptionalUintQuery(c, "riskId"); !ok {
		return
	}
	if filter.AssigneeProfileID, ok = parseOptionalUintQuery(c, "assigneeProfileId"); !ok {
		return
	}
	if filter.OwningServiceID, ok = parseOptionalUintQuery(c, "owningServiceId"); !ok {
		return
	}
	if filter.Overdue, ok = parseOptionalBoolQuery(c, "overdue"); !ok {
		return
	}
	if filter.DueBefore, ok = parseOptionalTimeQuery(c, "dueBefore"); !ok {
		return
	}

	result, err := h.service.ListRemediationActions(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetRemediationAction(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindRemediationAction(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateRemediationAction(c *gin.Context) {
	var req RemediationActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Action de remédiation invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRemediationAction(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateRemediationAction(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req RemediationActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Action de remédiation invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveRemediationAction(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
