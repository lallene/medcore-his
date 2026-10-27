package grc

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"gorm.io/gorm"
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

var errGRCAuditRejected = errors.New("grc audit rejected")

// insertGRCAuditRow is the only AuditLog write used by GRC mutations.
// Tests replace it to force a deterministic audit failure.
var insertGRCAuditRow = func(tx *gorm.DB, row *audit.AuditLog) error {
	return tx.Create(row).Error
}

// afterGRCAudit records the event for tests after the row is staged in the
// same transaction. It does not write a second row and does not use MemoryBus.
var afterGRCAudit func(audit.AuditEvent)

// writeGRCAudit inserts the audit row on the caller's transaction. The
// business mutation and this insert commit or roll back together. A failure
// is returned without the snapshot, so the caller does not report success.
func writeGRCAudit(tx *gorm.DB, module, action string, recordID, actor uint, oldValue, newValue string) error {
	if tx == nil || recordID == 0 || actor == 0 {
		slog.Error("grc audit write rejected", "module", module, "action", action, "recordId", recordID)
		return errGRCAuditRejected
	}
	userID := actor
	now := time.Now().UTC()
	row := &audit.AuditLog{
		Module:     module,
		Action:     action,
		RecordID:   recordID,
		UserID:     &userID,
		OldValue:   oldValue,
		NewValue:   newValue,
		OccurredAt: now,
	}
	if err := insertGRCAuditRow(tx, row); err != nil {
		slog.Error("grc audit write failed", "module", module, "action", action, "recordId", recordID)
		return err
	}
	if afterGRCAudit != nil {
		afterGRCAudit(audit.AuditEvent{
			Module:   module,
			Action:   action,
			RecordID: recordID,
			UserID:   &userID,
			OldValue: oldValue,
			NewValue: newValue,
			At:       now,
		})
	}
	return nil
}

// saveWithAudit commits one business write and its audit row together.
func (s *GRCService) saveWithAudit(write func(tx *gorm.DB) error, module, action string, id func() uint, actor uint, oldValue string, newValue func() string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := write(tx); err != nil {
			return err
		}
		return writeGRCAudit(tx, module, action, id(), actor, oldValue, newValue())
	})
}
