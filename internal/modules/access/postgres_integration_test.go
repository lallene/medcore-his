package access

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func accessDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("acc_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, e := gorm.Open(postgres.Open(dsn+sep+"search_path="+url.QueryEscape(schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(
		&auth.User{}, &organization.Department{}, &organization.Service{},
		&staff.Profile{}, &organization.StaffServiceAssignment{},
		&staff.Function{}, &staff.Specialty{}, &staff.Capability{}, &staff.AuditEvent{},
		&PermissionOverride{}, &MatrixOverride{}, &AccessAuditEvent{},
	); e != nil {
		t.Fatal(e)
	}
	return db
}

func seedUser(t *testing.T, db *gorm.DB, email, role string, functions []string) (userID, profileID uint) {
	t.Helper()
	u := auth.User{Name: email, Email: email, Role: role, IsActive: true, PasswordHash: "x"}
	if e := db.Create(&u).Error; e != nil {
		t.Fatal(e)
	}
	active := true
	svc := staff.NewService(db)
	view, e := svc.Upsert(0, staff.UpsertRequest{
		UserID: u.ID, EmployeeCode: "E-" + email, JobTitle: "Test", PrimaryDepartment: "Admin",
		Active: &active, Functions: functions,
	}, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	return u.ID, view.ID
}

func wireAntiLockoutHook(svc *Service) {
	staff.AfterProfileChangeValidate = func(db *gorm.DB, userID uint, active bool, functions, specialties []string) error {
		lock := NewService(db)
		if !active {
			return lock.assertNotLastAdmin(userID, nil)
		}
		var role string
		_ = db.Table("users").Select("role").Where("id=?", userID).Scan(&role)
		overlays, err := lock.loadMatrixOverlays()
		if err != nil {
			return err
		}
		overrides, err := lock.loadUserOverrides(userID)
		if err != nil {
			return err
		}
		perms := rbac.EffectiveStaffPermissionsFull(role, functions, specialties, overlays, overrides)
		return lock.assertNotLastAdmin(userID, perms)
	}
}

func TestPostgresOverridesAndAntiLockout(t *testing.T) {
	db := accessDB(t)
	svc := NewService(db)
	auth.EffectivePermissionsHook = func(_ *gorm.DB, userID uint, _ string, _, _ []string) ([]string, error) {
		return svc.ComputeEffectivePermissions(userID)
	}
	wireAntiLockoutHook(svc)
	t.Cleanup(func() {
		auth.EffectivePermissionsHook = nil
		staff.AfterProfileChangeValidate = nil
	})

	adminUID, adminPID := seedUser(t, db, "admin-rbac@test.local", "staff", []string{"DIRECTEUR_ADMINISTRATIF"})
	docUID, docPID := seedUser(t, db, "doc-rbac@test.local", "staff", nil)
	// physician via specialty
	_ = db.Create(&staff.Specialty{ProfileID: docPID, Code: "MEDECINE_GENERALE", Active: true, AssignedBy: adminUID, AssignedAt: time.Now().UTC()})

	perms, e := svc.ComputeEffectivePermissions(docUID)
	if e != nil {
		t.Fatal(e)
	}
	if !rbac.HasAnyPermission(perms, "queue.doctor.read") {
		t.Fatalf("doctor should inherit queue.doctor.read: %+v", perms)
	}

	// DENY doctor take
	if _, e := svc.SetOverride(docPID, adminUID, OverrideRequest{Permission: "queue.doctor.take", Effect: rbac.OverrideDeny, Reason: "test"}); e != nil {
		t.Fatal(e)
	}
	perms, _ = svc.ComputeEffectivePermissions(docUID)
	if rbac.HasAnyPermission(perms, "queue.doctor.take") {
		t.Fatal("DENY queue.doctor.take failed")
	}
	entries, _ := svc.Explain(docUID)
	foundDeny := false
	for _, en := range entries {
		if en.Permission == "queue.doctor.take" && !en.Allowed && en.Source == rbac.SourceDirectDeny {
			foundDeny = true
		}
	}
	if !foundDeny {
		t.Fatal("explain missing DIRECT_DENY")
	}

	// GRANT qa.read
	if _, e := svc.SetOverride(docPID, adminUID, OverrideRequest{Permission: "qa.read", Effect: rbac.OverrideGrant}); e != nil {
		t.Fatal(e)
	}
	perms, _ = svc.ComputeEffectivePermissions(docUID)
	if !rbac.HasAnyPermission(perms, "qa.read") {
		t.Fatal("GRANT qa.read failed")
	}

	sim, e := svc.Simulate(docPID)
	if e != nil {
		t.Fatal(e)
	}
	visibleDoctor := false
	for _, n := range sim.Navigation {
		if n.Href == "/queue/doctor" && n.Visible {
			visibleDoctor = true
		}
	}
	if !visibleDoctor {
		t.Fatal("simulation should show doctor queue")
	}

	// anti-lockout: cannot strip last directeur via ACC
	if _, e := svc.SetFunctions(adminPID, adminUID, FunctionsRequest{Functions: []string{"CAISSIER"}}); e == nil {
		t.Fatal("expected anti-lockout on last RBAC admin")
	}

	// anti-lockout: Staff Upsert must also refuse stripping last directeur
	active := true
	if _, e := staff.NewService(db).Upsert(adminPID, staff.UpsertRequest{
		UserID: adminUID, EmployeeCode: "E-admin-rbac@test.local", JobTitle: "Test", PrimaryDepartment: "Admin",
		Active: &active, Functions: []string{"CAISSIER"},
	}, adminUID); e == nil {
		t.Fatal("expected staff Upsert anti-lockout on last RBAC admin")
	}

	// With a technical role=admin backup, matrix DENY of staff.manage on DIRECTEUR is allowed
	_ = db.Create(&auth.User{Name: "tech", Email: "tech-admin@test.local", Role: "admin", IsActive: true, PasswordHash: "x"})
	if e := svc.ToggleMatrix(adminUID, MatrixToggleRequest{
		FunctionCode: "DIRECTEUR_ADMINISTRATIF", Permission: "staff.manage", Effect: rbac.OverrideDeny, Reason: "ok-with-admin-role",
	}); e != nil {
		t.Fatalf("matrix DENY with role=admin backup should succeed: %v", e)
	}

	audits, e := svc.Audit(docUID, 20)
	if e != nil || len(audits) < 1 {
		t.Fatalf("audit missing: %v %d", e, len(audits))
	}
}

func TestPostgresInactiveProfileHasNoEffectivePermissions(t *testing.T) {
	db := accessDB(t)
	svc := NewService(db)
	wireAntiLockoutHook(svc)
	t.Cleanup(func() { staff.AfterProfileChangeValidate = nil })

	// RBAC admin first so non-admin seed / deactivate are allowed
	_, _ = seedUser(t, db, "keeper-dir@test.local", "staff", []string{"DIRECTEUR_ADMINISTRATIF"})
	uid, pid := seedUser(t, db, "inactive-doc@test.local", "staff", []string{"CAISSIER"})

	detail, e := svc.SetActive(pid, uid, ActiveRequest{Active: false, Reason: "deactivate-test"})
	if e != nil {
		t.Fatal(e)
	}
	if detail.Active {
		t.Fatal("expected inactive summary")
	}
	perms, e := svc.ComputeEffectivePermissions(detail.UserID)
	if e != nil {
		t.Fatal(e)
	}
	if len(perms) != 0 {
		t.Fatalf("inactive profile must have empty effective perms, got %+v", perms)
	}
	var userActive bool
	_ = db.Table("users").Select("is_active").Where("id=?", detail.UserID).Scan(&userActive)
	if userActive {
		t.Fatal("users.is_active must follow ACC deactivate")
	}

	// Profile-only deactivate (user flag left true) must still yield no effective perms
	_ = db.Table("users").Where("id=?", detail.UserID).Update("is_active", true)
	_ = db.Model(&staff.Profile{}).Where("id=?", pid).Update("active", false)
	perms, _ = svc.ComputeEffectivePermissions(detail.UserID)
	if len(perms) != 0 {
		t.Fatalf("inactive staff profile must empty effective perms even if user.is_active=true: %+v", perms)
	}
}

func TestPostgresMatrixAntiLockoutBlocksLastAdminWipe(t *testing.T) {
	db := accessDB(t)
	svc := NewService(db)
	wireAntiLockoutHook(svc)
	t.Cleanup(func() { staff.AfterProfileChangeValidate = nil })

	adminUID, _ := seedUser(t, db, "solo-dir@test.local", "staff", []string{"DIRECTEUR_ADMINISTRATIF"})
	// Deny every permission that CanAdministerRBAC checks — last DENY that drops count to 0 must fail & rollback
	keys := []string{"staff.manage", "rbac.user.manage"}
	for i, p := range keys {
		err := svc.ToggleMatrix(adminUID, MatrixToggleRequest{
			FunctionCode: "DIRECTEUR_ADMINISTRATIF", Permission: p, Effect: rbac.OverrideDeny, Reason: "solo-lockout",
		})
		if i < len(keys)-1 {
			if err != nil {
				t.Fatalf("DENY %s should succeed while other admin key remains: %v", p, err)
			}
			continue
		}
		if err == nil {
			t.Fatal("expected matrix anti-lockout when last admin capability would disappear")
		}
		var n int64
		db.Model(&MatrixOverride{}).Where("function_code=? AND permission=? AND active", "DIRECTEUR_ADMINISTRATIF", p).Count(&n)
		if n != 0 {
			t.Fatal("failed matrix DENY must rollback (no active overlay left)")
		}
	}
	perms, _ := svc.ComputeEffectivePermissions(adminUID)
	if !rbac.CanAdministerRBAC(perms) {
		t.Fatal("solo directeur must still administer RBAC after blocked matrix wipe")
	}
}
