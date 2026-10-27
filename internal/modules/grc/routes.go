package grc

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/grc")

	// Read-only aggregates: no mutation, so no audit event.
	g.GET("/dashboard", rbac.Permission("grc.dashboard.read"), h.GetDashboard)
	// Analytical read model. It does not replace the dashboard and does not
	// publish an audit event.
	g.GET("/reporting", rbac.Permission("grc.reporting.read"), h.GetReporting)

	assets := g.Group("/assets")
	assets.GET("", rbac.Permission("grc.asset.read"), h.ListAssets)
	assets.GET("/:id", rbac.Permission("grc.asset.read"), h.GetAsset)
	assets.POST("", rbac.Permission("grc.asset.manage"), h.CreateAsset)
	assets.PUT("/:id", rbac.Permission("grc.asset.manage"), h.UpdateAsset)

	risks := g.Group("/risks")
	risks.GET("", rbac.Permission("grc.risk.read"), h.ListRisks)
	risks.GET("/:id", rbac.Permission("grc.risk.read"), h.GetRisk)
	risks.POST("", rbac.Permission("grc.risk.create"), h.CreateRisk)
	risks.PUT("/:id", rbac.Permission("grc.risk.update"), h.UpdateRisk)

	frameworks := g.Group("/frameworks")
	frameworks.GET("", rbac.Permission("grc.framework.read"), h.ListFrameworks)
	frameworks.GET("/:id", rbac.Permission("grc.framework.read"), h.GetFramework)
	frameworks.POST("", rbac.Permission("grc.framework.manage"), h.CreateFramework)
	frameworks.PUT("/:id", rbac.Permission("grc.framework.manage"), h.UpdateFramework)

	// Read-only control mapping coverage (same definition as the dashboard);
	// it aggregates requirements of frameworks, hence both read permissions.
	g.GET("/compliance/coverage",
		rbac.Permission("grc.framework.read"), rbac.Permission("grc.requirement.read"), h.GetComplianceCoverage)

	requirements := g.Group("/requirements")
	requirements.GET("", rbac.Permission("grc.requirement.read"), h.ListRequirements)
	requirements.GET("/:id", rbac.Permission("grc.requirement.read"), h.GetRequirement)
	requirements.POST("", rbac.Permission("grc.requirement.manage"), h.CreateRequirement)
	requirements.PUT("/:id", rbac.Permission("grc.requirement.manage"), h.UpdateRequirement)

	controls := g.Group("/controls")
	controls.GET("", rbac.Permission("grc.control.read"), h.ListControls)
	controls.GET("/:id", rbac.Permission("grc.control.read"), h.GetControl)
	controls.POST("", rbac.Permission("grc.control.manage"), h.CreateControl)
	controls.PUT("/:id", rbac.Permission("grc.control.manage"), h.UpdateControl)

	implementations := g.Group("/control-implementations")
	implementations.GET("", rbac.Permission("grc.control.implementation.read"), h.ListControlImplementations)
	implementations.GET("/:id", rbac.Permission("grc.control.implementation.read"), h.GetControlImplementation)
	implementations.POST("", rbac.Permission("grc.control.implementation.manage"), h.CreateControlImplementation)
	implementations.PUT("/:id", rbac.Permission("grc.control.implementation.manage"), h.UpdateControlImplementation)

	// Explicit relations are managed with the control permission.
	requirementControls := g.Group("/requirement-controls")
	requirementControls.POST("", rbac.Permission("grc.control.manage"), h.LinkRequirementControl)
	requirementControls.DELETE("/:requirementId/:controlId", rbac.Permission("grc.control.manage"), h.UnlinkRequirementControl)

	controlRisks := g.Group("/control-risks")
	controlRisks.POST("", rbac.Permission("grc.control.manage"), h.LinkControlRisk)
	controlRisks.DELETE("/:controlId/:riskId", rbac.Permission("grc.control.manage"), h.UnlinkControlRisk)

	// Evidence is metadata only: no upload, download or delete route.
	evidence := g.Group("/evidence")
	evidence.GET("", rbac.Permission("grc.evidence.read"), h.ListEvidence)
	evidence.GET("/:id", rbac.Permission("grc.evidence.read"), h.GetEvidence)
	evidence.POST("", rbac.Permission("grc.evidence.manage"), h.CreateEvidence)
	evidence.PUT("/:id", rbac.Permission("grc.evidence.manage"), h.UpdateEvidence)

	// Status changes (completion, reopening, cancellation) go through PUT.
	actions := g.Group("/actions")
	actions.GET("", rbac.Permission("grc.action.read"), h.ListRemediationActions)
	actions.GET("/:id", rbac.Permission("grc.action.read"), h.GetRemediationAction)
	actions.POST("", rbac.Permission("grc.action.create"), h.CreateRemediationAction)
	actions.PUT("/:id", rbac.Permission("grc.action.update"), h.UpdateRemediationAction)

	// Sensitive transitions have dedicated routes and permissions; the PUT
	// routes refuse them (separation of duties).
	assessments := g.Group("/assessments")
	assessments.GET("", rbac.Permission("grc.assessment.read"), h.ListAssessments)
	assessments.GET("/:id", rbac.Permission("grc.assessment.read"), h.GetAssessment)
	assessments.POST("", rbac.Permission("grc.assessment.create"), h.CreateAssessment)
	assessments.PUT("/:id", rbac.Permission("grc.assessment.update"), h.UpdateAssessment)
	assessments.POST("/:id/complete", rbac.Permission("grc.assessment.complete"), h.CompleteAssessment)
	assessments.GET("/:id/evidence", rbac.Permission("grc.assessment.read"), h.ListAssessmentEvidence)
	assessments.POST("/:id/evidence", rbac.Permission("grc.assessment.update"), h.LinkAssessmentEvidence)
	assessments.DELETE("/:id/evidence/:evidenceId", rbac.Permission("grc.assessment.update"), h.UnlinkAssessmentEvidence)

	audits := g.Group("/audits")
	audits.GET("", rbac.Permission("grc.audit.read"), h.ListAuditEngagements)
	audits.GET("/:id", rbac.Permission("grc.audit.read"), h.GetAuditEngagement)
	audits.POST("", rbac.Permission("grc.audit.create"), h.CreateAuditEngagement)
	audits.PUT("/:id", rbac.Permission("grc.audit.update"), h.UpdateAuditEngagement)
	audits.POST("/:id/complete", rbac.Permission("grc.audit.complete"), h.CompleteAuditEngagement)

	findings := g.Group("/findings")
	findings.GET("", rbac.Permission("grc.finding.read"), h.ListFindings)
	findings.GET("/:id", rbac.Permission("grc.finding.read"), h.GetFinding)
	findings.POST("", rbac.Permission("grc.finding.create"), h.CreateFinding)
	findings.PUT("/:id", rbac.Permission("grc.finding.update"), h.UpdateFinding)
	findings.POST("/:id/close", rbac.Permission("grc.finding.close"), h.CloseFinding)
	findings.POST("/:id/accept", rbac.Permission("grc.finding.accept"), h.AcceptFinding)

	incidents := g.Group("/incidents")
	incidents.GET("", rbac.Permission("grc.incident.read"), h.ListSecurityIncidents)
	incidents.GET("/:id", rbac.Permission("grc.incident.read"), h.GetSecurityIncident)
	incidents.POST("", rbac.Permission("grc.incident.create"), h.CreateSecurityIncident)
	incidents.PUT("/:id", rbac.Permission("grc.incident.update"), h.UpdateSecurityIncident)
	incidents.POST("/:id/close", rbac.Permission("grc.incident.close"), h.CloseSecurityIncident)

	policies := g.Group("/policies")
	policies.GET("", rbac.Permission("grc.policy.read"), h.ListGovernancePolicies)
	policies.GET("/:id", rbac.Permission("grc.policy.read"), h.GetGovernancePolicy)
	policies.POST("", rbac.Permission("grc.policy.create"), h.CreateGovernancePolicy)
	policies.PUT("/:id", rbac.Permission("grc.policy.update"), h.UpdateGovernancePolicy)
	policies.POST("/:id/approve", rbac.Permission("grc.policy.approve"), h.ApproveGovernancePolicy)
	policies.POST("/:id/publish", rbac.Permission("grc.policy.publish"), h.PublishGovernancePolicy)
	policies.POST("/:id/retire", rbac.Permission("grc.policy.retire"), h.RetireGovernancePolicy)
}
