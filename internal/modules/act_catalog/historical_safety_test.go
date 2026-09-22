package act_catalog

import (
	"reflect"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
)

// Historical safety (source-level): ActCatalog must not be wired into invoice lines.
// Invoice lines snapshot UnitPrice independently; BasePrice changes must not cascade.
func TestHistoricalSafety_NoActCatalogOnInvoiceLine(t *testing.T) {
	t.Parallel()
	lineType := reflect.TypeOf(billing.InvoiceLine{})
	if _, ok := lineType.FieldByName("ActCatalogID"); ok {
		t.Fatal("InvoiceLine must not reference ActCatalogID in LOT27B")
	}
	if _, ok := lineType.FieldByName("ActCatalogEntryID"); ok {
		t.Fatal("InvoiceLine must not reference ActCatalogEntryID in LOT27B")
	}
	entryType := reflect.TypeOf(Entry{})
	if _, ok := entryType.FieldByName("TariffID"); ok {
		t.Fatal("ActCatalog Entry must not own TariffID in LOT27B")
	}
}
