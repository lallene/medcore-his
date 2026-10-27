package grc

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// Sensitive transitions are reserved to dedicated routes guarded by their own
// permission: the generic PUT refuses them, even for a wildcard holder, so
// *.update can never bypass *.complete, *.close or *.accept.

func rejectReservedStatus(c *gin.Context, status string, reserved map[string]string) bool {
	if route, ok := reserved[normalizeEnum(status)]; ok {
		fail(c, coreerrors.Forbidden("Ce changement de statut passe par la route dédiée "+route))
		return true
	}
	return false
}

var (
	assessmentReservedStatuses = map[string]string{AssessmentStatusCompleted: "/complete"}
	auditReservedStatuses      = map[string]string{AuditEngagementStatusCompleted: "/complete"}
	findingReservedStatuses    = map[string]string{FindingStatusClosed: "/close", FindingStatusAccepted: "/accept"}
	incidentReservedStatuses   = map[string]string{SecurityIncidentStatusClosed: "/close"}
	policyReservedStatuses     = map[string]string{
		GovernancePolicyStatusApproved:  "/approve",
		GovernancePolicyStatusPublished: "/publish",
		GovernancePolicyStatusRetired:   "/retire",
	}
)

// --- Assessment ---

func assessmentRequestFrom(a Assessment) AssessmentRequest {
	return AssessmentRequest{
		Reference:               a.Reference,
		Title:                   a.Title,
		ControlImplementationID: a.ControlImplementationID,
		Type:                    a.Type,
		Status:                  a.Status,
		Result:                  a.Result,
		AssessorProfileID:       a.AssessorProfileID,
		AssessedAt:              a.AssessedAt,
		NextAssessmentAt:        a.NextAssessmentAt,
		Summary:                 a.Summary,
	}
}

func (h *Handler) ListAssessments(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := AssessmentFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Type:   strings.TrimSpace(c.Query("type")),
		Status: strings.TrimSpace(c.Query("status")),
		Result: strings.TrimSpace(c.Query("result")),
		Page:   page,
		Limit:  limit,
	}
	if filter.ControlImplementationID, ok = parseOptionalUintQuery(c, "controlImplementationId"); !ok {
		return
	}
	if filter.EvidenceID, ok = parseOptionalUintQuery(c, "evidenceId"); !ok {
		return
	}
	if filter.AssessorProfileID, ok = parseOptionalUintQuery(c, "assessorProfileId"); !ok {
		return
	}
	if filter.ReassessmentDue, ok = parseOptionalBoolQuery(c, "reassessmentDue"); !ok {
		return
	}
	if filter.HasInvalidEvidence, ok = parseOptionalBoolQuery(c, "hasInvalidEvidence"); !ok {
		return
	}
	if filter.AssessedAfter, ok = parseOptionalTimeQuery(c, "assessedAfter"); !ok {
		return
	}
	if filter.AssessedBefore, ok = parseOptionalTimeQuery(c, "assessedBefore"); !ok {
		return
	}

	result, err := h.service.ListAssessments(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetAssessment(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindAssessment(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateAssessment(c *gin.Context) {
	var req AssessmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Évaluation GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAssessment(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateAssessment(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req AssessmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Évaluation GRC invalide"))
		return
	}
	if rejectReservedStatus(c, req.Status, assessmentReservedStatuses) {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAssessment(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CompleteAssessment(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var body AssessmentCompleteRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, coreerrors.BadRequest("Clôture d'évaluation invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	current, err := h.service.FindAssessment(id)
	if err != nil {
		fail(c, err)
		return
	}

	req := assessmentRequestFrom(*current)
	req.Status = AssessmentStatusCompleted
	if body.Result != nil {
		req.Result = body.Result
	}
	if body.AssessorProfileID != nil {
		req.AssessorProfileID = body.AssessorProfileID
	}
	if body.AssessedAt != nil {
		req.AssessedAt = body.AssessedAt
	}
	if body.NextAssessmentAt != nil {
		req.NextAssessmentAt = body.NextAssessmentAt
	}
	if body.Summary != nil {
		req.Summary = *body.Summary
	}

	item, err := h.service.SaveAssessment(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) ListAssessmentEvidence(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	links, err := h.service.ListAssessmentEvidence(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": links})
}

func (h *Handler) LinkAssessmentEvidence(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req AssessmentEvidenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Lien évaluation-preuve invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.LinkAssessmentEvidence(actor, id, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UnlinkAssessmentEvidence(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	evidenceID, ok := parseUintParam(c, "evidenceId")
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	if err := h.service.UnlinkAssessmentEvidence(actor, id, evidenceID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// --- AuditEngagement ---

func auditEngagementRequestFrom(a AuditEngagement) AuditEngagementRequest {
	return AuditEngagementRequest{
		Reference:            a.Reference,
		Title:                a.Title,
		Description:          a.Description,
		Scope:                a.Scope,
		Type:                 a.Type,
		Status:               a.Status,
		LeadAuditorProfileID: a.LeadAuditorProfileID,
		OwningServiceID:      a.OwningServiceID,
		PlannedStartAt:       a.PlannedStartAt,
		PlannedEndAt:         a.PlannedEndAt,
		StartedAt:            a.StartedAt,
	}
}

func (h *Handler) ListAuditEngagements(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := AuditEngagementFilter{
		Search: strings.TrimSpace(c.Query("search")),
		Type:   strings.TrimSpace(c.Query("type")),
		Status: strings.TrimSpace(c.Query("status")),
		Page:   page,
		Limit:  limit,
	}
	if filter.LeadAuditorProfileID, ok = parseOptionalUintQuery(c, "leadAuditorProfileId"); !ok {
		return
	}
	if filter.OwningServiceID, ok = parseOptionalUintQuery(c, "owningServiceId"); !ok {
		return
	}
	if filter.PlannedStartAfter, ok = parseOptionalTimeQuery(c, "plannedStartAfter"); !ok {
		return
	}
	if filter.PlannedStartBefore, ok = parseOptionalTimeQuery(c, "plannedStartBefore"); !ok {
		return
	}

	result, err := h.service.ListAuditEngagements(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetAuditEngagement(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindAuditEngagement(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateAuditEngagement(c *gin.Context) {
	var req AuditEngagementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Audit GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAuditEngagement(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateAuditEngagement(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req AuditEngagementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Audit GRC invalide"))
		return
	}
	if rejectReservedStatus(c, req.Status, auditReservedStatuses) {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveAuditEngagement(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// CompleteAuditEngagement takes no body: CompletedAt is set by the service.
func (h *Handler) CompleteAuditEngagement(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	current, err := h.service.FindAuditEngagement(id)
	if err != nil {
		fail(c, err)
		return
	}

	req := auditEngagementRequestFrom(*current)
	req.Status = AuditEngagementStatusCompleted

	item, err := h.service.SaveAuditEngagement(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// --- Finding ---

func findingRequestFrom(f Finding) FindingRequest {
	return FindingRequest{
		Reference:               f.Reference,
		Title:                   f.Title,
		Description:             f.Description,
		AuditEngagementID:       f.AuditEngagementID,
		AssessmentID:            f.AssessmentID,
		Category:                f.Category,
		Severity:                f.Severity,
		Status:                  f.Status,
		ControlImplementationID: f.ControlImplementationID,
		RiskID:                  f.RiskID,
		OwnerProfileID:          f.OwnerProfileID,
		OwningServiceID:         f.OwningServiceID,
		DueAt:                   f.DueAt,
		ClosureNote:             f.ClosureNote,
	}
}

func (h *Handler) ListFindings(c *gin.Context) {
	page, limit, ok := parseStrictPage(c)
	if !ok {
		return
	}

	filter := FindingFilter{
		Search:   strings.TrimSpace(c.Query("search")),
		Category: strings.TrimSpace(c.Query("category")),
		Severity: strings.TrimSpace(c.Query("severity")),
		Status:   strings.TrimSpace(c.Query("status")),
		Page:     page,
		Limit:    limit,
	}
	for name, target := range map[string]**uint{
		"auditEngagementId":       &filter.AuditEngagementID,
		"assessmentId":            &filter.AssessmentID,
		"controlImplementationId": &filter.ControlImplementationID,
		"riskId":                  &filter.RiskID,
		"ownerProfileId":          &filter.OwnerProfileID,
		"owningServiceId":         &filter.OwningServiceID,
	} {
		if *target, ok = parseOptionalUintQuery(c, name); !ok {
			return
		}
	}
	if filter.Overdue, ok = parseOptionalBoolQuery(c, "overdue"); !ok {
		return
	}
	if filter.DueBefore, ok = parseOptionalTimeQuery(c, "dueBefore"); !ok {
		return
	}

	result, err := h.service.ListFindings(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) GetFinding(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	item, err := h.service.FindFinding(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) CreateFinding(c *gin.Context) {
	var req FindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Constat GRC invalide"))
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveFinding(0, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *Handler) UpdateFinding(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req FindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, coreerrors.BadRequest("Constat GRC invalide"))
		return
	}
	if rejectReservedStatus(c, req.Status, findingReservedStatuses) {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	item, err := h.service.SaveFinding(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

// CloseFinding takes no body: ClosedAt is set by the service, which also
// requires every linked remediation action to be done or cancelled.
func (h *Handler) CloseFinding(c *gin.Context) {
	h.transitionFinding(c, FindingStatusClosed, nil)
}

func (h *Handler) AcceptFinding(c *gin.Context) {
	if _, ok := parseID(c); !ok {
		return
	}

	var body FindingAcceptRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, coreerrors.BadRequest("Acceptation de constat invalide"))
		return
	}
	h.transitionFinding(c, FindingStatusAccepted, &body.ClosureNote)
}

func (h *Handler) transitionFinding(c *gin.Context, status string, closureNote *string) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	actor, ok := currentUser(c)
	if !ok {
		return
	}

	current, err := h.service.FindFinding(id)
	if err != nil {
		fail(c, err)
		return
	}

	req := findingRequestFrom(*current)
	req.Status = status
	if closureNote != nil {
		req.ClosureNote = *closureNote
	}

	item, err := h.service.SaveFinding(id, actor, req)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}
