package dashboard

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// dryRunDB génère le SQL sans jamais se connecter à PostgreSQL.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 dbname=dryrun"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

func countSQL(t *testing.T, build func(*gorm.DB, DashboardFilter) *gorm.DB, filter DashboardFilter) string {
	t.Helper()
	db := dryRunDB(t)
	return db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var n int64
		return build(tx, filter).Count(&n)
	})
}

func assertContains(t *testing.T, sql string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(sql, part) {
			t.Fatalf("SQL sans %q :\n%s", part, sql)
		}
	}
}

func fullFilter() DashboardFilter {
	serviceID := uint(2)
	return DashboardFilter{ServiceID: &serviceID, From: "2026-09-25", To: "2026-10-25"}
}

func TestActivityQueriesApplyServiceAndHalfOpenCivilPeriod(t *testing.T) {
	cases := []struct {
		name       string
		build      func(*gorm.DB, DashboardFilter) *gorm.DB
		dateColumn string
		service    string
	}{
		{"consultations", consultationsQuery, "created_at", "service_id"},
		{"hospitalizations", hospitalizationsQuery, "admitted_at", "service_id"},
		{"pharmacyPurchases", pharmacyPurchasesQuery, "d.created_at", "c.service_id"},
		{"patientsSeen", patientsSeenQuery, "created_at", "service_id"},
		{"queueTickets", queueTicketsQuery, "arrived_at", "service_id"},
		{"insuranceVouchers", insuranceVouchersQuery, "iv.created_at", "c.service_id"},
		{"billedAmount", billedAmountQuery, "i.issued_at", invoiceLineServiceColumn},
		{"insuranceReceivables", insuranceReceivablesQuery, "i.created_at", invoiceLineServiceColumn},
		{"labExams", labExamsQuery, "pa.performed_at", "COALESCE(c.service_id, h.service_id)"},
		{"imagingExams", imagingExamsQuery, "pa.performed_at", "COALESCE(c.service_id, h.service_id)"},
		{"otherActs", otherActsQuery, "pa.performed_at", "COALESCE(c.service_id, h.service_id)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql := countSQL(t, tc.build, fullFilter())
			assertContains(t, sql,
				tc.service+" = 2",
				tc.dateColumn+" >= '2026-09-25'::date",
				tc.dateColumn+" < '2026-10-25'::date",
			)
		})
	}
}

func TestActivityQueriesWithoutFilterHaveNoPeriodOrService(t *testing.T) {
	for _, build := range []func(*gorm.DB, DashboardFilter) *gorm.DB{
		consultationsQuery, hospitalizationsQuery, pharmacyPurchasesQuery, patientsSeenQuery, queueTicketsQuery,
		insuranceVouchersQuery, billedAmountQuery, patientReceivablesQuery, insuranceReceivablesQuery,
		refundsQuery, labExamsQuery, imagingExamsQuery, otherActsQuery,
	} {
		sql := countSQL(t, build, DashboardFilter{})
		if strings.Contains(sql, "::date") || strings.Contains(sql, "service_id =") {
			t.Fatalf("filtre inattendu sans paramètres :\n%s", sql)
		}
	}
}

func TestBusinessDefinitions(t *testing.T) {
	assertContains(t, countSQL(t, hospitalizationsQuery, DashboardFilter{}),
		`FROM "hospitalizations"`, "deleted_at IS NULL", "status <> 'CANCELLED'")
	assertContains(t, countSQL(t, queueTicketsQuery, DashboardFilter{}),
		`FROM "patient_queue_tickets"`, "status <> 'CANCELLED'")
	assertContains(t, countSQL(t, patientsSeenQuery, DashboardFilter{}),
		"COUNT(DISTINCT(\"patient_id\"))", `FROM "consultations"`)

	pharmacy := countSQL(t, pharmacyPurchasesQuery, DashboardFilter{})
	assertContains(t, pharmacy,
		"COUNT(DISTINCT(\"vl\".\"voucher_id\"))",
		"JOIN pharmacy_voucher_lines AS vl ON vl.prescription_id = d.reference_id",
		"JOIN pharmacy_vouchers AS pv ON pv.id = vl.voucher_id",
		"d.reference_type = 'CONSULTATION_PRESCRIPTION'",
		"d.status = 'COMPLETED'",
		"pv.cancelled_at IS NULL",
	)
}

func TestBilledAmountDefinition(t *testing.T) {
	sql := dryRunDB(t).ToSQL(func(tx *gorm.DB) *gorm.DB {
		var amount int64
		return billedAmountQuery(tx, fullFilter()).Scan(&amount)
	})
	assertContains(t, sql,
		"COALESCE(SUM(l.gross_amount), 0)",
		"l.is_active",
		"i.status NOT IN ('CANCELLED','DRAFT')",
		"l.act_type = 'PERFORMED_ACT'",
		"l.act_type = 'MEDICATION'",
		"l.act_type = 'HOSPITALIZATION'",
		invoiceLineServiceColumn+" = 2",
	)
}

func TestReceivablesRefundsAndActsDefinitions(t *testing.T) {
	patient := countSQL(t, patientReceivablesQuery, fullFilter())
	assertContains(t, patient,
		"i.status IN ('ISSUED','PARTIALLY_PAID')",
		"NOT i.coverage_pending",
		patientBalanceExpr+" > 0",
		"EXISTS (SELECT 1 FROM billing_invoice_lines AS l",
		invoiceLineServiceColumn+" = 2",
		"i.created_at >= '2026-09-25'::date",
		"i.created_at < '2026-10-25'::date",
	)

	assertContains(t, countSQL(t, insuranceReceivablesQuery, DashboardFilter{}),
		"COUNT(DISTINCT(\"l\".\"invoice_id\"))",
		"l.coverage_resolution IN ('DIRECT','COVERED')",
		"a.status IN ('APPROVED','PARTIALLY_APPROVED')",
		"s.status = 'POSTED'",
		"l.insurance_amount - COALESCE(sa.paid, 0) > 0",
	)

	// Un remboursement n'a pas de service : seul le filtre de période s'applique.
	refunds := countSQL(t, refundsQuery, fullFilter())
	assertContains(t, refunds,
		"COUNT(DISTINCT(\"r\".\"id\"))",
		"r.status = 'EXECUTED'",
		"e.executed_at >= '2026-09-25'::date",
		"e.executed_at < '2026-10-25'::date",
	)
	if strings.Contains(refunds, "service_id") {
		t.Fatalf("remboursements filtrés par service :\n%s", refunds)
	}

	assertContains(t, countSQL(t, labExamsQuery, DashboardFilter{}),
		"pa.status = 'PERFORMED' AND pa.voided_at IS NULL", "pa.act_category IN ('LABORATORY')")
	assertContains(t, countSQL(t, imagingExamsQuery, DashboardFilter{}), "pa.act_category IN ('IMAGING')")
	assertContains(t, countSQL(t, otherActsQuery, DashboardFilter{}), "pa.act_category IN ('PROCEDURE','OTHER')")
}
