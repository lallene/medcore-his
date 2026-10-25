package grc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

const malformedJSON = `{"code":`

func grcJSONRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func grcServiceRouter(svc *GRCService, permissions []string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 7, "staff", permissions)
		c.Next()
	})

	RegisterRoutes(r.Group("/api"), NewHandler(svc))
	return r
}

type complianceRouteCase struct {
	method     string
	path       string
	body       string
	permission string
}

// complianceRoutesFailingBeforeService lists one request per route that the
// handler rejects with 400 before reaching the (nil) service.
func complianceRoutesFailingBeforeService() []complianceRouteCase {
	return []complianceRouteCase{
		{http.MethodGet, "/api/grc/frameworks/bad", "", "grc.framework.read"},
		{http.MethodPost, "/api/grc/frameworks", malformedJSON, "grc.framework.manage"},
		{http.MethodPut, "/api/grc/frameworks/bad", "{}", "grc.framework.manage"},

		{http.MethodGet, "/api/grc/requirements?frameworkId=abc", "", "grc.requirement.read"},
		{http.MethodGet, "/api/grc/requirements/bad", "", "grc.requirement.read"},
		{http.MethodPost, "/api/grc/requirements", malformedJSON, "grc.requirement.manage"},
		{http.MethodPut, "/api/grc/requirements/0", "{}", "grc.requirement.manage"},

		{http.MethodGet, "/api/grc/controls?serviceId=abc", "", "grc.control.read"},
		{http.MethodGet, "/api/grc/controls/bad", "", "grc.control.read"},
		{http.MethodPost, "/api/grc/controls", malformedJSON, "grc.control.manage"},
		{http.MethodPut, "/api/grc/controls/bad", "{}", "grc.control.manage"},

		{http.MethodGet, "/api/grc/control-implementations?controlId=abc", "", "grc.control.implementation.read"},
		{http.MethodGet, "/api/grc/control-implementations/bad", "", "grc.control.implementation.read"},
		{http.MethodPost, "/api/grc/control-implementations", malformedJSON, "grc.control.implementation.manage"},
		{http.MethodPut, "/api/grc/control-implementations/bad", "{}", "grc.control.implementation.manage"},

		{http.MethodPost, "/api/grc/requirement-controls", malformedJSON, "grc.control.manage"},
		{http.MethodDelete, "/api/grc/requirement-controls/bad/1", "", "grc.control.manage"},
		{http.MethodPost, "/api/grc/control-risks", malformedJSON, "grc.control.manage"},
		{http.MethodDelete, "/api/grc/control-risks/1/bad", "", "grc.control.manage"},
	}
}

func TestGRCComplianceRoutesRegistered(t *testing.T) {
	r := grcTestRouter(nil)

	want := map[string]bool{
		"GET /api/grc/frameworks":                                        false,
		"GET /api/grc/frameworks/:id":                                    false,
		"POST /api/grc/frameworks":                                       false,
		"PUT /api/grc/frameworks/:id":                                    false,
		"GET /api/grc/requirements":                                      false,
		"GET /api/grc/requirements/:id":                                  false,
		"POST /api/grc/requirements":                                     false,
		"PUT /api/grc/requirements/:id":                                  false,
		"GET /api/grc/controls":                                          false,
		"GET /api/grc/controls/:id":                                      false,
		"POST /api/grc/controls":                                         false,
		"PUT /api/grc/controls/:id":                                      false,
		"GET /api/grc/control-implementations":                           false,
		"GET /api/grc/control-implementations/:id":                       false,
		"POST /api/grc/control-implementations":                          false,
		"PUT /api/grc/control-implementations/:id":                       false,
		"POST /api/grc/requirement-controls":                             false,
		"DELETE /api/grc/requirement-controls/:requirementId/:controlId": false,
		"POST /api/grc/control-risks":                                    false,
		"DELETE /api/grc/control-risks/:controlId/:riskId":               false,
	}

	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}

	for route, found := range want {
		if !found {
			t.Errorf("route absente: %s", route)
		}
	}
}

func TestGRCComplianceRoutesRequireAuthentication(t *testing.T) {
	r := grcTestRouter(nil)

	for _, tc := range complianceRoutesFailingBeforeService() {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := grcJSONRequest(r, tc.method, tc.path, tc.body)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCComplianceRoutesAllowDedicatedPermission(t *testing.T) {
	for _, tc := range complianceRoutesFailingBeforeService() {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := grcTestRouter([]string{tc.permission})
			w := grcJSONRequest(r, tc.method, tc.path, tc.body)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected handler 400 after RBAC, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCComplianceRoutesAllowWildcard(t *testing.T) {
	r := grcTestRouter([]string{"*"})

	for _, tc := range complianceRoutesFailingBeforeService() {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := grcJSONRequest(r, tc.method, tc.path, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected handler 400 with wildcard, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGRCComplianceRoutesEnforceDedicatedPermissions(t *testing.T) {
	readOnly := map[string]string{
		"grc.framework.manage":              "grc.framework.read",
		"grc.requirement.manage":            "grc.requirement.read",
		"grc.control.manage":                "grc.control.read",
		"grc.control.implementation.manage": "grc.control.implementation.read",
	}
	manageOnly := map[string]string{}
	for manage, read := range readOnly {
		manageOnly[read] = manage
	}

	for _, tc := range complianceRoutesFailingBeforeService() {
		var denied []string
		if read, isManage := readOnly[tc.permission]; isManage {
			// *.read cannot write.
			denied = append(denied, read)
		}
		if manage, isRead := manageOnly[tc.permission]; isRead {
			// *.manage does not imply *.read.
			denied = append(denied, manage)
		}
		denied = append(denied, "grc.asset.manage", "grc.risk.approve")
		if tc.permission == "grc.control.manage" {
			denied = append(denied, "grc.control.implementation.manage", "grc.requirement.manage")
		}

		for _, permission := range denied {
			t.Run(permission+" "+tc.method+" "+tc.path, func(t *testing.T) {
				r := grcTestRouter([]string{permission})
				w := grcJSONRequest(r, tc.method, tc.path, tc.body)

				if w.Code != http.StatusForbidden {
					t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
				}
				if !strings.Contains(w.Body.String(), tc.permission) {
					t.Fatalf("expected required permission %q in body=%s", tc.permission, w.Body.String())
				}
			})
		}
	}
}

func TestGRCComplianceHandlersRejectInvalidRelationIDs(t *testing.T) {
	cases := []struct {
		method string
		path   string
		perm   string
	}{
		{http.MethodGet, "/api/grc/requirements?frameworkId=abc", "grc.requirement.read"},
		{http.MethodGet, "/api/grc/requirements?frameworkId=0", "grc.requirement.read"},
		{http.MethodGet, "/api/grc/requirements?parentRequirementId=-1", "grc.requirement.read"},
		{http.MethodGet, "/api/grc/controls?serviceId=abc", "grc.control.read"},
		{http.MethodGet, "/api/grc/controls?ownerProfileId=0", "grc.control.read"},
		{http.MethodGet, "/api/grc/control-implementations?controlId=abc", "grc.control.implementation.read"},
		{http.MethodGet, "/api/grc/control-implementations?assetId=0", "grc.control.implementation.read"},
		{http.MethodGet, "/api/grc/control-implementations?serviceId=x1", "grc.control.implementation.read"},
		{http.MethodGet, "/api/grc/control-implementations?ownerProfileId=1.5", "grc.control.implementation.read"},
		{http.MethodGet, "/api/grc/requirements/bad", "grc.requirement.read"},
		{http.MethodPut, "/api/grc/requirements/-3", "grc.requirement.manage"},
		{http.MethodDelete, "/api/grc/requirement-controls/bad/1", "grc.control.manage"},
		{http.MethodDelete, "/api/grc/requirement-controls/1/0", "grc.control.manage"},
		{http.MethodDelete, "/api/grc/control-risks/0/1", "grc.control.manage"},
		{http.MethodDelete, "/api/grc/control-risks/1/abc", "grc.control.manage"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := grcTestRouter([]string{tc.perm})
			w := grcJSONRequest(r, tc.method, tc.path, "{}")

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "invalide") {
				t.Fatalf("unexpected body=%s", w.Body.String())
			}
		})
	}
}

func TestGRCComplianceStaticRoutesAreNotCapturedByDynamicRoutes(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	r := grcServiceRouter(svc, []string{"*"})

	for _, path := range []string{
		"/api/grc/frameworks",
		"/api/grc/requirements",
		"/api/grc/controls",
		"/api/grc/control-implementations",
	} {
		w := grcRequest(r, http.MethodGet, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items"`) {
			t.Fatalf("GET %s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}

	// Malformed pagination keeps the GRC-01A defaults.
	w := grcRequest(r, http.MethodGet, "/api/grc/frameworks?page=abc&limit=zz")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"limit":20`) {
		t.Fatalf("malformed pagination: status=%d body=%s", w.Code, w.Body.String())
	}

	// Static segments remain ids for the GRC-01A dynamic routes.
	for _, path := range []string{
		"/api/grc/assets/frameworks",
		"/api/grc/risks/control-risks",
	} {
		w := grcRequest(r, http.MethodGet, path)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("GET %s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}

	// Relation collections only accept POST; their DELETE needs both ids.
	for _, path := range []string{
		"/api/grc/requirement-controls",
		"/api/grc/control-risks",
		"/api/grc/requirement-controls/1",
		"/api/grc/control-risks/1",
	} {
		w := grcRequest(r, http.MethodDelete, path)
		if w.Code != http.StatusNotFound {
			t.Fatalf("DELETE %s: status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestGRCComplianceAPIUsesAuthenticatedActor(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	r := grcServiceRouter(svc, []string{"grc.framework.manage", "grc.framework.read"})

	w := grcJSONRequest(r, http.MethodPost, "/api/grc/frameworks", `{
		"code": "fw-api",
		"name": "Référentiel API",
		"version": "1.0",
		"status": "draft",
		"createdBy": 99,
		"updatedBy": 99
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}

	var created Framework
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.CreatedBy != 7 || created.UpdatedBy != 7 || created.Code != "FW-API" {
		t.Fatalf("actor must come from auth context: %+v", created)
	}

	w = grcJSONRequest(r, http.MethodPost, "/api/grc/frameworks", `{"code":"FW-API","name":"Doublon","version":"1.0","status":"DRAFT"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST status=%d body=%s", w.Code, w.Body.String())
	}

	w = grcJSONRequest(r, http.MethodPost, "/api/grc/frameworks", `{"code":"FW-X","name":"X","version":"1.0","status":"COMPLIANT"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid status POST status=%d body=%s", w.Code, w.Body.String())
	}

	w = grcRequest(r, http.MethodGet, "/api/grc/frameworks/999999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET unknown status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGRCComplianceRelationAPILifecycle(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	r := grcServiceRouter(svc, []string{"grc.control.manage"})

	framework := createFramework(t, svc, "FW-API", "1.0")
	requirement := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	control := createControl(t, svc, f, "CTL-API", ControlStatusActive)
	asset := createRiskAsset(t, f)
	risk, err := svc.SaveRisk(0, 10, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}

	body := `{"requirementId":` + jsonUint(requirement.ID) + `,"controlId":` + jsonUint(control.ID) + `,"createdBy":99}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/requirement-controls", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("link status=%d body=%s", w.Code, w.Body.String())
	}
	var link RequirementControl
	if err = json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if link.CreatedBy != 7 {
		t.Fatalf("actor must come from auth context: %+v", link)
	}

	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/requirement-controls", body); w.Code != http.StatusConflict {
		t.Fatalf("duplicate link status=%d body=%s", w.Code, w.Body.String())
	}

	unlinkPath := "/api/grc/requirement-controls/" + jsonUint(requirement.ID) + "/" + jsonUint(control.ID)
	if w = grcRequest(r, http.MethodDelete, unlinkPath); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("unlink status=%d body=%s", w.Code, w.Body.String())
	}
	if w = grcRequest(r, http.MethodDelete, unlinkPath); w.Code != http.StatusNotFound {
		t.Fatalf("second unlink status=%d body=%s", w.Code, w.Body.String())
	}

	body = `{"controlId":` + jsonUint(control.ID) + `,"riskId":` + jsonUint(risk.ID) + `}`
	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/control-risks", body); w.Code != http.StatusCreated {
		t.Fatalf("control risk link status=%d body=%s", w.Code, w.Body.String())
	}
	if w = grcJSONRequest(r, http.MethodPost, "/api/grc/control-risks", body); w.Code != http.StatusConflict {
		t.Fatalf("duplicate control risk status=%d body=%s", w.Code, w.Body.String())
	}

	unlinkPath = "/api/grc/control-risks/" + jsonUint(control.ID) + "/" + jsonUint(risk.ID)
	if w = grcRequest(r, http.MethodDelete, unlinkPath); w.Code != http.StatusNoContent {
		t.Fatalf("control risk unlink status=%d body=%s", w.Code, w.Body.String())
	}

	if _, err = svc.FindRequirement(requirement.ID); err != nil {
		t.Fatalf("unlink must keep requirement: %v", err)
	}
	if _, err = svc.FindControl(control.ID); err != nil {
		t.Fatalf("unlink must keep control: %v", err)
	}
	if _, err = svc.FindRisk(risk.ID); err != nil {
		t.Fatalf("unlink must keep risk: %v", err)
	}
}

func jsonUint(value uint) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
