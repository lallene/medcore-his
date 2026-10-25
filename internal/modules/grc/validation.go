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
