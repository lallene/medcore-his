package grc

import (
	"encoding/json"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/events"
)

const (
	auditModuleGRCAsset = "GRC_ASSET"
	auditModuleGRCRisk  = "GRC_RISK"

	auditActionCreate = "CREATE"
	auditActionUpdate = "UPDATE"
)

type assetAuditSnapshot struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Criticality     string `json:"criticality"`
	OwningServiceID *uint  `json:"owningServiceId,omitempty"`
	OwnerProfileID  *uint  `json:"ownerProfileId,omitempty"`
	Status          string `json:"status"`
}

type riskAuditSnapshot struct {
	Reference          string     `json:"reference"`
	Title              string     `json:"title"`
	AssetID            *uint      `json:"assetId,omitempty"`
	OwningServiceID    *uint      `json:"owningServiceId,omitempty"`
	OwnerProfileID     *uint      `json:"ownerProfileId,omitempty"`
	ApproverProfileID  *uint      `json:"approverProfileId,omitempty"`
	Likelihood         int        `json:"likelihood"`
	Impact             int        `json:"impact"`
	InherentScore      int        `json:"inherentScore"`
	ResidualLikelihood int        `json:"residualLikelihood"`
	ResidualImpact     int        `json:"residualImpact"`
	ResidualScore      int        `json:"residualScore"`
	Treatment          string     `json:"treatment"`
	TreatmentDueAt     *time.Time `json:"treatmentDueAt,omitempty"`
	Status             string     `json:"status"`
}

func assetAuditValue(asset Asset) string {
	value, err := json.Marshal(assetAuditSnapshot{
		Code:            asset.Code,
		Name:            asset.Name,
		Type:            asset.Type,
		Criticality:     asset.Criticality,
		OwningServiceID: asset.OwningServiceID,
		OwnerProfileID:  asset.OwnerProfileID,
		Status:          asset.Status,
	})
	if err != nil {
		return ""
	}
	return string(value)
}

func riskAuditValue(risk Risk) string {
	value, err := json.Marshal(riskAuditSnapshot{
		Reference:          risk.Reference,
		Title:              risk.Title,
		AssetID:            risk.AssetID,
		OwningServiceID:    risk.OwningServiceID,
		OwnerProfileID:     risk.OwnerProfileID,
		ApproverProfileID:  risk.ApproverProfileID,
		Likelihood:         risk.Likelihood,
		Impact:             risk.Impact,
		InherentScore:      risk.InherentScore,
		ResidualLikelihood: risk.ResidualLikelihood,
		ResidualImpact:     risk.ResidualImpact,
		ResidualScore:      risk.ResidualScore,
		Treatment:          risk.Treatment,
		TreatmentDueAt:     risk.TreatmentDueAt,
		Status:             risk.Status,
	})
	if err != nil {
		return ""
	}
	return string(value)
}

func publishGRCAudit(module, action string, recordID, actor uint, oldValue, newValue string) {
	userID := actor

	_ = events.DefaultBus.Publish(audit.AuditEvent{
		Module:   module,
		Action:   action,
		RecordID: recordID,
		UserID:   &userID,
		OldValue: oldValue,
		NewValue: newValue,
		At:       time.Now(),
	})
}
