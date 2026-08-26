package qa

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

func qaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, e := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AutoMigrate(&Campaign{}, &TestResult{}, &Artifact{}); e != nil {
		t.Fatal(e)
	}
	return db
}
func TestImportSummaryIsIdempotent(t *testing.T) {
	db := qaDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	x := &Summary{RunID: "QA-1", Environment: "ci", StartedAt: now.Format(time.RFC3339), FinishedAt: now.Add(time.Second).Format(time.RFC3339), Duration: 1000, Total: 1, Passed: 1, Status: StatusPassed, Tests: []SummaryTest{{Key: "QA-SMOKE-001", Suite: "smoke", Title: "Smoke", Status: "PASSED"}}}
	if e := ImportSummary(db, x); e != nil {
		t.Fatal(e)
	}
	if e := ImportSummary(db, x); e != nil {
		t.Fatal(e)
	}
	var campaigns, results int64
	db.Model(&Campaign{}).Count(&campaigns)
	db.Model(&TestResult{}).Count(&results)
	if campaigns != 1 || results != 1 {
		t.Fatalf("campaigns=%d results=%d", campaigns, results)
	}
}
func TestImportSummaryRejectsUnexecutedTestsMarkedPassed(t *testing.T) {
	db := qaDB(t)
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	x := &Summary{RunID: "QA-INVALID", Environment: "ci", StartedAt: now, Total: 1, NotImplemented: 1, Status: StatusPassed, Tests: []SummaryTest{{Key: "QA-PLANNED", Suite: "full", Title: "Planned", Status: ResultNotImplemented}}}
	if e := ImportSummary(db, x); e == nil {
		t.Fatal("incoherent PASS must be rejected")
	}
	var campaigns int64
	db.Model(&Campaign{}).Count(&campaigns)
	if campaigns != 0 {
		t.Fatal("invalid campaign was persisted")
	}
}
func TestCampaignReadModels(t *testing.T) {
	db := qaDB(t)
	now := time.Now()
	db.Create(&Campaign{RunID: "QA-2", Environment: "ci", StartedAt: now, Status: StatusFailed, Total: 2, Passed: 1, Failed: 1})
	s := NewService(db)
	k, e := s.KPIs()
	if e != nil || k.Failed != 1 {
		t.Fatalf("kpis=%#v err=%v", k, e)
	}
	p, e := s.List(Filter{Environment: "ci"})
	if e != nil || p.Meta.Total != 1 {
		t.Fatalf("page=%#v err=%v", p, e)
	}
}
