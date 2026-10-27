package grc

import (
	"testing"
	"time"
)

// TestPostgresGRC02Dashboard runs the dashboard aggregates on PostgreSQL with
// enforced foreign keys and CHECK constraints: GROUP BY over enum columns,
// the correlated LIMIT 1 subquery for the latest assessment, EXISTS clauses
// and timestamptz boundaries. The dataset is written at +02:00 while the
// service clock is UTC, so every comparison is between instants.
func TestPostgresGRC02Dashboard(t *testing.T) {
	db := grcPostgres(t)
	if err := db.AutoMigrate(dashboardModels()...); err != nil {
		t.Fatal(err)
	}
	withCaptureBus(t)
	svc := NewService(db)
	svc.now = func() time.Time { return pgNow }

	empty, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Summary != (DashboardSummary{}) || empty.Compliance.ControlMappingCoverage != (DashboardRatio{}) {
		t.Fatalf("empty schema must give zero figures: %+v", empty.Summary)
	}

	seedDashboardDataset(t, db, pgNowParis)
	d, err := svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	requireDashboardDataset(t, d, pgNow)

	// One second later, the items due exactly at the seed instant are late.
	svc.now = func() time.Time { return pgNow.Add(time.Second) }
	d, err = svc.Dashboard(defaultDashboardFilter())
	if err != nil {
		t.Fatal(err)
	}
	requireValue(t, "evidence.expired", d.Evidence.Expired, 2)
	requireValue(t, "evidence.expiringSoon", d.Evidence.ExpiringSoon, 3)
	requireValue(t, "actions.overdue", d.Actions.Overdue, 2)
	requireValue(t, "actions.dueSoon", d.Actions.DueSoon, 1)
	requireValue(t, "findings.overdue", d.Findings.Overdue, 3)
	requireValue(t, "assessments.reassessmentDue", d.Assessments.ReassessmentDue, 2)
}
