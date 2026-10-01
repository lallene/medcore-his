package performed_acts

import (
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
)

// applyCatalogSnapshot copies LOT27C catalogue fields onto a PerformedAct.
// Shared by manual Create and EnsureFromProducer — single authoritative path.
func applyCatalogSnapshot(item *Act, catalog act_catalog.Entry) {
	item.ActCatalogEntryID = catalog.ID
	item.ActCode = catalog.Code
	item.ActLabel = catalog.Label
	item.ActDescription = catalog.Description
	item.ActCategory = catalog.Category
	item.BasePrice = catalog.BasePrice
	item.Currency = catalog.Currency
	item.Billable = catalog.Billable
	item.InsuranceEligible = catalog.InsuranceEligible
}
