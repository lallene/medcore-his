package complete

import (
	"context"
	"errors"
	"fmt"
	"net/url"
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

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
)

// isolatedSchema creates an empty schema and returns a DB bound to it plus its DSN.
func isolatedSchema(t *testing.T, prefix string) (*gorm.DB, string) {
	t.Helper()
	dsn := strings.Replace(pgtest.DSN(t), "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
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
	cfg.RuntimeParams["search_path"] = schema
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
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return db, u.String()
}

// migrate runs the real schema owner (cmd/migrate) on the isolated schema.
func migrate(t *testing.T, dsn string) {
	t.Helper()
	cmd := exec.Command("go", "run", "github.com/lallene/medcore-his/backend/cmd/migrate")
	cmd.Env = append(os.Environ(), "DATABASE_URL="+dsn, "APP_ENV=test", "JWT_SECRET=test-only-not-a-secret")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
}

// testAccounts is a create-only factory equivalent to cmd/seed's createDemoAccount.
func testAccounts(db *gorm.DB, name, email, role string, active bool, profile func(tx *gorm.DB, u auth.User) error) (auth.User, bool, error) {
	var out auth.User
	created := false
	err := db.Transaction(func(tx *gorm.DB) error {
		var existing auth.User
		if e := tx.Unscoped().Where("email = ?", email).First(&existing).Error; e == nil {
			out = existing
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte("test-only-passphrase"), bcrypt.MinCost)
		u := auth.User{Name: name, Email: email, PasswordHash: string(hash), Role: role, IsActive: true}
		if e := tx.Create(&u).Error; e != nil {
			return e
		}
		if !active {
			if e := tx.Model(&u).Update("is_active", false).Error; e != nil {
				return e
			}
		}
		out, created = u, true
		if profile != nil {
			return profile(tx, u)
		}
		return nil
	})
	return out, created, err
}

func testCatalog(db *gorm.DB) {
	svc := pharmacy.NewService(pharmacy.NewRepository(db))
	fam, err := svc.CreateFamily(pharmacy.CreateMedicationFamilyRequest{Code: "TST", Name: "Famille test"})
	if err != nil {
		panic(err)
	}
	for i, n := range []string{"Paracétamol", "Amoxicilline", "Ibuprofène", "Métronidazole", "Amlodipine"} {
		m, err := svc.CreateMedication(pharmacy.CreateMedicationRequest{FamilyID: fam.ID, Code: fmt.Sprintf("MED%d", i), Name: n})
		if err != nil {
			panic(err)
		}
		if _, err := svc.CreatePresentation(pharmacy.CreateMedicationPresentationRequest{MedicationID: m.ID, Code: fmt.Sprintf("PRES%d", i), Dosage: "500 mg", Form: "Comprimé", Route: "Orale", Unit: "comprimé"}); err != nil {
			panic(err)
		}
	}
}

func smallOptions() Options {
	anchor := time.Now().UTC().AddDate(0, 0, -40).Truncate(24 * time.Hour) // past anchor: fixed horizon
	return Options{
		Config:            Config{Anchor: anchor, Days: 21, Patients: 60, Seed: 99, Location: time.UTC},
		CreateAccount:     testAccounts,
		ReferenceCatalogs: testCatalog,
	}
}

// fingerprint summarises the logical dataset (statuses and amounts, not ids or instants).
func fingerprint(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var parts []string
	for _, q := range []string{
		"SELECT string_agg(status||':'||n, ',' ORDER BY status) FROM (SELECT status, COUNT(*) n FROM consultations GROUP BY 1) x",
		"SELECT string_agg(status||':'||n, ',' ORDER BY status) FROM (SELECT status, COUNT(*) n FROM laboratory_orders GROUP BY 1) x",
		"SELECT string_agg(status||':'||n, ',' ORDER BY status) FROM (SELECT status, COUNT(*) n FROM billing_invoices GROUP BY 1) x",
		"SELECT COALESCE(SUM(gross_amount),0)||'/'||COALESCE(SUM(insurance_amount),0)||'/'||COALESCE(SUM(paid_amount),0) FROM billing_invoices",
		"SELECT string_agg(stage||':'||n, ',' ORDER BY stage) FROM (SELECT stage, COUNT(*) n FROM patient_queue_tickets GROUP BY 1) x",
		"SELECT string_agg(nom||' '||prenoms, ',' ORDER BY id) FROM (SELECT id, nom, prenoms FROM patients ORDER BY id LIMIT 20) x",
	} {
		var s *string
		if err := db.Raw(q).Scan(&s).Error; err != nil {
			t.Fatal(err)
		}
		if s != nil {
			parts = append(parts, *s)
		} else {
			parts = append(parts, "-")
		}
	}
	return strings.Join(parts, " | ")
}

func TestTimewarpReanchorsWindowsOnly(t *testing.T) {
	db, _ := isolatedSchema(t, "dc_warp")
	if err := db.Exec(`CREATE TABLE warp_demo (id serial PRIMARY KEY, created_at timestamptz, updated_at timestamptz, done_at timestamptz, due_on date)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE patients (id serial PRIMARY KEY, date_naissance timestamptz, created_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	tw, err := NewTimewarp(db, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO warp_demo (created_at, updated_at) VALUES (?, ?)`, old, old).Error; err != nil {
		t.Fatal(err)
	}
	sim1 := time.Date(2025, 11, 3, 9, 30, 0, 0, time.UTC)
	sim2 := time.Date(2025, 11, 4, 14, 0, 0, 0, time.UTC)
	birth := time.Date(1990, 5, 1, 0, 0, 0, 0, time.UTC)
	_ = tw.Run(sim1, func() error {
		time.Sleep(time.Millisecond) // steps last milliseconds in practice
		now := time.Now()
		return db.Exec(`INSERT INTO warp_demo (created_at, updated_at, due_on) VALUES (?, ?, (?::timestamptz)::date + 30)`, now, now, now).Error
	})
	_ = tw.Run(sim1.Add(time.Hour), func() error {
		time.Sleep(time.Millisecond)
		return db.Exec(`INSERT INTO patients (date_naissance, created_at) VALUES (?, ?)`, birth, time.Now()).Error
	})
	_ = tw.Run(sim2, func() error {
		time.Sleep(time.Millisecond)
		return db.Exec(`UPDATE warp_demo SET done_at = ?, updated_at = ? WHERE id = 2`, time.Now(), time.Now()).Error
	})
	if err := tw.Flush(); err != nil {
		t.Fatal(err)
	}
	var r struct {
		CreatedAt, UpdatedAt, DoneAt time.Time
		DueOn                        time.Time
	}
	db.Raw(`SELECT created_at, updated_at, done_at, due_on FROM warp_demo WHERE id = 2`).Scan(&r)
	near := func(a, b time.Time) bool { d := a.Sub(b); return d > -time.Second && d < time.Second }
	if !near(r.CreatedAt, sim1) || !near(r.DoneAt, sim2) || !near(r.UpdatedAt, sim2) {
		t.Fatalf("re-anchoring: created %s done %s updated %s", r.CreatedAt, r.DoneAt, r.UpdatedAt)
	}
	if !r.DueOn.Equal(sim1.Truncate(24*time.Hour).AddDate(0, 0, 30)) && !r.DueOn.Equal(sim2.Truncate(24*time.Hour).AddDate(0, 0, 30)) {
		t.Fatalf("date column shifted with its row: %s", r.DueOn)
	}
	var untouched time.Time
	db.Raw(`SELECT created_at FROM warp_demo WHERE id = 1`).Scan(&untouched)
	var b time.Time
	db.Raw(`SELECT date_naissance FROM patients`).Scan(&b)
	if !untouched.Equal(old) || !b.Equal(birth) {
		t.Fatal("rows outside the windows and planned columns must not move")
	}
}

func TestDemoCompleteSmallDatasetIsCoherentDeterministicAndRerunSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("full pipeline")
	}
	dbA, dsnA := isolatedSchema(t, "dc_full_a")
	migrate(t, dsnA)
	rep, err := Run(dbA, smallOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Checks {
		coherence := strings.Contains(c.Detail, "incohérente")
		if (coherence || c.Domain == "RBAC / organisation" || c.Domain == "Intégrité") && c.Critical && !c.OK {
			t.Errorf("%s / %s: %s", c.Domain, c.Name, c.Detail)
		}
	}
	fpA := fingerprint(t, dbA)
	if !strings.Contains(fpA, "completed") {
		t.Fatalf("no completed consultation: %s", fpA)
	}

	// Re-run on the generated database: validation only, nothing added.
	var before int64
	dbA.Table("consultations").Count(&before)
	if _, err := Run(dbA, smallOptions()); !errors.Is(err, ErrDatasetExists) {
		t.Fatalf("re-run must only validate, got %v", err)
	}
	var after int64
	dbA.Table("consultations").Count(&after)
	if after != before {
		t.Fatalf("re-run wrote data: %d → %d", before, after)
	}

	// Same configuration on a second empty database: same logical dataset.
	dbB, dsnB := isolatedSchema(t, "dc_full_b")
	migrate(t, dsnB)
	// A foreign patient makes the target ineligible.
	if err := dbB.Exec(`INSERT INTO patients (code_patient, numero_dossier, nom, created_at, updated_at) VALUES ('X1', 'X1', 'EXTERNE', now(), now())`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dbB, smallOptions()); err == nil || !strings.Contains(err.Error(), "refusé") {
		t.Fatalf("a database with foreign patients must be refused, got %v", err)
	}
	if err := dbB.Exec(`DELETE FROM patients`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dbB, smallOptions()); err != nil {
		t.Fatal(err)
	}
	if fpB := fingerprint(t, dbB); fpB != fpA {
		t.Fatalf("determinism:\nA=%s\nB=%s", fpA, fpB)
	}
}
