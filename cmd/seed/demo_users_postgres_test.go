package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/lallene/medcore-his/backend/internal/modules/access"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
)

// seedUsersDB opens an isolated schema with the account / staff / organization tables.
func seedUsersDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.Replace(pgtest.DSN(t), "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("seed_users_%d", time.Now().UnixNano())
	ident := pgx.Identifier{schema}.Sanitize()
	if err := admin.Exec("CREATE SCHEMA " + ident).Error; err != nil {
		t.Fatal(err)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["search_path"] = ident
	sqlDB := stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+ident)
		return err
	}))
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, _ := admin.DB()
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA IF EXISTS " + ident + " CASCADE").Error
		_ = adminSQL.Close()
	})
	if err := db.AutoMigrate(&auth.User{}, &organization.Department{}, &organization.Service{}, &staff.Profile{},
		&organization.StaffServiceAssignment{}, &staff.Function{}, &staff.Specialty{}, &staff.Capability{},
		&staff.AuditEvent{}, &access.PermissionOverride{}, &access.MatrixOverride{}, &access.AccessAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	if _, err := organization.SeedReference(db, 1); err != nil {
		t.Fatal(err)
	}
	return db
}

func withDemoHash(t *testing.T, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	prev := demoPasswordHash
	demoPasswordHash = string(hash)
	t.Cleanup(func() { demoPasswordHash = prev })
}

type userSnapshot struct {
	Name, Role, PasswordHash string
	IsActive                 bool
}

func snapshotUser(t *testing.T, db *gorm.DB, email string) userSnapshot {
	t.Helper()
	var u auth.User
	if err := db.Where("email=?", email).First(&u).Error; err != nil {
		t.Fatalf("%s: %v", email, err)
	}
	return userSnapshot{u.Name, u.Role, u.PasswordHash, u.IsActive}
}

// The production incident: an existing admin@medcore.local deactivated with a personal hash
// must stay strictly unchanged after any demo seed run (no reset, no reactivation).
func TestSeedAdminNeverModifiesExistingAccount(t *testing.T) {
	db := seedUsersDB(t)
	realHash, _ := bcrypt.GenerateFromPassword([]byte("operator-chosen-secret-xyz"), bcrypt.MinCost)
	existing := auth.User{Name: "Compte bootstrap", Email: "admin@medcore.local", PasswordHash: string(realHash), Role: "staff", IsActive: true}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&existing).Update("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	before := snapshotUser(t, db, "admin@medcore.local")

	withDemoHash(t, "demo-only-passphrase-42")
	seedAdmin(db)
	seedAdmin(db)

	after := snapshotUser(t, db, "admin@medcore.local")
	if after != before {
		t.Fatalf("existing account modified by seedAdmin:\nbefore=%+v\nafter=%+v", before, after)
	}
	if after.IsActive {
		t.Fatal("deactivated account must stay deactivated")
	}
	if bcrypt.CompareHashAndPassword([]byte(after.PasswordHash), []byte("demo-only-passphrase-42")) == nil {
		t.Fatal("demo password must not become a login path for an existing account")
	}
	var n int64
	db.Model(&auth.User{}).Where("email=?", "admin@medcore.local").Count(&n)
	if n != 1 {
		t.Fatalf("duplicate admin rows=%d", n)
	}
}

func TestSeedAdminCreatesOnFreshDatabaseWithProvidedPassword(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	seedAdmin(db)
	u := snapshotUser(t, db, "admin@medcore.local")
	if u.Role != "admin" || !u.IsActive {
		t.Fatalf("fresh admin: %+v", u)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("demo-only-passphrase-42")) != nil {
		t.Fatal("fresh demo admin must use MEDCORE_DEMO_SEED_PASSWORD")
	}
}

func TestSeedDemoStaffCreateOnlyAndIdempotent(t *testing.T) {
	db := seedUsersDB(t)
	withDemoHash(t, "demo-only-passphrase-42")
	// Pre-existing accounts on demo emails, deliberately different from the demo spec.
	otherHash, _ := bcrypt.GenerateFromPassword([]byte("someone-else-secret-1"), bcrypt.MinCost)
	cashier := auth.User{Name: "Renommée par l'exploitant", Email: "demo.caissiere@medcore.local", PasswordHash: string(otherHash), Role: "admin", IsActive: true}
	db.Create(&cashier)
	db.Model(&cashier).Update("is_active", false)
	before := snapshotUser(t, db, "demo.caissiere@medcore.local")

	seedDemoStaff(db, 1)
	var users1, profiles1 int64
	db.Model(&auth.User{}).Count(&users1)
	db.Model(&staff.Profile{}).Count(&profiles1)

	seedDemoStaff(db, 1)
	seedDemoInactiveStaff(db, 1)
	seedDemoInactiveStaff(db, 1)
	var users2, profiles2 int64
	db.Model(&auth.User{}).Count(&users2)
	db.Model(&staff.Profile{}).Count(&profiles2)

	if users2 != users1+1 || profiles2 != profiles1+1 {
		t.Fatalf("second run duplicated rows: users %d→%d profiles %d→%d", users1, users2, profiles1, profiles2)
	}
	if after := snapshotUser(t, db, "demo.caissiere@medcore.local"); after != before {
		t.Fatalf("existing demo-email account modified:\nbefore=%+v\nafter=%+v", before, after)
	}
	var cashierProfiles int64
	db.Model(&staff.Profile{}).Where("user_id=?", cashier.ID).Count(&cashierProfiles)
	if cashierProfiles != 0 {
		t.Fatal("seed must not attach a staff profile to an existing account")
	}
	inactive := snapshotUser(t, db, "demo.inactive@medcore.local")
	if inactive.IsActive {
		t.Fatal("inactive fixture must be created inactive")
	}
	// Every account created by the seed has its profile (no partial account).
	var orphans int64
	db.Raw(`SELECT COUNT(*) FROM users u WHERE u.email LIKE 'demo.%' AND u.id <> ? AND NOT EXISTS (SELECT 1 FROM staff_profiles p WHERE p.user_id=u.id)`, cashier.ID).Scan(&orphans)
	if orphans != 0 {
		t.Fatalf("%d demo account(s) without staff profile", orphans)
	}
	fresh := snapshotUser(t, db, "demo.infirmier@medcore.local")
	if !fresh.IsActive || fresh.Role != "staff" {
		t.Fatalf("fresh demo staff: %+v", fresh)
	}
}

// Real entrypoint against a non-allowlisted database: refused before any write.
func TestDemoSeedRefusedOnNonAllowedDatabaseWithoutWrites(t *testing.T) {
	dsn := pgtest.DSN(t)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var name string
	db.Raw("SELECT current_database()").Scan(&name)
	if validateDemoTarget(name) == nil {
		t.Skipf("test database %q is an allowed demo target; refusal cannot be exercised here", name)
	}
	var before int64
	db.Raw("SELECT COUNT(*) FROM pg_namespace").Scan(&before)
	if os.Getenv("SEED_REFUSE_CHILD") == "1" {
		os.Args = []string{"seed", "--demo-full"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDemoSeedRefusedOnNonAllowedDatabaseWithoutWrites$")
	cmd.Env = append(os.Environ(), "SEED_REFUSE_CHILD=1", "DATABASE_URL="+dsn, "APP_ENV=development",
		EnvAllowDemoSeed+"=1", EnvDemoSeedPassword+"=demo-only-passphrase-42")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "seed DEMO refusé sur la base") {
		t.Fatalf("expected refusal: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "demo-only-passphrase-42") {
		t.Fatal("password leaked in output")
	}
	var users int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name='users'").Scan(&users)
	if users != 0 {
		var n int64
		db.Raw("SELECT COUNT(*) FROM users WHERE email='admin@medcore.local'").Scan(&n)
		if n != 0 {
			t.Fatal("refused seed must not create the demo admin")
		}
	}
}
