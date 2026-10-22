package help

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/lallene/medcore-his/backend/internal/modules/access"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"github.com/lallene/medcore-his/backend/internal/testsupport/pgtest"
)

func helpDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.Replace(pgtest.DSN(t), "-pooler", "", 1)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("help_%d", time.Now().UnixNano())
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
	sqlDB.SetMaxOpenConns(4)
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
	if err := db.AutoMigrate(&Entry{}, &auth.User{}, &organization.Department{}, &organization.Service{},
		&staff.Profile{}, &organization.StaffServiceAssignment{}, &staff.Function{}, &staff.Specialty{},
		&staff.Capability{}, &access.PermissionOverride{}, &access.MatrixOverride{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func countRows(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var n int64
	q := db.Model(&Entry{})
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSyncCatalogIsIdempotentAndUnique(t *testing.T) {
	db := helpDB(t)
	entries, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	first, err := SyncCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	var stamp time.Time
	db.Model(&Entry{}).Select("max(created_at)").Scan(&stamp)
	second, err := SyncCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	if first.Upserted != len(entries) || second.Upserted != len(entries) || second.Deactivated != 0 {
		t.Fatalf("sync results %+v / %+v", first, second)
	}
	if n := countRows(t, db, ""); n != int64(len(entries)) {
		t.Fatalf("rows=%d want %d", n, len(entries))
	}
	if n := countRows(t, db, "is_active"); n != int64(len(entries)) {
		t.Fatalf("active=%d", n)
	}
	var created time.Time
	db.Model(&Entry{}).Select("max(created_at)").Scan(&created)
	if !created.Equal(stamp) {
		t.Fatal("second sync must not recreate rows (created_at changed)")
	}
	// DB-enforced uniqueness on help_key.
	dup := Entry{HelpKey: entries[0].Key, Module: "billing", Page: "/x", ElementType: ElementField, ElementLabel: "x", ShortHelp: "x"}
	if err := db.Create(&dup).Error; err == nil {
		t.Fatal("duplicate help_key must violate the unique index")
	}
}

func TestSyncDeactivatesRemovedEntriesWithoutDeleting(t *testing.T) {
	db := helpDB(t)
	a := validEntry("billing.demo.kept")
	b := validEntry("billing.demo.removed")
	if _, err := syncEntries(db, []CatalogEntry{a, b}, "v1"); err != nil {
		t.Fatal(err)
	}
	res, err := syncEntries(db, []CatalogEntry{a}, "v2")
	if err != nil {
		t.Fatal(err)
	}
	if res.Deactivated != 1 {
		t.Fatalf("deactivated=%d", res.Deactivated)
	}
	if n := countRows(t, db, "help_key=?", "billing.demo.removed"); n != 1 {
		t.Fatal("removed entry must be kept (no physical delete)")
	}
	if n := countRows(t, db, "help_key=? AND is_active", "billing.demo.removed"); n != 0 {
		t.Fatal("removed entry must be inactive")
	}
	visible, err := NewService(db).Visible([]string{"*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range visible.Entries {
		if e.HelpKey == "billing.demo.removed" {
			t.Fatal("inactive entry must not be served")
		}
	}
	// Re-adding the key reactivates the same row.
	b.Short = "Texte mis à jour."
	if _, err := syncEntries(db, []CatalogEntry{a, b}, "v3"); err != nil {
		t.Fatal(err)
	}
	var row Entry
	db.Where("help_key=?", "billing.demo.removed").First(&row)
	if !row.IsActive || row.ShortHelp != "Texte mis à jour." || countRows(t, db, "") != 2 {
		t.Fatalf("re-added entry: %+v", row)
	}
}

func TestVisibleFiltersByPermissions(t *testing.T) {
	db := helpDB(t)
	public := validEntry("billing.demo.public")
	restricted := validEntry("billing.demo.refund")
	restricted.RequiredPermissions = []string{"billing.refund.execute", "billing.refund.approve"}
	if _, err := syncEntries(db, []CatalogEntry{public, restricted}, "v1"); err != nil {
		t.Fatal(err)
	}
	keys := func(perms []string) string {
		out, err := NewService(db).Visible(perms)
		if err != nil {
			t.Fatal(err)
		}
		var k []string
		for _, e := range out.Entries {
			k = append(k, e.HelpKey)
		}
		return strings.Join(k, ",")
	}
	if got := keys(nil); got != "billing.demo.public" {
		t.Fatalf("no permission: %s", got)
	}
	if got := keys([]string{"billing.read"}); got != "billing.demo.public" {
		t.Fatalf("unrelated permission: %s", got)
	}
	if got := keys([]string{"billing.refund.approve"}); !strings.Contains(got, "billing.demo.refund") {
		t.Fatalf("any-of permission: %s", got)
	}
	if got := keys([]string{"*"}); !strings.Contains(got, "billing.demo.refund") {
		t.Fatalf("admin wildcard: %s", got)
	}
	a, _ := NewService(db).Visible([]string{"billing.read"})
	b, _ := NewService(db).Visible([]string{"*"})
	if a.Version == b.Version {
		t.Fatal("version (ETag) must differ when the visible set differs")
	}
}

func TestHelpEndpointRequiresAuthenticationAndFilters(t *testing.T) {
	db := helpDB(t)
	if _, err := SyncCatalog(db); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	const secret = "test-only-jwt-secret-for-help-endpoint"
	r := gin.New()
	g := r.Group("/api")
	g.Use(auth.Middleware(secret, db))
	RegisterRoutes(g, NewHandler(NewService(db)))

	call := func(token, etag string) (*httptest.ResponseRecorder, CatalogResponse) {
		req := httptest.NewRequest(http.MethodGet, "/api/help/entries", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var body CatalogResponse
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w, body
	}
	if w, _ := call("", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous must get 401, got %d", w.Code)
	}
	if w, _ := call("not-a-jwt", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token must get 401, got %d", w.Code)
	}

	mkUser := func(email, role string) string {
		u := auth.User{Name: email, Email: email, PasswordHash: "x", Role: role, IsActive: true}
		if err := db.Create(&u).Error; err != nil {
			t.Fatal(err)
		}
		tok, err := auth.GenerateToken(secret, u, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	adminTok := mkUser("help-admin@test.invalid", "admin")
	staffTok := mkUser("help-staff@test.invalid", "staff")

	w, adminBody := call(adminTok, "")
	if w.Code != http.StatusOK || w.Header().Get("ETag") == "" {
		t.Fatalf("admin: %d etag=%q", w.Code, w.Header().Get("ETag"))
	}
	_, staffBody := call(staffTok, "")
	if len(staffBody.Entries) == 0 || len(staffBody.Entries) >= len(adminBody.Entries) {
		t.Fatalf("staff without finance rights must see fewer entries: staff=%d admin=%d", len(staffBody.Entries), len(adminBody.Entries))
	}
	for _, e := range staffBody.Entries {
		if strings.HasPrefix(e.HelpKey, "billing.refund.") || e.HelpKey == "patient.insurance.member_details" {
			t.Fatalf("restricted entry leaked to staff: %s", e.HelpKey)
		}
	}
	if strings.Contains(w.Body.String(), "requiredPermissions") || strings.Contains(w.Body.String(), `"id"`) {
		t.Fatal("response must not expose internal ids or permission lists")
	}
	if w304, _ := call(adminTok, w.Header().Get("ETag")); w304.Code != http.StatusNotModified {
		t.Fatalf("matching ETag must return 304, got %d", w304.Code)
	}
	// A deactivated account loses access immediately.
	db.Model(&auth.User{}).Where("email=?", "help-staff@test.invalid").Update("is_active", false)
	if w, _ := call(staffTok, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("inactive account must get 401, got %d", w.Code)
	}
}
