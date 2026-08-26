package staff

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func staffDSN(dsn, schema string) string {
	if strings.Contains(dsn, "://") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + url.QueryEscape(schema)
	}
	return dsn + " search_path=" + schema
}
func staffDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("staff_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	db, e := gorm.Open(postgres.Open(staffDSN(dsn, schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(
		&auth.User{},
		&organization.Department{},
		&organization.Service{},
		&Profile{},
		&organization.StaffServiceAssignment{},
		&Function{},
		&Specialty{},
		&Capability{},
		&AuditEvent{},
	); e != nil {
		t.Fatal(e)
	}
	return db
}
func user(t *testing.T, db *gorm.DB, email string) auth.User {
	t.Helper()
	u := auth.User{Name: email, Email: email, PasswordHash: "x", Role: "staff", IsActive: true}
	if e := db.Create(&u).Error; e != nil {
		t.Fatal(e)
	}
	return u
}
func boolp(x bool) *bool { return &x }
func TestPostgresStaffMultiRoleAuditInactiveAndConstraints(t *testing.T) {
	db := staffDB(t)
	s := NewService(db)
	u := user(t, db, "director@test.local")
	v, e := s.Upsert(0, UpsertRequest{UserID: u.ID, EmployeeCode: "DIR-001", Active: boolp(true), Functions: []string{"DIRECTEUR_MEDICAL"}, Specialties: []string{"ORL", "MEDECINE_GENERALE"}}, 91)
	if e != nil {
		t.Fatal(e)
	}
	if len(v.Specialties) != 2 || !hasPermission(v.EffectivePermissions, "consultations.create") || hasPermission(v.EffectivePermissions, "cash.payment.create") {
		t.Fatalf("view=%+v", v)
	}
	v, e = s.Upsert(v.ID, UpsertRequest{UserID: u.ID, EmployeeCode: "DIR-001", Active: boolp(true), Functions: []string{"FACTURATION", "CAISSIER"}}, 92)
	if e != nil {
		t.Fatal(e)
	}
	if !hasPermission(v.EffectivePermissions, "billing.create") || !hasPermission(v.EffectivePermissions, "cash.payment.create") {
		t.Fatalf("multi=%v", v.EffectivePermissions)
	}
	staleToken, e := auth.GenerateToken("test-secret", u, v.EffectivePermissions, v.Functions, v.Specialties, v.Capabilities)
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.Upsert(v.ID, UpsertRequest{UserID: u.ID, EmployeeCode: "DIR-001", Active: boolp(true), Functions: []string{"FACTURATION"}}, 93)
	if e != nil || hasPermission(v.EffectivePermissions, "cash.payment.create") {
		t.Fatalf("removal=%+v %v", v, e)
	}
	gin.SetMode(gin.TestMode)
	permissionRouter := gin.New()
	permissionRouter.Use(auth.Middleware("test-secret", db))
	permissionRouter.GET("/cash", rbac.Permission("cash.payment.create"), func(c *gin.Context) { c.Status(http.StatusOK) })
	permissionReq := httptest.NewRequest(http.MethodGet, "/cash", nil)
	permissionReq.Header.Set("Authorization", "Bearer "+staleToken)
	permissionWriter := httptest.NewRecorder()
	permissionRouter.ServeHTTP(permissionWriter, permissionReq)
	if permissionWriter.Code != http.StatusForbidden {
		t.Fatalf("removed permission remained active status=%d", permissionWriter.Code)
	}
	audit, e := s.Audit(v.ID)
	if e != nil || len(audit) < 5 {
		t.Fatalf("audit=%v %v", audit, e)
	}
	v, e = s.Upsert(v.ID, UpsertRequest{UserID: u.ID, EmployeeCode: "DIR-001", Active: boolp(false), Functions: []string{"FACTURATION"}}, 94)
	if e != nil || v.Active {
		t.Fatal("deactivation failed")
	}
	var active bool
	db.Table("users").Select("is_active").Where("id=?", u.ID).Scan(&active)
	if active {
		t.Fatal("user remains active")
	}
	token, e := auth.GenerateToken("test-secret", u, []string{"billing.read"}, []string{"FACTURATION"}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(auth.Middleware("test-secret", db))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("inactive existing JWT status=%d", w.Code)
	}
	u2 := user(t, db, "second@test.local")
	if _, e = s.Upsert(0, UpsertRequest{UserID: u2.ID, EmployeeCode: "DIR-001", Active: boolp(true)}, 1); e == nil {
		t.Fatal("duplicate employee code accepted")
	}
	if e = db.Create(&Function{ProfileID: 999999, Code: "CAISSIER", Active: true, AssignedBy: 1, AssignedAt: time.Now()}).Error; e == nil {
		t.Fatal("foreign key violation accepted")
	}
}
func hasPermission(xs []string, p string) bool {
	for _, x := range xs {
		if x == p {
			return true
		}
	}
	return false
}
func TestPostgresConcurrentFunctionChangesRemainUnique(t *testing.T) {
	db := staffDB(t)
	s := NewService(db)
	u := user(t, db, "concurrent@test.local")
	v, e := s.Upsert(0, UpsertRequest{UserID: u.ID, EmployeeCode: "CONC-1", Active: boolp(true), Functions: []string{"FACTURATION"}}, 1)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, functions := range [][]string{{"FACTURATION", "CAISSIER"}, {"FACTURATION", "COMPTABLE"}} {
		wg.Add(1)
		go func(f []string) {
			defer wg.Done()
			_, e := s.Upsert(v.ID, UpsertRequest{UserID: u.ID, EmployeeCode: "CONC-1", Active: boolp(true), Functions: f}, 2)
			errs <- e
		}(functions)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var duplicates int64
	db.Raw("SELECT count(*) FROM (SELECT code,count(*) c FROM staff_functions WHERE profile_id=? GROUP BY code HAVING count(*)>1) x", v.ID).Scan(&duplicates)
	if duplicates != 0 {
		t.Fatal("duplicate assignments")
	}
}
