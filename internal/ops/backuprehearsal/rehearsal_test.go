package backuprehearsal

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/grc"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGRC15B03BackupRestoreRehearsal(t *testing.T) {
	if os.Getenv("MEDCORE_BACKUP_REHEARSAL") != "1" {
		t.Skip("MEDCORE_BACKUP_REHEARSAL=1 runs the isolated backup/restore rehearsal")
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	source := "medcore_b03_source_" + suffix
	restore := "medcore_restore_test_" + suffix
	corrupt := "medcore_restore_corrupt_" + suffix
	if err := CheckNames(source, restore, corrupt); err != nil {
		t.Fatal(err)
	}
	t.Logf("source=%s restore=%s corrupt=%s", source, restore, corrupt)

	admin := openSQL(t, "postgres")
	defer admin.Close()
	var adminDB, adminUser, version string
	if err := admin.QueryRow(`SELECT current_database(), current_user, current_setting('server_version')`).Scan(&adminDB, &adminUser, &version); err != nil {
		t.Fatal(err)
	}
	if adminDB != "postgres" {
		t.Fatalf("admin connection is %s, not the local maintenance database", adminDB)
	}
	if adminDB == ForbiddenMedcore || adminDB == ForbiddenMedcoreHIS {
		t.Fatal("refusing to run from a forbidden database")
	}
	var sourcePublic, hisExists int
	mustQueryOne(t, admin, `SELECT count(*) FROM pg_database WHERE datname = 'medcore_his'`, &hisExists)
	mustQueryOne(t, admin, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`, &sourcePublic)
	// information_schema on the admin connection is the postgres database, not medcore_test.
	var testPublic int
	testDB := openSQL(t, "medcore_test")
	defer testDB.Close()
	var testName string
	if err := testDB.QueryRow(`SELECT current_database()`).Scan(&testName); err != nil {
		t.Fatal(err)
	}
	if testName != "medcore_test" {
		t.Fatalf("expected medcore_test, got %s", testName)
	}
	mustQueryOne(t, testDB, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`, &testPublic)
	t.Logf("server=%s user=%s medcore_his_present=%d medcore_test_public_tables=%d", version, adminUser, hisExists, testPublic)

	assertMissingDumpRefused(t)

	mustExec(t, admin, `CREATE DATABASE `+quoteIdent(source))
	mustExec(t, admin, `CREATE DATABASE `+quoteIdent(restore))
	mustExec(t, admin, `CREATE DATABASE `+quoteIdent(corrupt))
	t.Cleanup(func() {
		dropRehearsalDB(source)
		dropRehearsalDB(restore)
		dropRehearsalDB(corrupt)
	})

	var restoreTables int
	restoreSQL := openSQL(t, restore)
	mustQueryOne(t, restoreSQL, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`, &restoreTables)
	restoreSQL.Close()
	if restoreTables != 0 {
		t.Fatalf("restore database was not empty: %d tables", restoreTables)
	}

	backend := backendRoot(t)
	migrateBin := filepath.Join(t.TempDir(), "medcore-migrate")
	run(t, backend, "go", "build", "-o", migrateBin, "./cmd/migrate")

	runMigrate(t, migrateBin, source)
	seedSource(t, source)

	before := readManifest(t, source)
	if before.rows["audit_logs"] < 1 || before.rows["grc_reminder_intents"] < 1 || before.rows["grc_assets"] < 1 {
		t.Fatalf("source fixture incomplete: %+v", before.rows)
	}
	for _, required := range []string{
		"grc_reminder_one_target",
		"grc_reminder_kind_target",
		"uidx_grc_reminder_recipient_occurrence",
		"idx_grc_reminder_claim",
		"idx_grc_reminder_lease",
	} {
		if !strings.Contains(before.constraints+"\n"+before.indexes, required) {
			t.Fatalf("source manifest missing %s", required)
		}
	}

	dumpPath := filepath.Join(t.TempDir(), "medcore_test_rehearsal.dump")
	dumpStart := time.Now()
	run(t, backend, "pg_dump", "--format=custom", "--no-owner", "--no-privileges", "--file="+dumpPath, localDSN(source))
	dumpDuration := time.Since(dumpStart)
	info, err := os.Stat(dumpPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("dump missing or empty: %v", err)
	}
	sum := sha256File(t, dumpPath)
	list := runOutput(t, backend, "pg_restore", "--list", dumpPath)
	for _, token := range []string{"TABLE DATA", "CONSTRAINT", "INDEX", "SEQUENCE", "audit_logs", "grc_reminder_intents", "grc_assets", "grc_governance_policies"} {
		if !strings.Contains(list, token) {
			t.Fatalf("dump list missing %s", token)
		}
	}
	if sha256File(t, dumpPath) != sum {
		t.Fatal("checksum changed before restore")
	}

	restoreStart := time.Now()
	run(t, backend, "pg_restore", "--no-owner", "--no-privileges", "--exit-on-error", "--single-transaction", "--dbname="+localDSN(restore), dumpPath)
	restoreDuration := time.Since(restoreStart)

	after := readManifest(t, restore)
	if before.schemas != after.schemas || before.tables != after.tables || before.grcTables != after.grcTables ||
		canonicalBlock(before.constraints) != canonicalBlock(after.constraints) || before.indexes != after.indexes || before.sequences != after.sequences {
		t.Fatalf("manifest mismatch schemas %d/%d tables %d/%d grc %v constraints %v indexes %v sequences %v\n%s",
			before.schemas, after.schemas, before.tables, after.tables, before.grcTables == after.grcTables,
			canonicalBlock(before.constraints) == canonicalBlock(after.constraints), before.indexes == after.indexes, before.sequences == after.sequences,
			diffText(canonicalBlock(before.constraints), canonicalBlock(after.constraints)))
	}
	for table, count := range before.rows {
		if after.rows[table] != count {
			t.Fatalf("row count %s source=%d restored=%d", table, count, after.rows[table])
		}
	}

	smokeStart := time.Now()
	smokeRestored(t, restore, before.rows["audit_logs"], before.rows["grc_assets"])
	smokeDuration := time.Since(smokeStart)

	runMigrate(t, migrateBin, restore)
	smokeAfterMigrate(t, restore)

	corruptPath := filepath.Join(t.TempDir(), "corrupt.dump")
	if err = os.WriteFile(corruptPath, []byte("not a postgres dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = exec.Command("pg_restore", "--no-owner", "--no-privileges", "--exit-on-error", "--dbname="+localDSN(corrupt), corruptPath).Run(); err == nil {
		t.Fatal("corrupt dump was restored successfully")
	}
	corruptSQL := openSQL(t, corrupt)
	var corruptTables int
	mustQueryOne(t, corruptSQL, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`, &corruptTables)
	corruptSQL.Close()
	if corruptTables != 0 {
		t.Fatalf("corrupt restore left %d tables", corruptTables)
	}

	t.Logf("SUMMARY version=%s source=%s restore=%s schemas=%d tables=%d grc_tables=%d constraints=%d indexes=%d audit_logs=%d reminder_intents=%d dump_bytes=%d sha256=%s dump=%s restore=%s smoke=%s",
		version, source, restore, before.schemas, before.tables, strings.Count(before.grcTables, "\n")+1, strings.Count(before.constraints, "\n")+1, strings.Count(before.indexes, "\n")+1,
		before.rows["audit_logs"], before.rows["grc_reminder_intents"], info.Size(), sum,
		dumpDuration.Round(time.Millisecond), restoreDuration.Round(time.Millisecond), smokeDuration.Round(time.Millisecond))
	t.Logf("ROWS %s", formatRows(before.rows))
	t.Logf("GRC_TABLES %s", strings.ReplaceAll(strings.TrimSpace(before.grcTables), "\n", ","))
}

func assertMissingDumpRefused(t *testing.T) {
	t.Helper()
	const missing = "medcore_b03_source_0000000001"
	admin := openSQL(t, "postgres")
	defer admin.Close()
	var n int
	mustQueryOne(t, admin, `SELECT count(*) FROM pg_database WHERE datname = '`+missing+`'`, &n)
	if n != 0 {
		t.Fatal("missing-db probe name already exists; not dropping an unknown database")
	}
	cmd := exec.Command("pg_dump", "--format=custom", "--file="+filepath.Join(t.TempDir(), "missing.dump"), localDSN(missing))
	if err := cmd.Run(); err == nil {
		t.Fatal("pg_dump of a missing database succeeded")
	}
}

func seedSource(t *testing.T, name string) {
	t.Helper()
	db := openGorm(t, name)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var current string
	if err = db.Raw(`SELECT current_database()`).Scan(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current != name || current == ForbiddenMedcore || current == ForbiddenMedcoreHIS {
		t.Fatalf("refusing to seed %s", current)
	}

	user := auth.User{Name: "B03", Email: "b03-rehearsal@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	department := organization.Department{Code: "B03", Name: "Rehearsal", Active: true, CreatedBy: user.ID, UpdatedBy: user.ID}
	if err = db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	service := organization.Service{
		DepartmentID: department.ID, Code: "B03", Name: "Rehearsal",
		ServiceType: organization.TypeAdministrative, Active: true, CreatedBy: user.ID, UpdatedBy: user.ID,
	}
	if err = db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	profile := staff.Profile{UserID: user.ID, EmployeeCode: "B03", Active: true}
	if err = db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}

	svc := grc.NewService(db)
	actor := user.ID
	asset, err := svc.SaveAsset(0, actor, grc.AssetRequest{
		Code: "B03-ASSET", Name: "Actif rehearsal", Type: grc.AssetTypeApplication,
		Criticality: grc.AssetCriticalityLow, Status: grc.AssetStatusActive,
		OwningServiceID: &service.ID, OwnerProfileID: &profile.ID,
		Description: "SECRET-B03-DESC",
	})
	if err != nil {
		t.Fatal(err)
	}
	risk, err := svc.SaveRisk(0, actor, grc.RiskRequest{
		Reference: "B03-RISK", Title: "Risque rehearsal", AssetID: &asset.ID,
		Likelihood: 2, Impact: 3, ResidualLikelihood: 1, ResidualImpact: 2,
		Treatment: grc.RiskTreatmentReduce, Status: grc.RiskStatusOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	framework, err := svc.SaveFramework(0, actor, grc.FrameworkRequest{
		Code: "B03-FW", Name: "Référentiel rehearsal", Version: "1", Status: grc.FrameworkStatusDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	requirement, err := svc.SaveRequirement(0, actor, grc.RequirementRequest{
		FrameworkID: framework.ID, Code: "B03-REQ", Title: "Exigence rehearsal", Status: grc.RequirementStatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	control, err := svc.SaveControl(0, actor, grc.ControlRequest{
		Code: "B03-CTL", Name: "Contrôle rehearsal", Status: grc.ControlStatusActive,
		OwnerProfileID: &profile.ID, OwningServiceID: &service.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.LinkRequirementControl(actor, grc.RequirementControlRequest{RequirementID: requirement.ID, ControlID: control.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.LinkControlRisk(actor, grc.ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID}); err != nil {
		t.Fatal(err)
	}
	implementation, err := svc.SaveControlImplementation(0, actor, grc.ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: grc.ControlImplementationStatusImplemented,
		OwnerProfileID: &profile.ID, OwningServiceID: &service.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := svc.SaveEvidence(0, actor, grc.EvidenceRequest{
		Reference: "B03-EVD", Title: "Preuve rehearsal", ControlImplementationID: implementation.ID,
		Type: grc.EvidenceTypeReport, Status: grc.EvidenceStatusValid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveRemediationAction(0, actor, grc.RemediationActionRequest{
		Reference: "B03-ACT", Title: "Action rehearsal", ControlImplementationID: &implementation.ID,
		Priority: grc.RemediationActionPriorityHigh, Status: grc.RemediationActionStatusOpen,
	}); err != nil {
		t.Fatal(err)
	}
	engagement, err := svc.SaveAuditEngagement(0, actor, grc.AuditEngagementRequest{
		Reference: "B03-AUD", Title: "Audit rehearsal", Type: grc.AuditEngagementTypeInternal, Status: grc.AuditEngagementStatusInProgress,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SaveFinding(0, actor, grc.FindingRequest{
		Reference: "B03-FND", Title: "Constat rehearsal", AuditEngagementID: &engagement.ID,
		Category: grc.FindingCategoryControlDeficiency, Severity: grc.FindingSeverityHigh, Status: grc.FindingStatusOpen,
	}); err != nil {
		t.Fatal(err)
	}
	assessment, err := svc.SaveAssessment(0, actor, grc.AssessmentRequest{
		Reference: "B03-ASM", Title: "Évaluation rehearsal", ControlImplementationID: implementation.ID,
		Type: grc.AssessmentTypeDesign, Status: grc.AssessmentStatusDraft,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.LinkAssessmentEvidence(actor, assessment.ID, grc.AssessmentEvidenceRequest{EvidenceID: evidence.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateSecurityIncident(actor, grc.SecurityIncidentRequest{
		Reference: "B03-INC", Title: "Incident rehearsal", Severity: grc.SecurityIncidentSeverityLow,
		Status: grc.SecurityIncidentStatusOpen, DetectedAt: time.Now().UTC(), AssetID: &asset.ID,
	}); err != nil {
		t.Fatal(err)
	}
	frameworkIDs := []uint{framework.ID}
	controlIDs := []uint{control.ID}
	policy, err := svc.CreateGovernancePolicy(actor, grc.GovernancePolicyRequest{
		Reference: "B03-POL", Title: "Politique rehearsal", Version: "1",
		FrameworkIDs: &frameworkIDs, ControlIDs: &controlIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	intent := grc.GRCReminderIntent{
		Kind: grc.ReminderKindEvidenceExpiring, Status: grc.ReminderStatusPending,
		EvidenceID: &evidence.ID, RecipientProfileID: profile.ID,
		OccurrenceKey: "evidence-expiring:b03", ScheduledFor: now,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = db.Create(&intent).Error; err != nil {
		t.Fatal(err)
	}
	var secret string
	if err = db.Raw(`SELECT coalesce(string_agg(new_value, ''), '') FROM audit_logs`).Scan(&secret).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(secret, "SECRET-B03-DESC") {
		t.Fatal("audit snapshot stored the asset description")
	}
	if policy.ID == 0 || intent.ID == 0 {
		t.Fatal("seed did not persist policy or reminder")
	}
}

func smokeRestored(t *testing.T, name string, auditCount, assetCount int64) {
	t.Helper()
	db := openGorm(t, name)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var current string
	if err = db.Raw(`SELECT current_database()`).Scan(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current != name {
		t.Fatalf("smoke connected to %s", current)
	}
	if err = db.Raw(`SELECT 1`).Error; err != nil {
		t.Fatal(err)
	}

	svc := grc.NewService(db)
	assets, err := svc.ListAssets(grc.AssetFilter{Page: 1, Limit: 10})
	if err != nil || assets.Total != assetCount {
		t.Fatalf("asset read total=%v err=%v", assets, err)
	}
	risks, err := svc.ListRisks(grc.RiskFilter{Page: 1, Limit: 10})
	if err != nil || risks.Total < 1 {
		t.Fatalf("risk read %d %v", risks.Total, err)
	}
	frameworks, err := svc.ListFrameworks(grc.FrameworkFilter{Page: 1, Limit: 10})
	if err != nil || frameworks.Total < 1 {
		t.Fatalf("framework read %v", err)
	}
	evidence, err := svc.ListEvidence(grc.EvidenceFilter{Page: 1, Limit: 10})
	if err != nil || evidence.Total < 1 {
		t.Fatalf("evidence read %v", err)
	}
	actions, err := svc.ListRemediationActions(grc.RemediationActionFilter{Page: 1, Limit: 10})
	if err != nil || actions.Total < 1 {
		t.Fatalf("action read %v", err)
	}
	audits, err := svc.ListAuditEngagements(grc.AuditEngagementFilter{Page: 1, Limit: 10})
	if err != nil || audits.Total < 1 {
		t.Fatalf("audit engagement read %v", err)
	}
	findings, err := svc.ListFindings(grc.FindingFilter{Page: 1, Limit: 10})
	if err != nil || findings.Total < 1 {
		t.Fatalf("finding read %v", err)
	}
	incidents, err := svc.ListSecurityIncidents(grc.SecurityIncidentFilter{Page: 1, Limit: 10})
	if err != nil || incidents.Total < 1 {
		t.Fatalf("incident read %v", err)
	}
	policies, err := svc.ListGovernancePolicies(grc.GovernancePolicyFilter{Page: 1, Limit: 10})
	if err != nil || policies.Total < 1 {
		t.Fatalf("policy read %v", err)
	}
	if _, err = svc.Dashboard(grc.DashboardFilter{ExpiringSoonDays: 30, DueSoonDays: 30}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Reporting(grc.ReportingFilter{EvidenceExpiringSoonDays: 30, ActionDueSoonDays: 30}); err != nil {
		t.Fatal(err)
	}

	var serviceID, profileID, frameworkID uint
	if err = db.Raw(`SELECT id FROM organization_services WHERE code = 'B03'`).Scan(&serviceID).Error; err != nil || serviceID == 0 {
		t.Fatal(err)
	}
	if err = db.Raw(`SELECT id FROM staff_profiles WHERE employee_code = 'B03'`).Scan(&profileID).Error; err != nil || profileID == 0 {
		t.Fatal(err)
	}
	if err = db.Raw(`SELECT id FROM grc_frameworks WHERE code = 'B03-FW'`).Scan(&frameworkID).Error; err != nil || frameworkID == 0 {
		t.Fatal(err)
	}
	var userID uint
	if err = db.Raw(`SELECT id FROM users WHERE email = 'b03-rehearsal@test.local'`).Scan(&userID).Error; err != nil || userID == 0 {
		t.Fatal(err)
	}

	var maxID int64
	if err = db.Raw(`SELECT COALESCE(MAX(id), 0) FROM grc_assets`).Scan(&maxID).Error; err != nil {
		t.Fatal(err)
	}
	created, err := svc.SaveAsset(0, userID, grc.AssetRequest{
		Code: "B03-ASSET-2", Name: "Actif restauré", Type: grc.AssetTypeApplication,
		Criticality: grc.AssetCriticalityLow, Status: grc.AssetStatusActive,
		OwningServiceID: &serviceID, OwnerProfileID: &profileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if int64(created.ID) <= maxID {
		t.Fatalf("restored sequence collided: new=%d max=%d", created.ID, maxID)
	}
	var auditRow audit.AuditLog
	if err = db.Where("module = ? AND action = ? AND record_id = ? AND user_id = ?", "GRC_ASSET", "CREATE", created.ID, userID).First(&auditRow).Error; err != nil {
		t.Fatal(err)
	}
	var auditsAfter int64
	if err = db.Model(&audit.AuditLog{}).Count(&auditsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if auditsAfter != auditCount+1 {
		t.Fatalf("audit count after mutation = %d, want %d", auditsAfter, auditCount+1)
	}
	if _, err = svc.SaveAsset(0, userID, grc.AssetRequest{
		Code: "B03-ASSET", Name: "Doublon", Type: grc.AssetTypeApplication,
		Criticality: grc.AssetCriticalityLow, Status: grc.AssetStatusActive,
	}); err == nil {
		t.Fatal("duplicate asset reference was accepted")
	}

	err = db.Exec(`DELETE FROM grc_frameworks WHERE id = ?`, frameworkID).Error
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || (pgErr.Code != "23001" && pgErr.Code != "23503") {
		t.Fatalf("FK restrict was not enforced: %v", err)
	}

	now := time.Now().UTC()
	err = db.Exec(`INSERT INTO grc_reminder_intents
		(kind, status, evidence_id, remediation_action_id, recipient_profile_id, occurrence_key, scheduled_for, attempt_count, created_at, updated_at)
		VALUES ('EVIDENCE_EXPIRING', 'PENDING', 1, 1, ?, 'bad-two-targets', ?, 0, ?, ?)`,
		profileID, now, now, now).Error
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || (pgErr.ConstraintName != "grc_reminder_one_target" && pgErr.ConstraintName != "grc_reminder_kind_target") {
		t.Fatalf("multi-target reminder was not rejected by a check: %v", err)
	}
}

func smokeAfterMigrate(t *testing.T, name string) {
	t.Helper()
	db := openGorm(t, name)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	svc := grc.NewService(db)
	var serviceID, profileID, userID uint
	if err = db.Raw(`SELECT id FROM organization_services WHERE code = 'B03'`).Scan(&serviceID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Raw(`SELECT id FROM staff_profiles WHERE employee_code = 'B03'`).Scan(&profileID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Raw(`SELECT id FROM users WHERE email = 'b03-rehearsal@test.local'`).Scan(&userID).Error; err != nil {
		t.Fatal(err)
	}
	created, err := svc.SaveAsset(0, userID, grc.AssetRequest{
		Code: "B03-ASSET-3", Name: "Après migration", Type: grc.AssetTypeApplication,
		Criticality: grc.AssetCriticalityLow, Status: grc.AssetStatusActive,
		OwningServiceID: &serviceID, OwnerProfileID: &profileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var n int64
	if err = db.Model(&audit.AuditLog{}).Where("record_id = ? AND module = ?", created.ID, "GRC_ASSET").Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("post-migrate audit count=%d err=%v", n, err)
	}
	if _, err = svc.Dashboard(grc.DashboardFilter{ExpiringSoonDays: 30, DueSoonDays: 30}); err != nil {
		t.Fatal(err)
	}
}

type dbManifest struct {
	schemas     int
	tables      int
	grcTables   string
	constraints string
	indexes     string
	sequences   string
	rows        map[string]int64
}

func readManifest(t *testing.T, name string) dbManifest {
	t.Helper()
	db := openSQL(t, name)
	defer db.Close()
	var current string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current != name {
		t.Fatalf("manifest read %s", current)
	}
	out := dbManifest{rows: map[string]int64{}}
	mustQueryOne(t, db, `SELECT count(*) FROM information_schema.schemata WHERE schema_name NOT LIKE 'pg\_%' AND schema_name <> 'information_schema'`, &out.schemas)
	mustQueryOne(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'`, &out.tables)
	out.grcTables = queryLines(t, db, `SELECT relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND c.relkind = 'r' AND (c.relname LIKE 'grc_%' OR c.relname = 'audit_logs') ORDER BY 1`)
	out.constraints = queryLines(t, db, `SELECT c.relname || '|' || con.conname || '|' || pg_get_constraintdef(con.oid) FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public' AND (c.relname LIKE 'grc_%' OR c.relname = 'audit_logs') ORDER BY 1`)
	out.indexes = queryLines(t, db, `SELECT tablename || '|' || indexname || '|' || indexdef FROM pg_indexes WHERE schemaname = 'public' AND (tablename LIKE 'grc_%' OR tablename = 'audit_logs') ORDER BY 1`)
	out.sequences = queryLines(t, db, `SELECT sequencename || '|' || COALESCE(last_value::text, 'null') FROM pg_sequences WHERE schemaname = 'public' ORDER BY 1`)
	for _, table := range strings.Split(strings.TrimSpace(out.grcTables), "\n") {
		if table == "" {
			continue
		}
		if !safeTable.MatchString(table) {
			t.Fatalf("unexpected table %q", table)
		}
		var n int64
		mustQueryOne(t, db, `SELECT count(*) FROM `+quoteIdent(table), &n)
		out.rows[table] = n
	}
	return out
}

var safeTable = regexpMust(`^(audit_logs|grc_[a-z_]+)$`)

func regexpMust(pattern string) *safePattern {
	return &safePattern{ok: pattern}
}

type safePattern struct{ ok string }

func (p *safePattern) MatchString(value string) bool {
	for _, r := range value {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	if value == "audit_logs" {
		return true
	}
	return strings.HasPrefix(value, "grc_") && len(value) > 4
}

// canonicalBlock ignores PostgreSQL's equivalent CHECK deparse
// (ARRAY['A'::varchar] versus ARRAY[('A'::varchar)::text]) after restore.
func canonicalBlock(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = canonicalLine(line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func canonicalLine(value string) string {
	value = strings.ReplaceAll(value, "::character varying", "")
	value = strings.ReplaceAll(value, "::text", "")
	var b strings.Builder
	for _, r := range value {
		if r == '(' || r == ')' || r == '[' || r == ']' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func diffText(left, right string) string {
	if left == right {
		return ""
	}
	seen := map[string]int{}
	for _, line := range strings.Split(left, "\n") {
		seen[line]++
	}
	var out []string
	for _, line := range strings.Split(right, "\n") {
		if seen[line] == 0 {
			out = append(out, "+ "+line)
		} else {
			seen[line]--
		}
	}
	for line, n := range seen {
		for n > 0 {
			out = append(out, "- "+line)
			n--
		}
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return strings.Join(out, "\n")
}

func formatRows(rows map[string]int64) string {
	parts := make([]string, 0, len(rows))
	for table, count := range rows {
		parts = append(parts, fmt.Sprintf("%s=%d", table, count))
	}
	return strings.Join(parts, " ")
}

func runMigrate(t *testing.T, bin, name string) {
	t.Helper()
	if err := CheckNames(name, "medcore_restore_test_1000000000", "medcore_restore_corrupt_1000000000"); err != nil && !strings.HasPrefix(name, "medcore_b03_source_") && !strings.HasPrefix(name, "medcore_restore_test_") {
		t.Fatal(err)
	}
	if name == ForbiddenMedcore || name == ForbiddenMedcoreHIS || name == "medcore_test" {
		t.Fatalf("refusing to migrate %s", name)
	}
	cmd := exec.Command(bin)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
		"APP_ENV=test",
		"DATABASE_URL=" + localDSN(name),
		"JWT_SECRET=test-only-not-a-secret",
		"MEDCORE_BUSINESS_TIMEZONE=UTC",
		"MEDCORE_TIMEZONE=UTC",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("migrate %s: %v\n%s", name, err, redact(string(out)))
	}
}

func dropRehearsalDB(name string) {
	if name == ForbiddenMedcore || name == ForbiddenMedcoreHIS || name == "medcore_test" || name == "postgres" {
		return
	}
	if err := CheckNames(
		firstMatching(name, "medcore_b03_source_1000000000"),
		firstMatching(name, "medcore_restore_test_1000000000"),
		firstMatching(name, "medcore_restore_corrupt_1000000000"),
	); err != nil && !sourceNameMatches(name) && !restoreNameMatches(name) && !corruptNameMatches(name) {
		return
	}
	if !sourceNameMatches(name) && !restoreNameMatches(name) && !corruptNameMatches(name) {
		return
	}
	admin, err := sql.Open("pgx", localDSN("postgres"))
	if err != nil {
		return
	}
	defer admin.Close()
	_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + quoteIdent(name) + ` WITH (FORCE)`)
}

func firstMatching(name, fallback string) string {
	if sourceNameMatches(name) || restoreNameMatches(name) || corruptNameMatches(name) {
		return name
	}
	return fallback
}

func sourceNameMatches(name string) bool {
	return strings.HasPrefix(name, "medcore_b03_source_") && len(name) > len("medcore_b03_source_")+9
}
func restoreNameMatches(name string) bool {
	return strings.HasPrefix(name, "medcore_restore_test_") && len(name) > len("medcore_restore_test_")+9
}
func corruptNameMatches(name string) bool {
	return strings.HasPrefix(name, "medcore_restore_corrupt_") && len(name) > len("medcore_restore_corrupt_")+9
}

func localDSN(name string) string {
	if strings.Contains(name, "/") || strings.Contains(name, "?") || strings.Contains(name, " ") {
		panic("unsafe database name")
	}
	return "postgres:///" + name + "?host=/tmp&sslmode=disable"
}

func quoteIdent(name string) string {
	if strings.Contains(name, `"`) || name == "" {
		panic("unsafe identifier")
	}
	return `"` + name + `"`
}

func openSQL(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", localDSN(name))
	if err != nil {
		t.Fatal(redact(err.Error()))
	}
	if err = db.Ping(); err != nil {
		t.Fatal(redact(err.Error()))
	}
	return db
}

func openGorm(t *testing.T, name string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(localDSN(name)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(redact(err.Error()))
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(redact(err.Error()))
	}
}

func mustQueryOne(t *testing.T, db *sql.DB, query string, dest any) {
	t.Helper()
	if err := db.QueryRow(query).Scan(dest); err != nil {
		t.Fatal(redact(err.Error()))
	}
}

func queryLines(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(redact(err.Error()))
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func backendRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(dir, "..", "..", ".."))
	if _, err = os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	return root
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	out := runOutput(t, dir, name, args...)
	_ = out
}

func runOutput(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, redact(string(out)))
	}
	return string(out)
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func redact(value string) string {
	value = strings.ReplaceAll(value, localDSN("placeholder"), "postgres://REDACTED")
	if idx := strings.Index(value, "postgres://"); idx >= 0 {
		end := strings.IndexAny(value[idx:], " \n\t")
		if end < 0 {
			return value[:idx] + "postgres://REDACTED"
		}
		return value[:idx] + "postgres://REDACTED" + value[idx+end:]
	}
	return value
}
