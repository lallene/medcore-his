package complete

import (
	"fmt"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
)

// Pharmacy stock life through pharmacy.Service: initial receipts (two batches with different
// expiries for FEFO), twice-weekly deliveries of low items, and two references never
// re-ordered (supplier shortage) so that LOW_STOCK / OUT_OF_STOCK are visible.

type pharmacyPlanner struct{}

func (pharmacyPlanner) Name() string { return "Pharmacie (stocks)" }

func (pharmacyPlanner) Setup(w *World) error {
	g := w.RNG.Stream("pharmacy.setup")
	for i, p := range w.Ref.Presentations {
		qty := float64(g.Between(6, 30) * 50)
		exp1 := w.Cfg.Anchor.AddDate(0, g.Between(1, 5), 0)
		exp2 := w.Cfg.Anchor.AddDate(1, g.Between(0, 11), 0)
		for k, exp := range []time.Time{exp1, exp2} {
			if _, err := w.Svc.Pharmacy.CreateBatch(pharmacy.CreatePharmacyBatchRequest{PresentationID: p.ID, BatchNumber: fmt.Sprintf("DC-%s-%02d%d", p.Code, i, k), QuantityReceived: qty / 2,
				ExpirationDate: exp.Format("2006-01-02"), Supplier: Pick(g, suppliers), PurchasePrice: float64(g.Between(2, 40) * 100)}); err != nil {
				return wrapf(err, "lot initial %s", p.Code)
			}
		}
	}
	return nil
}

var suppliers = []string{"Laborex CI", "Copharmed", "DPCI", "Tedis Pharma", "PSP Côte d'Ivoire"}

func (pharmacyPlanner) PlanDay(w *World, d Day) {
	if wd := d.Date.Weekday(); wd != time.Monday && wd != time.Thursday {
		return
	}
	g := w.RNG.Stream("pharmacy.day", uint64(d.Index))
	w.Sched.At(d.At(8, 15), "pharmacie.livraison", func(ctx *StepContext) error {
		var stocks []struct {
			PresentationID    uint
			QuantityAvailable float64
		}
		if err := w.DB.Table("pharmacy_stocks").Select("presentation_id, quantity_available").Scan(&stocks).Error; err != nil {
			return err
		}
		level := map[uint]float64{}
		for _, s := range stocks {
			level[s.PresentationID] = s.QuantityAvailable
		}
		for i, p := range w.Ref.Presentations {
			if i == 1 || i == 4 { // supplier shortage: never delivered again
				continue
			}
			if level[p.ID] >= 250 {
				continue
			}
			if _, err := w.Svc.Pharmacy.CreateBatch(pharmacy.CreatePharmacyBatchRequest{PresentationID: p.ID, BatchNumber: fmt.Sprintf("DC-%s-%s", p.Code, ctx.Sim.Format("060102")),
				QuantityReceived: float64(g.Between(8, 24) * 50), ExpirationDate: ctx.Sim.AddDate(0, g.Between(8, 30), 0).Format("2006-01-02"), Supplier: Pick(g, suppliers), PurchasePrice: float64(g.Between(2, 40) * 100)}); err != nil {
				return err
			}
		}
		return nil
	})
}
