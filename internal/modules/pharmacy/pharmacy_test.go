package pharmacy

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPharmacyTableNamesStockStatusesAndRoutes(t *testing.T) {
	tables := map[string]string{
		(MedicationFamily{}).TableName(): "medication_families", (Medication{}).TableName(): "medications",
		(MedicationPresentation{}).TableName(): "medication_presentations", (PharmacyStock{}).TableName(): "pharmacy_stocks",
		(PharmacyBatch{}).TableName(): "pharmacy_batches", (StockMovement{}).TableName(): "stock_movements",
		(PharmacyDispensation{}).TableName(): "pharmacy_dispensations", (PharmacyDispensationItem{}).TableName(): "pharmacy_dispensation_items",
		(PharmacyVoucher{}).TableName(): "pharmacy_vouchers", (PharmacyVoucherLine{}).TableName(): "pharmacy_voucher_lines",
	}
	for got, want := range tables {
		if got != want {
			t.Fatalf("table %q, attendu %q", got, want)
		}
	}
	cases := []struct {
		stock PharmacyStock
		want  string
	}{
		{PharmacyStock{}, "not_managed"},
		{PharmacyStock{IsStockManaged: true, QuantityAvailable: 0}, "out_of_stock"},
		{PharmacyStock{IsStockManaged: true, QuantityAvailable: 5, AlertThreshold: 5}, "low_stock"},
		{PharmacyStock{IsStockManaged: true, QuantityAvailable: 6, AlertThreshold: 5}, "available"},
	}
	for _, tc := range cases {
		if got := tc.stock.Status(); got != tc.want {
			t.Fatalf("status=%s attendu=%s", got, tc.want)
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New().Group("/api")
	RegisterRoutesWithHandler(router, NewHandler(NewService(&Repository{})))
}
