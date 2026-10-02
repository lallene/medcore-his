package authorization

import (
	"math"
	"testing"
)

func TestDecideRequiresRequestedAmount(t *testing.T) {
	db := authorizationDB(t)
	if err := db.AutoMigrate(&authorizationPerformedAct{}); err != nil {
		t.Fatal(err)
	}
	fx := seedAuthorization(t, db)
	s := NewService(db)
	act := newPerformedAct(fx.patient.ID, "SPLIT", true, "PERFORMED")
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	created, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "X", ExternalDecisionDate: "2026-10-01", ApprovedRate: f(80),
	}, 3, UnrestrictedAccess(3)); err == nil {
		t.Fatal("missing RequestedAmount must fail Decide for PERFORMED_ACT")
	}

	legacy, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: fx.act.ID,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(legacy.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(legacy.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "Y", ExternalDecisionDate: "2026-10-01", ApprovedRate: f(80),
	}, 3, UnrestrictedAccess(3)); err == nil {
		t.Fatal("missing RequestedAmount must fail Decide for CONSULTATION")
	}
}

func TestDecidePatientAmountEcho(t *testing.T) {
	db := authorizationDB(t)
	fx := seedAuthorization(t, db)
	s := NewService(db)
	amount := 10000.0
	created, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: fx.act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	rate := 80.0
	okEcho := 2000.0
	decided, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "ECHO-OK", ExternalDecisionDate: "2026-10-01",
		ApprovedRate: &rate, PatientAmount: &okEcho,
	}, 3, UnrestrictedAccess(3))
	if err != nil {
		t.Fatal(err)
	}
	if *decided.InsuranceAmount != 8000 || *decided.PatientAmount != 2000 {
		t.Fatalf("echo match: %#v", decided)
	}

	act2 := authorizationConsultation{PatientID: fx.patient.ID, Service: "Echo"}
	db.Create(&act2)
	created2, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: act2.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created2.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	bad := 2500.0
	if _, err := s.Decide(created2.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "ECHO-BAD", ExternalDecisionDate: "2026-10-01",
		ApprovedRate: &rate, PatientAmount: &bad,
	}, 3, UnrestrictedAccess(3)); !IsConflict(err) {
		t.Fatalf("mismatch echo must conflict: %v", err)
	}

	act3 := authorizationConsultation{PatientID: fx.patient.ID, Service: "EchoTol"}
	db.Create(&act3)
	created3, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: act3.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created3.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	boundary := 2000.01
	if _, err := s.Decide(created3.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "ECHO-TOL", ExternalDecisionDate: "2026-10-01",
		ApprovedRate: &rate, PatientAmount: &boundary,
	}, 3, UnrestrictedAccess(3)); err != nil {
		t.Fatalf("tolerance 0.01 must accept: %v", err)
	}
}

func TestDecideFullPartialRejectAndImmutability(t *testing.T) {
	db := authorizationDB(t)
	fx := seedAuthorization(t, db)
	s := NewService(db)
	amount := 10000.0
	rate := 80.0

	mk := func(ref uint) uint {
		c, err := s.Create(CreateRequest{
			PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
			ReferenceType: "CONSULTATION", ReferenceID: ref, RequestedAmount: &amount,
		}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Submit(c.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.MarkPending(c.ID, 2, UnrestrictedAccess(2)); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}

	approved, err := s.Decide(mk(fx.act.ID), DecisionRequest{
		Status: StatusApproved, ExternalReference: "FULL", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
	}, 3, UnrestrictedAccess(3))
	if err != nil || *approved.InsuranceAmount != 8000 || *approved.PatientAmount != 2000 {
		t.Fatalf("full: %#v err=%v", approved, err)
	}

	actP := authorizationConsultation{PatientID: fx.patient.ID, Service: "Partial"}
	db.Create(&actP)
	ceiling := 5000.0
	partial, err := s.Decide(mk(actP.ID), DecisionRequest{
		Status: StatusPartiallyApproved, ExternalReference: "PART", ExternalDecisionDate: "2026-10-01",
		ApprovedRate: &rate, CeilingAmount: &ceiling,
	}, 3, UnrestrictedAccess(3))
	if err != nil || *partial.InsuranceAmount != 5000 || *partial.PatientAmount != 5000 {
		t.Fatalf("partial: %#v err=%v", partial, err)
	}

	actR := authorizationConsultation{PatientID: fx.patient.ID, Service: "Reject"}
	db.Create(&actR)
	staleRate, staleFixed, staleCeil := 90.0, 7000.0, 6000.0
	rejected, err := s.Decide(mk(actR.ID), DecisionRequest{
		Status: StatusRejected, ExternalReference: "REJ", ExternalDecisionDate: "2026-10-01",
		ApprovedRate: &staleRate, ApprovedAmount: &staleFixed, CeilingAmount: &staleCeil,
		RejectionReason: "refus assureur",
	}, 3, UnrestrictedAccess(3))
	if err != nil {
		t.Fatal(err)
	}
	if *rejected.InsuranceAmount != 0 || *rejected.PatientAmount != 10000 {
		t.Fatalf("reject amounts: %#v", rejected)
	}
	if rejected.ApprovedRate != nil || rejected.ApprovedAmount != nil || rejected.CeilingAmount != nil {
		t.Fatalf("REJECTED must clear approval terms: %#v", rejected)
	}
	if rejected.RejectionReason == "" {
		t.Fatal("rejection reason required/persisted")
	}

	if _, err := s.Decide(rejected.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "AGAIN", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
	}, 4, UnrestrictedAccess(4)); !IsConflict(err) {
		t.Fatalf("final immutability: %v", err)
	}
}

func TestDecidePerformedActSplitWithoutCatalog(t *testing.T) {
	db, f := preparePerformedActDB(t)
	act := newPerformedAct(f.patient.ID, "FIN", true, "PERFORMED")
	act.BasePrice = 999999
	act.Quantity = 7
	if err := db.Create(&act).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	amount := 12345.67
	created, err := s.Create(CreateRequest{
		PatientID: f.patient.ID, PatientCoverageID: f.coverage.ID,
		ReferenceType: ReferencePerformedAct, ReferenceID: act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkPending(created.ID, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	rate := 50.0
	decided, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "PA-FIN", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
	}, 3, UnrestrictedAccess(3))
	if err != nil {
		t.Fatal(err)
	}
	ins, pat, _ := Calculate(amount, StatusApproved, &rate, nil, nil)
	if *decided.InsuranceAmount != ins || *decided.PatientAmount != pat {
		t.Fatalf("performed_act split=%v/%v want %v/%v (BasePrice must not apply)", *decided.InsuranceAmount, *decided.PatientAmount, ins, pat)
	}
	if math.Abs(ins+pat-math.Round(amount*100)/100) > 1e-9 {
		t.Fatalf("invariant: %v+%v", ins, pat)
	}
	if decided.ReferenceType != ReferencePerformedAct {
		t.Fatalf("reference drifted: %#v", decided)
	}
}

func TestDecideDoesNotMutateFinancialSideTables(t *testing.T) {
	db := authorizationDB(t)
	tables := []string{
		"billing_invoices", "billing_invoice_lines", "billing_authorization_allocations",
		"billing_payments", "insurance_receivable_followups", "insurance_receivable_metadata", "cash_receipts",
	}
	for _, table := range tables {
		if err := db.Exec("CREATE TABLE IF NOT EXISTS " + table + " (id bigserial primary key)").Error; err != nil {
			t.Fatal(err)
		}
	}
	fx := seedAuthorization(t, db)
	s := NewService(db)
	amount := 5000.0
	created, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: fx.act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	rate := 70.0
	if _, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusApproved, ExternalReference: "FW", ExternalDecisionDate: "2026-10-01", ApprovedRate: &rate,
	}, 3, UnrestrictedAccess(3)); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		var n int64
		if err := db.Table(table).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("%s mutated n=%d err=%v", table, n, err)
		}
	}
}

func TestDecideRejectRequiresReason(t *testing.T) {
	db := authorizationDB(t)
	fx := seedAuthorization(t, db)
	s := NewService(db)
	amount := 1000.0
	created, err := s.Create(CreateRequest{
		PatientID: fx.patient.ID, PatientCoverageID: fx.coverage.ID,
		ReferenceType: "CONSULTATION", ReferenceID: fx.act.ID, RequestedAmount: &amount,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(created.ID, SubmitRequest{}, 2, UnrestrictedAccess(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(created.ID, DecisionRequest{
		Status: StatusRejected, ExternalReference: "R", ExternalDecisionDate: "2026-10-01",
	}, 3, UnrestrictedAccess(3)); err == nil {
		t.Fatal("missing rejection reason accepted")
	}
}
