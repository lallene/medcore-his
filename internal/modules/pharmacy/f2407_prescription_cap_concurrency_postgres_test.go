package pharmacy

import (
	"fmt"
	"sync"
	"testing"
)

// LOT 24F F24-07 — prescription quantity cap must share the Dispense transaction.
//
// Production defect (RED only — do not fix here):
//   CreateDispensation (service.go) reads consultation_prescriptions.quantity and
//   SumDispensedQuantityForPrescription OUTSIDE repo.Dispense's transaction.
//   Dispense locks PharmacyStock / PharmacyBatch (FOR UPDATE) but never re-locks
//   the prescription row or re-validates remaining quantity before commit.
//
// Race: two concurrent requests each see remaining >= request qty, both pass the
// pre-tx guard, then serialize only on stock locks and collectively exceed the
// prescribed quantity.
//
// Idempotency: distinct keys required so the second request is not a replay.

func TestPostgresF2407_ConcurrentDispensationCannotExceedPrescriptionQuantity(t *testing.T) {
	db := pharmacyDB(t)
	s, p, userID := seedPharmacy(t, db)

	const prescribed = float64(10)
	const eachQty = float64(8) // individually valid vs remaining=10; together 16 > 10
	if p.Quantity != prescribed {
		t.Fatalf("seed prescribed quantity=%v want %v", p.Quantity, prescribed)
	}

	var stockBefore PharmacyStock
	if err := db.Where("presentation_id=?", *p.PresentationID).First(&stockBefore).Error; err != nil {
		t.Fatal(err)
	}
	if stockBefore.QuantityAvailable < eachQty*2 {
		t.Fatalf("stock must be abundant so only the prescription cap is contested: available=%v", stockBefore.QuantityAvailable)
	}

	var wg sync.WaitGroup
	type outcome struct {
		err error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, err := s.CreateDispensation(CreateDispensationRequest{
				PresentationID: *p.PresentationID,
				PrescriptionID: &p.ID,
				Quantity:       eachQty,
				IdempotencyKey: fmt.Sprintf("F2407-CONCURRENT-%d", n),
			}, userID)
			results <- outcome{err: err}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	success, failed := 0, 0
	for r := range results {
		if r.err == nil {
			success++
		} else {
			failed++
		}
	}

	var totalDispensed float64
	if err := db.Model(&PharmacyDispensation{}).
		Where("reference_type = ? AND reference_id = ?", "CONSULTATION_PRESCRIPTION", p.ID).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&totalDispensed).Error; err != nil {
		t.Fatal(err)
	}

	var stockAfter PharmacyStock
	if err := db.Where("presentation_id=?", *p.PresentationID).First(&stockAfter).Error; err != nil {
		t.Fatal(err)
	}
	stockDelta := stockBefore.QuantityAvailable - stockAfter.QuantityAvailable

	var dispensationCount int64
	if err := db.Model(&PharmacyDispensation{}).
		Where("reference_type = ? AND reference_id = ?", "CONSULTATION_PRESCRIPTION", p.ID).
		Count(&dispensationCount).Error; err != nil {
		t.Fatal(err)
	}

	if success > 1 {
		t.Fatalf("prescription over-dispense race: both concurrent CreateDispensation succeeded (success=%d failed=%d); totalDispensed=%v prescribed=%v stockDelta=%v",
			success, failed, totalDispensed, prescribed, stockDelta)
	}
	if totalDispensed > prescribed {
		t.Fatalf("total dispensed %v exceeds prescribed %v (success=%d failed=%d stockDelta=%v)",
			totalDispensed, prescribed, success, failed, stockDelta)
	}
	if stockDelta != totalDispensed {
		t.Fatalf("stock reduction %v must equal committed dispensation total %v", stockDelta, totalDispensed)
	}
	if success == 1 && failed != 1 {
		t.Fatalf("when one succeeds, the conflicting peer must fail: success=%d failed=%d", success, failed)
	}
	if success == 1 && dispensationCount != 1 {
		t.Fatalf("failed peer must leave no partial dispensation row: count=%d", dispensationCount)
	}
}
