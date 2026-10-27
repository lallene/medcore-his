package grc

import (
	"testing"
	"time"
)

// TestPostgresGRC12Reporting runs the read-only aggregates on PostgreSQL.
// The schema is the ephemeral grc_* schema created by grcPostgres. No report
// table is migrated.
func TestPostgresGRC12Reporting(t *testing.T) {
	db := grcPostgres(t)
	if err := db.AutoMigrate(append(dashboardModels(), &GovernancePolicy{})...); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db)
	svc.now = func() time.Time { return pgNow }

	empty, err := svc.Reporting(defaultReportingFilter())
	if err != nil {
		t.Fatal(err)
	}
	if empty.Executive != (ReportingExecutive{}) || empty.Compliance.MappingCoverage != (DashboardRatio{}) || empty.Compliance.ByFramework == nil {
		t.Fatalf("empty schema must give zero figures: %+v", empty.Executive)
	}

	seed := seedReportingDataset(t, db, pgNow)
	got, err := svc.Reporting(defaultReportingFilter())
	if err != nil {
		t.Fatal(err)
	}
	if !got.GeneratedAt.Equal(pgNow) {
		t.Fatalf("generatedAt %s", got.GeneratedAt)
	}
	requireReportingDataset(t, got, 30)

	narrow := defaultReportingFilter()
	narrow.EvidenceExpiringSoonDays = 1
	narrow.ActionDueSoonDays = 1
	got, err = svc.Reporting(narrow)
	if err != nil {
		t.Fatal(err)
	}
	requireReportingDataset(t, got, 1)

	filtered := defaultReportingFilter()
	filtered.FrameworkIDs = []uint{seed.frameworkA}
	got, err = svc.Reporting(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if got.Executive.OpenRisks != 3 || len(got.Compliance.ByFramework) != 1 || got.Compliance.ByFramework[0].Code != "FW-A" {
		t.Fatalf("framework filter %+v risks %d", got.Compliance, got.Executive.OpenRisks)
	}

	unknown := defaultReportingFilter()
	unknown.FrameworkIDs = []uint{999999}
	got, err = svc.Reporting(unknown)
	if err != nil {
		t.Fatal(err)
	}
	if got.Compliance.MappedRequirements != 0 || got.Executive.OpenFindings != 3 || got.Policies.Published != 1 {
		t.Fatalf("unknown framework changed non-compliance figures: %+v", got.Executive)
	}
}
