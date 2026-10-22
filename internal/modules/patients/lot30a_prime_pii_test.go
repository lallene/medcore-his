package patients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func piiRouter(svc Service, perms []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 3, "staff", perms)
		c.Next()
	})
	RegisterRoutesWithHandler(r.Group("/api"), NewHandler(svc))
	return r
}

func piiGet(t *testing.T, r *gin.Engine, path string) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, w.Code, w.Body.String())
	}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(body.Data, &out); err != nil {
		var list []map[string]any
		if err := json.Unmarshal(body.Data, &list); err != nil || len(list) == 0 {
			t.Fatalf("unexpected payload %s", body.Data)
		}
		return list[0]
	}
	return out
}

func seedInsuredPatient(t *testing.T) (Service, uint) {
	t.Helper()
	svc := NewService(newMemPatientRepo())
	p, err := svc.Create(CreatePatientRequest{Nom: "Assuré", Prenoms: "Test", IsAssure: true, TauxCouverture: 80, MatriculeAssure: "MAT-0001"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, p.ID
}

// A′-PII01 / A′-PII03: patients:read (incl. Patient360 shell) keeps the minimal indicator only.
func TestLOT30APrime_PatientInsuranceDetailsRedactedWithoutPermission(t *testing.T) {
	svc, id := seedInsuredPatient(t)
	for _, perms := range [][]string{{"patients:read"}, {"patients:read", "patients.360.read"}} {
		r := piiRouter(svc, perms)
		for _, path := range []string{fmt.Sprintf("/api/patients/%d", id), "/api/patients"} {
			got := piiGet(t, r, path)
			if got["isAssure"] != true {
				t.Fatalf("%v %s: minimal insured indicator must stay visible: %v", perms, path, got)
			}
			if got["matriculeAssure"] != "" || got["tauxCouverture"] != float64(0) || got["insuranceDetailsVisible"] != false {
				t.Fatalf("%v %s: insurance details must be redacted: %v", perms, path, got)
			}
		}
	}
}

// A′-PII02: insurance.coverage.read sees the member details.
func TestLOT30APrime_PatientInsuranceDetailsVisibleWithPermission(t *testing.T) {
	svc, id := seedInsuredPatient(t)
	for _, perms := range [][]string{{"patients:read", "insurance.coverage.read"}, {"*"}} {
		got := piiGet(t, piiRouter(svc, perms), fmt.Sprintf("/api/patients/%d", id))
		if got["matriculeAssure"] != "MAT-0001" || got["tauxCouverture"] != float64(80) || got["insuranceDetailsVisible"] != true {
			t.Fatalf("%v: authorized details expected: %v", perms, got)
		}
	}
}

// A redacted form (patients:update without insurance permission) must not wipe stored details.
func TestLOT30APrime_PatientUpdateWithoutPermissionPreservesInsuranceDetails(t *testing.T) {
	svc, id := seedInsuredPatient(t)
	r := piiRouter(svc, []string{"patients:read", "patients:update"})
	body, _ := json.Marshal(map[string]any{"nom": "Assuré", "prenoms": "Renommé", "isAssure": true, "tauxCouverture": 0, "matriculeAssure": ""})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/patients/%d", id), bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("update = %d %s", w.Code, w.Body.String())
	}
	stored, err := svc.FindByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Prenoms != "Renommé" || stored.TauxCouverture != 80 || stored.MatriculeAssure != "MAT-0001" {
		t.Fatalf("insurance details overwritten by redacted form: %#v", stored)
	}
}
