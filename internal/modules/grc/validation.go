package grc

import "strings"

var validAssetTypes = map[string]bool{
	AssetTypeApplication:     true,
	AssetTypeInfrastructure:  true,
	AssetTypeServer:          true,
	AssetTypeDatabase:        true,
	AssetTypeNetwork:         true,
	AssetTypeData:            true,
	AssetTypeBusinessService: true,
	AssetTypeVendor:          true,
	AssetTypeProcess:         true,
	AssetTypeSite:            true,
}

var validCriticalities = map[string]bool{
	AssetCriticalityLow:      true,
	AssetCriticalityMedium:   true,
	AssetCriticalityHigh:     true,
	AssetCriticalityCritical: true,
}

var validAssetStatuses = map[string]bool{
	AssetStatusActive:   true,
	AssetStatusInactive: true,
	AssetStatusRetired:  true,
}

func normalizeCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeEnum(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeText(value string) string {
	return strings.TrimSpace(value)
}

func isValidAssetType(value string) bool {
	return validAssetTypes[value]
}

func isValidCriticality(value string) bool {
	return validCriticalities[value]
}

func isValidAssetStatus(value string) bool {
	return validAssetStatuses[value]
}

var validRiskTreatments = map[string]bool{
	RiskTreatmentReduce:   true,
	RiskTreatmentAccept:   true,
	RiskTreatmentAvoid:    true,
	RiskTreatmentTransfer: true,
}

var validRiskStatuses = map[string]bool{
	RiskStatusDraft:       true,
	RiskStatusOpen:        true,
	RiskStatusInTreatment: true,
	RiskStatusAccepted:    true,
	RiskStatusClosed:      true,
}

func isValidRiskTreatment(value string) bool {
	return validRiskTreatments[value]
}

func isValidRiskStatus(value string) bool {
	return validRiskStatuses[value]
}

var validFrameworkStatuses = map[string]bool{
	FrameworkStatusDraft:    true,
	FrameworkStatusActive:   true,
	FrameworkStatusArchived: true,
}

var validRequirementStatuses = map[string]bool{
	RequirementStatusActive:   true,
	RequirementStatusInactive: true,
}

var validControlStatuses = map[string]bool{
	ControlStatusDraft:    true,
	ControlStatusActive:   true,
	ControlStatusInactive: true,
	ControlStatusRetired:  true,
}

var validControlImplementationStatuses = map[string]bool{
	ControlImplementationStatusPlanned:        true,
	ControlImplementationStatusImplemented:    true,
	ControlImplementationStatusPartial:        true,
	ControlImplementationStatusNotImplemented: true,
	ControlImplementationStatusNotApplicable:  true,
}

func isValidFrameworkStatus(value string) bool {
	return validFrameworkStatuses[value]
}

func isValidRequirementStatus(value string) bool {
	return validRequirementStatuses[value]
}

func isValidControlStatus(value string) bool {
	return validControlStatuses[value]
}

func isValidControlImplementationStatus(value string) bool {
	return validControlImplementationStatuses[value]
}
