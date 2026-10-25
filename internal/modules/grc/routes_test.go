package grc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func grcTestRouter(permissions []string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if permissions != nil {
			rbac.SetUser(c, 7, "staff", permissions)
		}
		c.Next()
	})

	RegisterRoutes(r.Group("/api"), NewHandler(nil))
	return r
}

func grcRequest(r http.Handler, method, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestGRCRoutesRegistered(t *testing.T) {
	r := grcTestRouter(nil)

	want := map[string]bool{
		"GET /api/grc/assets":     false,
		"GET /api/grc/assets/:id": false,
		"POST /api/grc/assets":    false,
		"PUT /api/grc/assets/:id": false,
		"GET /api/grc/risks":      false,
		"GET /api/grc/risks/:id":  false,
		"POST /api/grc/risks":     false,
		"PUT /api/grc/risks/:id":  false,
	}

	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}

	for route, found := range want {
		if !found {
			t.Fatalf("route absente: %s", route)
		}
	}
}

func TestGRCRoutesRequireAuthentication(t *testing.T) {
	r := grcTestRouter(nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/grc/assets/bad"},
		{http.MethodPost, "/api/grc/assets"},
		{http.MethodPut, "/api/grc/assets/bad"},
		{http.MethodGet, "/api/grc/risks/bad"},
		{http.MethodPost, "/api/grc/risks"},
		{http.MethodPut, "/api/grc/risks/bad"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := grcRequest(r, tc.method, tc.path)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCRoutesEnforceDedicatedPermissions(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		permissions []string
	}{
		{
			name:        "risk read cannot manage asset",
			method:      http.MethodPut,
			path:        "/api/grc/assets/bad",
			permissions: []string{"grc.risk.read"},
		},
		{
			name:        "asset read cannot manage asset",
			method:      http.MethodPut,
			path:        "/api/grc/assets/bad",
			permissions: []string{"grc.asset.read"},
		},
		{
			name:        "asset manage cannot read asset",
			method:      http.MethodGet,
			path:        "/api/grc/assets/bad",
			permissions: []string{"grc.asset.manage"},
		},
		{
			name:        "risk read cannot create risk",
			method:      http.MethodPost,
			path:        "/api/grc/risks",
			permissions: []string{"grc.risk.read"},
		},
		{
			name:        "risk create cannot update risk",
			method:      http.MethodPut,
			path:        "/api/grc/risks/bad",
			permissions: []string{"grc.risk.create"},
		},
		{
			name:        "risk update cannot create risk",
			method:      http.MethodPost,
			path:        "/api/grc/risks",
			permissions: []string{"grc.risk.update"},
		},
		{
			name:        "risk approve cannot create risk",
			method:      http.MethodPost,
			path:        "/api/grc/risks",
			permissions: []string{"grc.risk.approve"},
		},
		{
			name:        "risk approve cannot update risk",
			method:      http.MethodPut,
			path:        "/api/grc/risks/bad",
			permissions: []string{"grc.risk.approve"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := grcTestRouter(tc.permissions)
			w := grcRequest(r, tc.method, tc.path)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCRoutesAllowDedicatedPermissions(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		permissions []string
	}{
		{
			name:        "asset read",
			method:      http.MethodGet,
			path:        "/api/grc/assets/bad",
			permissions: []string{"grc.asset.read"},
		},
		{
			name:        "asset manage",
			method:      http.MethodPut,
			path:        "/api/grc/assets/bad",
			permissions: []string{"grc.asset.manage"},
		},
		{
			name:        "risk read",
			method:      http.MethodGet,
			path:        "/api/grc/risks/bad",
			permissions: []string{"grc.risk.read"},
		},
		{
			name:        "risk update",
			method:      http.MethodPut,
			path:        "/api/grc/risks/bad",
			permissions: []string{"grc.risk.update"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := grcTestRouter(tc.permissions)
			w := grcRequest(r, tc.method, tc.path)

			if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
				t.Fatalf("permission dédiée refusée: status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected handler 400 after RBAC, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCHandlerRejectsInvalidNumericQueries(t *testing.T) {
	cases := []struct {
		name string
		path string
		perm string
	}{
		{
			name: "asset serviceId",
			path: "/api/grc/assets?serviceId=abc",
			perm: "grc.asset.read",
		},
		{
			name: "asset ownerProfileId",
			path: "/api/grc/assets?ownerProfileId=0",
			perm: "grc.asset.read",
		},
		{
			name: "risk assetId",
			path: "/api/grc/risks?assetId=abc",
			perm: "grc.risk.read",
		},
		{
			name: "risk serviceId",
			path: "/api/grc/risks?serviceId=0",
			perm: "grc.risk.read",
		},
		{
			name: "risk ownerProfileId",
			path: "/api/grc/risks?ownerProfileId=-1",
			perm: "grc.risk.read",
		},
		{
			name: "risk approverProfileId",
			path: "/api/grc/risks?approverProfileId=nope",
			perm: "grc.risk.read",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := grcTestRouter([]string{tc.perm})
			w := grcRequest(r, http.MethodGet, tc.path)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "invalide") {
				t.Fatalf("unexpected body=%s", w.Body.String())
			}
		})
	}
}
