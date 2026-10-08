package cash

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

func movementCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if e := db.Model(&CashMovement{}).Count(&n).Error; e != nil {
		t.Fatal(e)
	}
	return n
}

func TestLOT29F_C_CashMovementMatrix(t *testing.T) {
	t.Run("CM01_CM10_create_in_out", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 41, Name: "Directeur"})
		db.Create(&cashPatient{ID: 41, Nom: "P", Prenoms: "M", CodePatient: "P-CM01"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM01", Name: "Caisse CM01"}, 41)
		open, e := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "cm01-o"}, 41)
		if e != nil {
			t.Fatal(e)
		}
		in, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 5000, Reason: "Apport fonds", IdempotencyKey: "cm01-in",
		}, 41)
		if e != nil || in.Amount != 5000 || in.CreatedBy != 41 || in.Direction != MovementIn {
			t.Fatalf("CM01 %+v %v", in, e)
		}
		if in.OccurredAt.IsZero() {
			t.Fatal("CM08 timestamp")
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.ExpectedCash != 15000 || sum.CashMovementIn != 5000 || sum.CashCollected != 0 {
			t.Fatalf("CM27 IN expected %+v", sum)
		}
		out, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementOut, Type: MovementManualOut, Amount: 3000, Reason: "Retrait coffre", IdempotencyKey: "cm01-out",
		}, 41)
		if e != nil || out.Direction != MovementOut {
			t.Fatalf("CM02 %v", e)
		}
		sum2, _ := s.Get(open.Session.ID)
		if sum2.ExpectedCash != 12000 || sum2.CashMovementOut != 3000 || sum2.NetCashMovement != 2000 {
			t.Fatalf("CM28 OUT expected %+v", sum2)
		}
		if sum2.Session.Register.Code != "CM01" {
			t.Fatal("CM10 register derived")
		}
	})

	t.Run("CM03_CM06_validation", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 42, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM03", Name: "C"}, 42)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "cm03-o"}, 42)
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 0, Reason: "Zero", IdempotencyKey: "cm03-z",
		}, 42); cashErrCode(e) != "BAD_REQUEST" {
			t.Fatalf("CM04 %v", e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: -1, Reason: "Neg", IdempotencyKey: "cm03-n",
		}, 42); cashErrCode(e) != "BAD_REQUEST" {
			t.Fatalf("CM05 %v", e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 100, Reason: "  ", IdempotencyKey: "cm03-r",
		}, 42); cashErrCode(e) != "BAD_REQUEST" {
			t.Fatalf("CM06 %v", e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: "REFUND", Amount: 100, Reason: "Bad type", IdempotencyKey: "cm03-t",
		}, 42); cashErrCode(e) != "BAD_REQUEST" {
			t.Fatalf("unsupported type %v", e)
		}
	})

	t.Run("CM11_CM12_open_closed", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 43, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM11", Name: "C"}, 43)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 2000, IdempotencyKey: "cm11-o"}, 43)
		closed, _ := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 2000, IdempotencyKey: "cm11-c"}, 43, false)
		if _, e := s.CreateMovement(closed.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 100, Reason: "Après clôture", IdempotencyKey: "cm12",
		}, 43); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("CM12 %v", e)
		}
		if movementCount(t, db) != 0 {
			t.Fatal("CM12 side effect")
		}
	})

	t.Run("CM16_CM18_idempotency", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 44, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM16", Name: "C"}, 44)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 8000, IdempotencyKey: "cm16-o"}, 44)
		req := MovementRequest{Direction: MovementIn, Type: MovementManualIn, Amount: 1000, Reason: "Replay", IdempotencyKey: "cm16-k"}
		a, e := s.CreateMovement(open.Session.ID, req, 44)
		if e != nil {
			t.Fatal(e)
		}
		b, e := s.CreateMovement(open.Session.ID, req, 44)
		if e != nil || b.ID != a.ID {
			t.Fatalf("CM16 %+v %v", b, e)
		}
		if movementCount(t, db) != 1 {
			t.Fatal("CM18")
		}
		req.Reason = "Autre motif"
		if _, e := s.CreateMovement(open.Session.ID, req, 44); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("CM17 %v", e)
		}
	})

	t.Run("CM19_CM20_concurrent_movements", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 45, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM19", Name: "C"}, 45)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 20000, IdempotencyKey: "cm19-o"}, 45)
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, e := s.CreateMovement(open.Session.ID, MovementRequest{
					Direction: MovementIn, Type: MovementManualIn, Amount: 1000, Reason: "Concurrent IN",
					IdempotencyKey: fmt.Sprintf("cm19-in-%d", i),
				}, 45)
				errs <- e
			}(i)
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.CashMovementIn != 2000 || sum.ExpectedCash != 22000 {
			t.Fatalf("CM19 %+v", sum)
		}
	})

	t.Run("CM21_CM23_movement_vs_close", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 46, Name: "Dir"})
		db.Create(&cashPatient{ID: 46, Nom: "P", Prenoms: "C", CodePatient: "P-CM21"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM21", Name: "C"}, 46)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "cm21-o"}, 46)
		inv := seedCashInvoice(t, db, "INV-CM21", 46, 5000, 46)
		if _, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 5000, PaymentMethod: "CASH", IdempotencyKey: "cm21-pay", Payer: &PayerRequest{Mode: "PATIENT"}}, 46); e != nil {
			t.Fatal(e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 2000, Reason: "Avant close", IdempotencyKey: "cm21-m",
		}, 46); e != nil {
			t.Fatal(e)
		}
		// expected = 10000 + 5000 + 2000 = 17000
		closed, e := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 17000, IdempotencyKey: "cm21-c"}, 46, false)
		if e != nil {
			t.Fatal(e)
		}
		if closed.ExpectedCash != 17000 || *closed.Session.ExpectedCashAmount != 17000 {
			t.Fatalf("CM23 %+v", closed)
		}
		if closed.CashCollected != 5000 || closed.CashMovementIn != 2000 {
			t.Fatalf("CM29/33 %+v", closed)
		}
	})

	t.Run("CM22_close_first_rejects_movement", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 47, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM22", Name: "C"}, 47)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 1000, IdempotencyKey: "cm22-o"}, 47)
		var wg sync.WaitGroup
		var closeErr, movErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, closeErr = s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 1000, IdempotencyKey: "cm22-c"}, 47, false)
		}()
		go func() {
			defer wg.Done()
			_, movErr = s.CreateMovement(open.Session.ID, MovementRequest{
				Direction: MovementIn, Type: MovementManualIn, Amount: 500, Reason: "Race close", IdempotencyKey: "cm22-m",
			}, 47)
		}()
		wg.Wait()
		if closeErr != nil && movErr != nil {
			t.Fatalf("CM22 both failed close=%v mov=%v", closeErr, movErr)
		}
		if closeErr == nil && movErr == nil {
			// Serialized: if movement won first, close expected includes it — check consistency.
			sum, _ := s.Get(open.Session.ID)
			if sum.Session.Status != SessionClosed {
				t.Fatal("CM22 expected closed")
			}
			if sum.CashMovementIn == 500 && *sum.Session.ExpectedCashAmount != 1500 {
				t.Fatalf("CM22 movement before close must be in snapshot %+v", sum)
			}
			if sum.CashMovementIn == 0 && *sum.Session.ExpectedCashAmount != 1000 {
				t.Fatalf("CM22 close before movement %+v", sum)
			}
			return
		}
		if closeErr == nil && cashErrCode(movErr) != "CONFLICT" {
			t.Fatalf("CM22 mov after close want conflict got %v", movErr)
		}
	})

	t.Run("CM24_CM26_out_balance", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 48, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM24", Name: "C"}, 48)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 5000, IdempotencyKey: "cm24-o"}, 48)
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementOut, Type: MovementManualOut, Amount: 5000, Reason: "Sortie exacte", IdempotencyKey: "cm24-ok",
		}, 48); e != nil {
			t.Fatal(e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementOut, Type: MovementManualOut, Amount: 1, Reason: "Trop", IdempotencyKey: "cm25",
		}, 48); cashErrCode(e) != "CONFLICT" {
			t.Fatalf("CM25 %v", e)
		}
		if movementCount(t, db) != 1 {
			t.Fatal("CM26 side effect")
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.ExpectedCash != 0 {
			t.Fatalf("CM24 expected 0 got %d", sum.ExpectedCash)
		}
	})

	t.Run("CM29_CM32_collections_exclude_movements", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 49, Name: "Dir"})
		db.Create(&cashPatient{ID: 49, Nom: "P", Prenoms: "C", CodePatient: "P-CM29"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM29", Name: "C"}, 49)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 0, IdempotencyKey: "cm29-o"}, 49)
		inv := seedCashInvoice(t, db, "INV-CM29", 49, 4000, 49)
		if _, e := s.Pay(open.Session.ID, PaymentRequest{
			InvoiceID: inv.ID, Amount: 4000, PaymentMethod: "CARD", IdempotencyKey: "cm29-pay", Payer: &PayerRequest{Mode: "PATIENT"}}, 49); e != nil {
			t.Fatal(e)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 9000, Reason: "Apport", IdempotencyKey: "cm29-m",
		}, 49); e != nil {
			t.Fatal(e)
		}
		sum, _ := s.Get(open.Session.ID)
		if sum.CashCollected != 0 || sum.NonCashCollected != 4000 || sum.TotalCollected != 4000 {
			t.Fatalf("CM29/30/31 %+v", sum)
		}
		if sum.OperationCount != 1 {
			t.Fatalf("CM32 ops=%d", sum.OperationCount)
		}
		if sum.ExpectedCash != 9000 {
			t.Fatalf("expected float+in only (no card) got %d", sum.ExpectedCash)
		}
	})

	t.Run("CM35_other_session_excluded", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 50, Name: "Dir"})
		s := NewService(db)
		r1, _ := s.SaveRegister(0, RegisterRequest{Code: "CM35A", Name: "A"}, 50)
		r2, _ := s.SaveRegister(0, RegisterRequest{Code: "CM35B", Name: "B"}, 50)
		a, _ := s.Open(OpenRequest{CashRegisterID: r1.ID, OpeningFloat: 1000, IdempotencyKey: "cm35-a"}, 50)
		b, _ := s.Open(OpenRequest{CashRegisterID: r2.ID, OpeningFloat: 1000, IdempotencyKey: "cm35-b"}, 50)
		if _, e := s.CreateMovement(a.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 700, Reason: "A only", IdempotencyKey: "cm35-m",
		}, 50); e != nil {
			t.Fatal(e)
		}
		sumB, _ := s.Get(b.Session.ID)
		if sumB.CashMovementIn != 0 || sumB.ExpectedCash != 1000 {
			t.Fatalf("CM35 %+v", sumB)
		}
	})

	t.Run("CM36_CM38_closed_snapshot", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 51, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM36", Name: "C"}, 51)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 10000, IdempotencyKey: "cm36-o"}, 51)
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementOut, Type: MovementManualOut, Amount: 2500, Reason: "Retrait", IdempotencyKey: "cm36-m",
		}, 51); e != nil {
			t.Fatal(e)
		}
		closed, e := s.Close(open.Session.ID, CloseRequest{CountedCashAmount: 7500, IdempotencyKey: "cm36-c"}, 51, false)
		if e != nil {
			t.Fatal(e)
		}
		snap := *closed.Session.ExpectedCashAmount
		again, _ := s.Get(open.Session.ID)
		if again.ExpectedCash != snap || again.ExpectedCash != 7500 {
			t.Fatalf("CM36/38 %+v", again)
		}
	})

	t.Run("CM39_CM45_no_side_domains", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 52, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM39", Name: "C"}, 52)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 3000, IdempotencyKey: "cm39-o"}, 52)
		beforePay := int64(0)
		db.Model(&billing.Payment{}).Count(&beforePay)
		beforeRev := int64(0)
		db.Model(&billing.PaymentReversal{}).Count(&beforeRev)
		beforeCN := int64(0)
		db.Model(&billing.CreditNote{}).Count(&beforeCN)
		beforeRefund := int64(0)
		if db.Migrator().HasTable("billing_refunds") {
			db.Model(&billing.Refund{}).Count(&beforeRefund)
		}
		if _, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 100, Reason: "Sans effets", IdempotencyKey: "cm39",
		}, 52); e != nil {
			t.Fatal(e)
		}
		var afterPay, afterRev, afterCN, afterRefund int64
		db.Model(&billing.Payment{}).Count(&afterPay)
		db.Model(&billing.PaymentReversal{}).Count(&afterRev)
		db.Model(&billing.CreditNote{}).Count(&afterCN)
		if afterPay != beforePay || afterRev != beforeRev || afterCN != beforeCN {
			t.Fatal("CM39-42 side domains mutated")
		}
		// LOT29F-I-A: refund aggregate may exist, but cash movements must not create refund rows.
		if db.Migrator().HasTable("billing_refunds") {
			db.Model(&billing.Refund{}).Count(&afterRefund)
			if afterRefund != beforeRefund {
				t.Fatal("CM41 cash movement must not create billing refunds")
			}
		}
	})

	t.Run("CM46_CM50_immutable_audit", func(t *testing.T) {
		db := cashDB(t)
		db.Create(&cashUser{ID: 53, Name: "Dir"})
		s := NewService(db)
		reg, _ := s.SaveRegister(0, RegisterRequest{Code: "CM46", Name: "C"}, 53)
		open, _ := s.Open(OpenRequest{CashRegisterID: reg.ID, OpeningFloat: 4000, IdempotencyKey: "cm46-o"}, 53)
		m, e := s.CreateMovement(open.Session.ID, MovementRequest{
			Direction: MovementIn, Type: MovementManualIn, Amount: 200, Reason: "Audit", IdempotencyKey: "cm46",
		}, 53)
		if e != nil {
			t.Fatal(e)
		}
		var audit CashMovementAudit
		if e := db.Where("movement_id=?", m.ID).First(&audit).Error; e != nil || audit.EventType != MovementAuditCreated {
			t.Fatalf("CM49 %+v %v", audit, e)
		}
		// No update/delete routes — prove service has no mutators by absence of Save path.
		e = db.Transaction(func(tx *gorm.DB) error {
			if e := tx.Create(&CashMovement{
				CashSessionID: open.Session.ID, Direction: MovementIn, Type: MovementManualIn,
				Amount: 50, Reason: "orphan", CreatedBy: 53, OccurredAt: open.Session.OpenedAt, IdempotencyKey: "cm50-orphan",
			}).Error; e != nil {
				return e
			}
			return errors.New("forced-audit-rollback")
		})
		if e == nil {
			t.Fatal("CM50 expected error")
		}
		var orphan int64
		db.Model(&CashMovement{}).Where("idempotency_key=?", "cm50-orphan").Count(&orphan)
		if orphan != 0 {
			t.Fatal("CM50 leaked")
		}
	})

	t.Run("CM14_rbac_roles", func(t *testing.T) {
		cai := rbac.EffectiveStaffPermissions("staff", []string{"CAISSIER"}, nil)
		if rbac.HasAnyPermission(cai, "cash.movement.create") {
			t.Fatal("CM14 CAISSIER must not create")
		}
		if !rbac.HasAnyPermission(cai, "cash.movement.read") {
			t.Fatal("CAISSIER should read movements")
		}
		dir := rbac.EffectiveStaffPermissions("staff", []string{"DIRECTEUR_ADMINISTRATIF"}, nil)
		if !rbac.HasAnyPermission(dir, "cash.movement.create") || !rbac.IsSensitive("cash.movement.create") {
			t.Fatal("CM14 directeur create/sensitive")
		}
		comp := rbac.EffectiveStaffPermissions("staff", []string{"COMPTABLE"}, nil)
		if rbac.HasAnyPermission(comp, "cash.movement.create") || !rbac.HasAnyPermission(comp, "cash.movement.read") {
			t.Fatal("COMPTABLE read-only movements")
		}
	})
}
