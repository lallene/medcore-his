package act_catalog

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func apiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:act_catalog_api_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func apiRouter(db *gorm.DB, perms []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 42, "staff", perms)
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(db)))
	return r
}

func TestAPIReadForbiddenWithoutPermission(t *testing.T) {
	db := apiDB(t)
	r := apiRouter(db, []string{"billing.read"})
	req := httptest.NewRequest(http.MethodGet, "/api/act-catalog", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIManageForbiddenForReadOnly(t *testing.T) {
	db := apiDB(t)
	r := apiRouter(db, []string{"act_catalog.read"})
	body := []byte(`{"code":"X1","label":"X","category":"OTHER","basePrice":0}`)
	req := httptest.NewRequest(http.MethodPost, "/api/act-catalog", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPICRUDAndDuplicateConflict(t *testing.T) {
	db := apiDB(t)
	r := apiRouter(db, []string{"act_catalog.read", "act_catalog.manage"})

	createBody := []byte(`{"code":"img-echo","label":"Écho","category":"imaging","basePrice":12000}`)
	req := httptest.NewRequest(http.MethodPost, "/api/act-catalog", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created Entry
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Code != "IMG-ECHO" || created.Category != "IMAGING" {
		t.Fatalf("created=%+v", created)
	}

	idPath := "/api/act-catalog/" + strconv.FormatUint(uint64(created.ID), 10)
	req = httptest.NewRequest(http.MethodGet, idPath, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get status=%d", w.Code)
	}

	dup := []byte(`{"code":"IMG-ECHO","label":"Dup","category":"IMAGING","basePrice":1}`)
	req = httptest.NewRequest(http.MethodPost, "/api/act-catalog", bytes.NewReader(dup))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("dup status=%d body=%s", w.Code, w.Body.String())
	}

	upd := []byte(`{"label":"Échographie","category":"IMAGING","basePrice":15000,"isActive":false}`)
	req = httptest.NewRequest(http.MethodPut, idPath, bytes.NewReader(upd))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", w.Code, w.Body.String())
	}
	var updated Entry
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Code != "IMG-ECHO" || updated.IsActive || updated.BasePrice != 15000 {
		t.Fatalf("updated=%+v", updated)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/act-catalog", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/act-catalog?active=false", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	var page Page
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("inactive list total=%d", page.Total)
	}
}
