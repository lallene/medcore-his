package grc

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

var grc01DPermissions = []string{
	"grc.assessment.read", "grc.assessment.create", "grc.assessment.update", "grc.assessment.complete",
	"grc.audit.read", "grc.audit.create", "grc.audit.update", "grc.audit.complete",
	"grc.finding.read", "grc.finding.create", "grc.finding.update", "grc.finding.close", "grc.finding.accept",
}

// grc01DRoutesFailingBeforeService lists one request per 01D.06 route that the
// handler rejects with 400 before reaching the (nil) service.
func grc01DRoutesFailingBeforeService() []complianceRouteCase {
	return []complianceRouteCase{
		{http.MethodGet, "/api/grc/assessments?page=x", "", "grc.assessment.read"},
		{http.MethodGet, "/api/grc/assessments/bad", "", "grc.assessment.read"},
		{http.MethodPost, "/api/grc/assessments", malformedJSON, "grc.assessment.create"},
		{http.MethodPut, "/api/grc/assessments/bad", "{}", "grc.assessment.update"},
		{http.MethodPost, "/api/grc/assessments/bad/complete", "{}", "grc.assessment.complete"},
		{http.MethodGet, "/api/grc/assessments/bad/evidence", "", "grc.assessment.read"},
		{http.MethodPost, "/api/grc/assessments/bad/evidence", "{}", "grc.assessment.update"},
		{http.MethodDelete, "/api/grc/assessments/bad/evidence/1", "", "grc.assessment.update"},

		{http.MethodGet, "/api/grc/audits?page=x", "", "grc.audit.read"},
		{http.MethodGet, "/api/grc/audits/bad", "", "grc.audit.read"},
		{http.MethodPost, "/api/grc/audits", malformedJSON, "grc.audit.create"},
		{http.MethodPut, "/api/grc/audits/bad", "{}", "grc.audit.update"},
		{http.MethodPost, "/api/grc/audits/bad/complete", "", "grc.audit.complete"},

		{http.MethodGet, "/api/grc/findings?page=x", "", "grc.finding.read"},
		{http.MethodGet, "/api/grc/findings/bad", "", "grc.finding.read"},
		{http.MethodPost, "/api/grc/findings", malformedJSON, "grc.finding.create"},
		{http.MethodPut, "/api/grc/findings/bad", "{}", "grc.finding.update"},
		{http.MethodPost, "/api/grc/findings/bad/close", "", "grc.finding.close"},
		{http.MethodPost, "/api/grc/findings/bad/accept", "{}", "grc.finding.accept"},
	}
}

func TestGRC01DRoutesRegistered(t *testing.T) {
	r := grcTestRouter(nil)

	want := map[string]bool{
		"GET /api/grc/assessments":                             false,
		"GET /api/grc/assessments/:id":                         false,
		"POST /api/grc/assessments":                            false,
		"PUT /api/grc/assessments/:id":                         false,
		"POST /api/grc/assessments/:id/complete":               false,
		"GET /api/grc/assessments/:id/evidence":                false,
		"POST /api/grc/assessments/:id/evidence":               false,
		"DELETE /api/grc/assessments/:id/evidence/:evidenceId": false,
		"GET /api/grc/audits":                                  false,
		"GET /api/grc/audits/:id":                              false,
		"POST /api/grc/audits":                                 false,
		"PUT /api/grc/audits/:id":                              false,
		"POST /api/grc/audits/:id/complete":                    false,
		"GET /api/grc/findings":                                false,
		"GET /api/grc/findings/:id":                            false,
		"POST /api/grc/findings":                               false,
		"PUT /api/grc/findings/:id":                            false,
		"POST /api/grc/findings/:id/close":                     false,
		"POST /api/grc/findings/:id/accept":                    false,
	}

	seen := map[string]int{}
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		seen[key]++
		if _, ok := want[key]; ok {
			want[key] = true
		}
		if route.Method == http.MethodDelete && (strings.HasPrefix(route.Path, "/api/grc/assessments") ||
			strings.HasPrefix(route.Path, "/api/grc/audits") || strings.HasPrefix(route.Path, "/api/grc/findings")) &&
			route.Path != "/api/grc/assessments/:id/evidence/:evidenceId" {
			t.Errorf("unexpected entity DELETE route %s", route.Path)
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("route absente: %s", route)
		}
		if seen[route] > 1 {
			t.Errorf("route %s registered %d times", route, seen[route])
		}
	}

	wildcard := grcTestRouter([]string{"*"})
	for _, path := range []string{"/api/grc/assessments/1", "/api/grc/audits/1", "/api/grc/findings/1"} {
		if w := grcRequest(wildcard, http.MethodDelete, path); w.Code != http.StatusNotFound {
			t.Errorf("DELETE %s status=%d, want 404", path, w.Code)
		}
	}
}

func TestGRC01DRoutesRequireAuthentication(t *testing.T) {
	r := grcTestRouter(nil)
	for _, tc := range grc01DRoutesFailingBeforeService() {
		w := grcJSONRequest(r, tc.method, tc.path, tc.body)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status=%d, want 401", tc.method, tc.path, w.Code)
		}
	}
}

// Each route accepts exactly its permission (or the wildcard). Every other
// 01D permission, a read-only permission on a mutation route and a non-GRC
// permission are refused: no implication between permissions.
func TestGRC01DRoutesEnforceExactPermissions(t *testing.T) {
	candidates := append(append([]string{}, grc01DPermissions...), "*", "grc.control.manage", "patients:read")

	for _, tc := range grc01DRoutesFailingBeforeService() {
		for _, permission := range candidates {
			r := grcTestRouter([]string{permission})
			w := grcJSONRequest(r, tc.method, tc.path, tc.body)

			if permission == tc.permission || permission == "*" {
				if w.Code != http.StatusBadRequest {
					t.Errorf("%s with %s %s: expected handler 400, got %d %s", permission, tc.method, tc.path, w.Code, w.Body.String())
				}
				continue
			}
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), tc.permission) {
				t.Errorf("%s with %s %s: expected 403 requiring %s, got %d %s",
					permission, tc.method, tc.path, tc.permission, w.Code, w.Body.String())
			}
		}
	}
}

// The generic PUT never performs a sensitive transition, whatever the
// permission held: *.update cannot bypass *.complete, *.close or *.accept.
func TestGRC01DUpdateCannotBypassSensitiveTransitions(t *testing.T) {
	cases := []struct {
		path       string
		permission string
		body       string
	}{
		{"/api/grc/assessments/1", "grc.assessment.update", `{"status":"COMPLETED"}`},
		{"/api/grc/assessments/1", "grc.assessment.update", `{"status":" completed "}`},
		{"/api/grc/audits/1", "grc.audit.update", `{"status":"COMPLETED"}`},
		{"/api/grc/findings/1", "grc.finding.update", `{"status":"CLOSED"}`},
		{"/api/grc/findings/1", "grc.finding.update", `{"status":"ACCEPTED","closureNote":"x"}`},
		{"/api/grc/findings/1", "grc.finding.update", `{"status":"accepted"}`},
	}
	for _, tc := range cases {
		for _, permissions := range [][]string{{tc.permission}, {"*"}, grc01DPermissions} {
			r := grcTestRouter(permissions)
			w := grcJSONRequest(r, http.MethodPut, tc.path, tc.body)
			if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "route dédiée") {
				t.Errorf("PUT %s %s with %v: status=%d body=%s, want 403", tc.path, tc.body, permissions, w.Code, w.Body.String())
			}
		}
	}
}

// writerPermissions holds every 01D permission plus evidence and action reads.
var writerPermissions = append(append([]string{}, grc01DPermissions...), "grc.evidence.read", "grc.action.read")

func TestGRC01DAssessmentAPI(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := grcServiceRouter(f.svc, writerPermissions)

	create := `{"reference":"asm-api","title":"Évaluation API","controlImplementationId":` + jsonUint(f.implementation.ID) +
		`,"type":"design","status":"draft","id":999,"createdBy":99,"updatedBy":99,"completedAt":"2026-01-01T00:00:00Z",` +
		`"reassessmentDue":true,"hasInvalidEvidence":true}`
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments", create)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeJSON[Assessment](t, w.Body.Bytes())
	if created.ID == 999 || created.CreatedBy != 7 || created.UpdatedBy != 7 || created.CompletedAt != nil ||
		created.ReassessmentDue || created.HasInvalidEvidence || created.Reference != "ASM-API" {
		t.Fatalf("server fields must not be client-controlled: %+v", created)
	}
	path := "/api/grc/assessments/" + jsonUint(created.ID)

	for name, tc := range map[string]struct {
		method, path, body string
		status             int
	}{
		"malformed":          {http.MethodPost, "/api/grc/assessments", malformedJSON, http.StatusBadRequest},
		"unknown CI":         {http.MethodPost, "/api/grc/assessments", `{"reference":"ASM-X","title":"X","controlImplementationId":999999,"type":"DESIGN","status":"DRAFT"}`, http.StatusNotFound},
		"get":                {http.MethodGet, path, "", http.StatusOK},
		"get zero":           {http.MethodGet, "/api/grc/assessments/0", "", http.StatusBadRequest},
		"get negative":       {http.MethodGet, "/api/grc/assessments/-1", "", http.StatusBadRequest},
		"get overflow":       {http.MethodGet, "/api/grc/assessments/18446744073709551616", "", http.StatusBadRequest},
		"get unknown":        {http.MethodGet, "/api/grc/assessments/999999", "", http.StatusNotFound},
		"list bad enum":      {http.MethodGet, "/api/grc/assessments?result=NOT_ASSESSED", "", http.StatusBadRequest},
		"list bad id":        {http.MethodGet, "/api/grc/assessments?controlImplementationId=x", "", http.StatusBadRequest},
		"list bad date":      {http.MethodGet, "/api/grc/assessments?assessedAfter=2026-06-01", "", http.StatusBadRequest},
		"list bad flag":      {http.MethodGet, "/api/grc/assessments?reassessmentDue=yes", "", http.StatusBadRequest},
		"list inverted":      {http.MethodGet, "/api/grc/assessments?assessedAfter=2026-06-02T00:00:00Z&assessedBefore=2026-06-01T00:00:00Z", "", http.StatusBadRequest},
		"list bad page":      {http.MethodGet, "/api/grc/assessments?page=x", "", http.StatusBadRequest},
		"list bad limit":     {http.MethodGet, "/api/grc/assessments?limit=1.5", "", http.StatusBadRequest},
		"complete malformed": {http.MethodPost, path + "/complete", malformedJSON, http.StatusBadRequest},
	} {
		if w := grcJSONRequest(r, tc.method, tc.path, tc.body); w.Code != tc.status {
			t.Errorf("%s: status=%d want %d body=%s", name, w.Code, tc.status, w.Body.String())
		}
	}

	update := `{"reference":"ASM-API","title":"Évaluation API modifiée","controlImplementationId":` + jsonUint(f.implementation.ID) +
		`,"type":"DESIGN","status":"DRAFT"}`
	if w := grcJSONRequest(r, http.MethodPut, path, update); w.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", w.Code, w.Body.String())
	}

	w = grcRequest(r, http.MethodGet, "/api/grc/assessments?type=design&status=draft&search=modifi&page=1&limit=10")
	page := decodeJSON[AssessmentPage](t, w.Body.Bytes())
	if w.Code != http.StatusOK || page.Total != 1 || page.Items[0].ID != created.ID || page.Limit != 10 {
		t.Fatalf("LIST status=%d body=%s", w.Code, w.Body.String())
	}

	f.setClock(evidenceTestNow.Add(time.Hour))
	complete := `{"result":"effective","assessorProfileId":` + jsonUint(f.profile.ID) +
		`,"assessedAt":"2026-06-01T11:00:00Z","completedAt":"2020-01-01T00:00:00Z"}`
	w = grcJSONRequest(r, http.MethodPost, path+"/complete", complete)
	if w.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", w.Code, w.Body.String())
	}
	completed := decodeJSON[Assessment](t, w.Body.Bytes())
	if completed.Status != AssessmentStatusCompleted || completed.CompletedAt == nil ||
		!completed.CompletedAt.Equal(evidenceTestNow.Add(time.Hour)) || completed.Title != "Évaluation API modifiée" {
		t.Fatalf("completion must keep the draft and set CompletedAt from the clock: %+v", completed)
	}

	if w := grcJSONRequest(r, http.MethodPost, path+"/complete", "{}"); w.Code != http.StatusConflict {
		t.Fatalf("second completion status=%d", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPut, path, update); w.Code != http.StatusConflict {
		t.Fatalf("update of a completed assessment status=%d", w.Code)
	}
	if w := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments/999999/complete", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("complete unknown status=%d", w.Code)
	}
}

func TestGRC01DAssessmentEvidenceAPI(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := grcServiceRouter(f.svc, writerPermissions)
	readOnly := grcServiceRouter(f.svc, []string{"grc.assessment.read", "grc.audit.read", "grc.finding.read"})

	assessment := f.createAssessment(t, f.assessmentRequest("ASM-EVD-API"))
	valid := f.validEvidence(t, "EVD-API-OK", f.implementation.ID)
	foreign := f.validEvidence(t, "EVD-API-OTHER", f.other.ID)
	base := "/api/grc/assessments/" + jsonUint(assessment.ID) + "/evidence"
	link := func(router http.Handler, evidenceID uint) int {
		return grcJSONRequest(router, http.MethodPost, base, `{"evidenceId":`+jsonUint(evidenceID)+`,"assessmentId":999,"createdBy":99}`).Code
	}

	if code := link(readOnly, valid.ID); code != http.StatusForbidden {
		t.Fatalf("read-only link status=%d", code)
	}
	w := grcJSONRequest(r, http.MethodPost, base, `{"evidenceId":`+jsonUint(valid.ID)+`,"assessmentId":999,"createdBy":99}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("link status=%d body=%s", w.Code, w.Body.String())
	}
	created := decodeJSON[AssessmentEvidence](t, w.Body.Bytes())
	if created.AssessmentID != assessment.ID || created.CreatedBy != 7 {
		t.Fatalf("assessment comes from the path and actor from the context: %+v", created)
	}

	for name, tc := range map[string]struct {
		evidenceID uint
		status     int
	}{
		"unknown evidence": {999999, http.StatusNotFound},
		"incompatible":     {foreign.ID, http.StatusConflict},
		"duplicate":        {valid.ID, http.StatusConflict},
	} {
		if code := link(r, tc.evidenceID); code != tc.status {
			t.Errorf("%s: status=%d want %d", name, code, tc.status)
		}
	}
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments/999999/evidence", `{"evidenceId":1}`).Code; code != http.StatusNotFound {
		t.Errorf("unknown assessment status=%d", code)
	}

	for _, router := range []http.Handler{r, readOnly} {
		w := grcRequest(router, http.MethodGet, base)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items"`) || !strings.Contains(w.Body.String(), "EVD-API-OK") {
			t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
		}
	}

	unlink := base + "/" + jsonUint(valid.ID)
	if code := grcRequest(readOnly, http.MethodDelete, unlink).Code; code != http.StatusForbidden {
		t.Fatalf("read-only unlink status=%d", code)
	}
	if w := grcRequest(r, http.MethodDelete, unlink); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("unlink status=%d", w.Code)
	}
	if code := grcRequest(r, http.MethodDelete, unlink).Code; code != http.StatusNotFound {
		t.Fatalf("second unlink status=%d", code)
	}
	if code := grcRequest(r, http.MethodDelete, base+"/abc").Code; code != http.StatusBadRequest {
		t.Fatalf("invalid evidence id status=%d", code)
	}
	if _, err := f.svc.FindEvidence(valid.ID); err != nil {
		t.Fatalf("unlink must keep the evidence: %v", err)
	}

	// A completed assessment freezes its evidence.
	if code := link(r, valid.ID); code != http.StatusCreated {
		t.Fatalf("relink status=%d", code)
	}
	completion := `{"result":"EFFECTIVE","assessorProfileId":` + jsonUint(f.profile.ID) + `,"assessedAt":"2026-06-01T11:00:00Z"}`
	if code := grcJSONRequest(r, http.MethodPost, "/api/grc/assessments/"+jsonUint(assessment.ID)+"/complete", completion).Code; code != http.StatusOK {
		t.Fatalf("complete status=%d", code)
	}
	other := f.validEvidence(t, "EVD-API-LATE", f.implementation.ID)
	if code := link(r, other.ID); code != http.StatusConflict {
		t.Fatalf("link on completed assessment status=%d", code)
	}
	if code := grcRequest(r, http.MethodDelete, unlink).Code; code != http.StatusConflict {
		t.Fatalf("unlink on completed assessment status=%d", code)
	}
}

func TestGRC01DAuditEngagementAPI(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := grcServiceRouter(f.svc, writerPermissions)

	w := grcJSONRequest(r, http.MethodPost, "/api/grc/audits",
		`{"reference":"aud-api","title":"Audit API","type":"internal","status":"planned","completedAt":"2020-01-01T00:00:00Z","createdBy":99}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST planned status=%d body=%s", w.Code, w.Body.String())
	}
	planned := decodeJSON[AuditEngagement](t, w.Body.Bytes())
	if planned.CompletedAt != nil || planned.StartedAt != nil || planned.CreatedBy != 7 {
		t.Fatalf("server fields must not be client-controlled: %+v", planned)
	}
	path := "/api/grc/audits/" + jsonUint(planned.ID)

	w = grcJSONRequest(r, http.MethodPost, "/api/grc/audits",
		`{"reference":"AUD-API-RUN","title":"Audit en cours","type":"EXTERNAL","status":"IN_PROGRESS"}`)
	running := decodeJSON[AuditEngagement](t, w.Body.Bytes())
	if w.Code != http.StatusCreated || running.StartedAt == nil || !running.StartedAt.Equal(evidenceTestNow) {
		t.Fatalf("POST in progress status=%d body=%s", w.Code, w.Body.String())
	}

	if code := grcJSONRequest(r, http.MethodPost, path+"/complete", "").Code; code != http.StatusConflict {
		t.Fatalf("PLANNED -> COMPLETED status=%d", code)
	}

	start := `{"reference":"AUD-API","title":"Audit API","type":"INTERNAL","status":"IN_PROGRESS"}`
	if w := grcJSONRequest(r, http.MethodPut, path, start); w.Code != http.StatusOK {
		t.Fatalf("PLANNED -> IN_PROGRESS status=%d body=%s", w.Code, w.Body.String())
	}

	f.setClock(evidenceTestNow.Add(3 * time.Hour))
	w = grcJSONRequest(r, http.MethodPost, path+"/complete", "")
	if w.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", w.Code, w.Body.String())
	}
	completed := decodeJSON[AuditEngagement](t, w.Body.Bytes())
	if completed.Status != AuditEngagementStatusCompleted || completed.CompletedAt == nil ||
		!completed.CompletedAt.Equal(evidenceTestNow.Add(3*time.Hour)) || !completed.StartedAt.Equal(evidenceTestNow) {
		t.Fatalf("unexpected completed audit: %+v", completed)
	}
	if code := grcJSONRequest(r, http.MethodPost, path+"/complete", "").Code; code != http.StatusConflict {
		t.Fatalf("second completion status=%d", code)
	}
	if code := grcJSONRequest(r, http.MethodPut, path, start).Code; code != http.StatusConflict {
		t.Fatalf("update of a completed audit status=%d", code)
	}

	for name, tc := range map[string]struct {
		path   string
		status int
	}{
		"get":           {path, http.StatusOK},
		"get unknown":   {"/api/grc/audits/999999", http.StatusNotFound},
		"get zero":      {"/api/grc/audits/0", http.StatusBadRequest},
		"list":          {"/api/grc/audits?type=external&status=in_progress", http.StatusOK},
		"bad status":    {"/api/grc/audits?status=DRAFT", http.StatusBadRequest},
		"bad lead":      {"/api/grc/audits?leadAuditorProfileId=0", http.StatusBadRequest},
		"bad date":      {"/api/grc/audits?plannedStartAfter=demain", http.StatusBadRequest},
		"inverted":      {"/api/grc/audits?plannedStartAfter=2026-06-02T00:00:00Z&plannedStartBefore=2026-06-01T00:00:00Z", http.StatusBadRequest},
		"bad page size": {"/api/grc/audits?limit=x", http.StatusBadRequest},
	} {
		if w := grcRequest(r, http.MethodGet, tc.path); w.Code != tc.status {
			t.Errorf("%s: status=%d want %d body=%s", name, w.Code, tc.status, w.Body.String())
		}
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/audits?type=external")
	if page := decodeJSON[AuditEngagementPage](t, w.Body.Bytes()); page.Total != 1 || page.Items[0].ID != running.ID {
		t.Fatalf("filtered list: %+v", page)
	}
}

func TestGRC01DFindingAPI(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := grcServiceRouter(f.svc, writerPermissions)
	audit := f.inProgressAudit(t, "AUD-FND-API")
	assessment := f.completedAssessment(t, "ASM-FND-API")

	create := func(reference, source string) (int, Finding) {
		w := grcJSONRequest(r, http.MethodPost, "/api/grc/findings", `{"reference":"`+reference+`","title":"Constat API",`+source+
			`,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"OPEN",`+
			`"resolvedAt":"2020-01-01T00:00:00Z","closedAt":"2020-01-01T00:00:00Z","createdBy":99,"overdue":true}`)
		if w.Code != http.StatusCreated {
			return w.Code, Finding{}
		}
		return w.Code, decodeJSON[Finding](t, w.Body.Bytes())
	}

	code, fromAudit := create("fnd-api-audit", `"auditEngagementId":`+jsonUint(audit.ID))
	if code != http.StatusCreated || fromAudit.ResolvedAt != nil || fromAudit.ClosedAt != nil || fromAudit.CreatedBy != 7 {
		t.Fatalf("server fields must not be client-controlled: %d %+v", code, fromAudit)
	}
	code, fromAssessment := create("FND-API-ASM", `"assessmentId":`+jsonUint(assessment.ID))
	if code != http.StatusCreated || fromAssessment.ControlImplementationID == nil {
		t.Fatalf("finding from assessment: %d %+v", code, fromAssessment)
	}
	if code, _ := create("FND-API-NONE", `"riskId":`+jsonUint(f.risk.ID)); code != http.StatusBadRequest {
		t.Fatalf("no source status=%d", code)
	}
	if code, _ := create("FND-API-UNKNOWN", `"auditEngagementId":999999`); code != http.StatusNotFound {
		t.Fatalf("unknown source status=%d", code)
	}

	path := "/api/grc/findings/" + jsonUint(fromAudit.ID)
	body := func(status, extra string) string {
		return `{"reference":"FND-API-AUDIT","title":"Constat API","auditEngagementId":` + jsonUint(audit.ID) +
			`,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"` + status + `"` + extra + `}`
	}

	f.setClock(evidenceTestNow.Add(time.Hour))
	w := grcJSONRequest(r, http.MethodPut, path, body("RESOLVED", `,"resolvedAt":"2020-01-01T00:00:00Z"`))
	if w.Code != http.StatusOK {
		t.Fatalf("OPEN -> RESOLVED status=%d body=%s", w.Code, w.Body.String())
	}
	if got := decodeJSON[Finding](t, w.Body.Bytes()); got.ResolvedAt == nil || !got.ResolvedAt.Equal(evidenceTestNow.Add(time.Hour)) {
		t.Fatalf("ResolvedAt must come from the clock: %+v", got.ResolvedAt)
	}

	other := f.inProgressAudit(t, "AUD-FND-OTHER")
	moved := `{"reference":"FND-API-AUDIT","title":"Constat API","auditEngagementId":` + jsonUint(other.ID) +
		`,"category":"CONTROL_DEFICIENCY","severity":"HIGH","status":"RESOLVED"}`
	if code := grcJSONRequest(r, http.MethodPut, path, moved).Code; code != http.StatusConflict {
		t.Fatalf("source change status=%d", code)
	}

	action := RemediationAction{
		Reference: "ACT-FND-API", Title: "Action", FindingID: &fromAudit.ID,
		Priority: RemediationActionPriorityHigh, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := f.db.Create(&action).Error; err != nil {
		t.Fatal(err)
	}
	if code := grcJSONRequest(r, http.MethodPost, path+"/close", "").Code; code != http.StatusConflict {
		t.Fatalf("close with an open action status=%d", code)
	}
	if err := f.db.Model(&RemediationAction{}).Where("id = ?", action.ID).
		Updates(map[string]any{"status": RemediationActionStatusCancelled}).Error; err != nil {
		t.Fatal(err)
	}
	f.setClock(evidenceTestNow.Add(2 * time.Hour))
	w = grcJSONRequest(r, http.MethodPost, path+"/close", "")
	if w.Code != http.StatusOK {
		t.Fatalf("close status=%d body=%s", w.Code, w.Body.String())
	}
	if closed := decodeJSON[Finding](t, w.Body.Bytes()); closed.ClosedAt == nil || !closed.ClosedAt.Equal(evidenceTestNow.Add(2*time.Hour)) {
		t.Fatalf("ClosedAt must come from the clock: %+v", closed)
	}
	if code := grcJSONRequest(r, http.MethodPut, path, body("OPEN", "")).Code; code != http.StatusConflict {
		t.Fatalf("update of a closed finding status=%d", code)
	}

	acceptPath := "/api/grc/findings/" + jsonUint(fromAssessment.ID) + "/accept"
	if code := grcJSONRequest(r, http.MethodPost, acceptPath, `{"closureNote":"  "}`).Code; code != http.StatusBadRequest {
		t.Fatalf("accept without justification status=%d", code)
	}
	w = grcJSONRequest(r, http.MethodPost, acceptPath, `{"closureNote":"Risque accepté","closedAt":"2020-01-01T00:00:00Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", w.Code, w.Body.String())
	}
	if accepted := decodeJSON[Finding](t, w.Body.Bytes()); accepted.Status != FindingStatusAccepted ||
		accepted.ClosedAt == nil || !accepted.ClosedAt.Equal(evidenceTestNow.Add(2*time.Hour)) {
		t.Fatalf("unexpected accepted finding: %+v", accepted)
	}
	if code := grcJSONRequest(r, http.MethodPost, acceptPath, `{"closureNote":"Encore"}`).Code; code != http.StatusConflict {
		t.Fatalf("accept of an accepted finding status=%d", code)
	}

	for name, tc := range map[string]struct {
		path   string
		status int
	}{
		"get":          {path, http.StatusOK},
		"get unknown":  {"/api/grc/findings/999999", http.StatusNotFound},
		"get bad id":   {"/api/grc/findings/abc", http.StatusBadRequest},
		"list":         {"/api/grc/findings?status=closed&auditEngagementId=" + jsonUint(audit.ID), http.StatusOK},
		"bad severity": {"/api/grc/findings?severity=INFO", http.StatusBadRequest},
		"bad id":       {"/api/grc/findings?riskId=-1", http.StatusBadRequest},
		"bad overdue":  {"/api/grc/findings?overdue=1", http.StatusBadRequest},
		"bad date":     {"/api/grc/findings?dueBefore=2026-06-01", http.StatusBadRequest},
	} {
		if w := grcRequest(r, http.MethodGet, tc.path); w.Code != tc.status {
			t.Errorf("%s: status=%d want %d body=%s", name, w.Code, tc.status, w.Body.String())
		}
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/findings?status=accepted")
	if page := decodeJSON[FindingPage](t, w.Body.Bytes()); page.Total != 1 || page.Items[0].ID != fromAssessment.ID {
		t.Fatalf("filtered list: %+v", page)
	}
}

// With the real service, refused PUT bypasses leave the database unchanged.
func TestGRC01DUpdateBypassLeavesDataUnchanged(t *testing.T) {
	f := newGRC01DServiceFixture(t)
	r := grcServiceRouter(f.svc, []string{"*"})

	assessment := f.createAssessment(t, f.assessmentRequest("ASM-BYPASS"))
	completion := f.completionOf(f.assessmentRequest("ASM-BYPASS"))
	body := `{"reference":"ASM-BYPASS","title":"X","controlImplementationId":` + jsonUint(f.implementation.ID) +
		`,"type":"OPERATING_EFFECTIVENESS","status":"COMPLETED","result":"EFFECTIVE","assessorProfileId":` +
		jsonUint(*completion.AssessorProfileID) + `,"assessedAt":"2026-06-01T11:00:00Z"}`
	if code := grcJSONRequest(r, http.MethodPut, "/api/grc/assessments/"+jsonUint(assessment.ID), body).Code; code != http.StatusForbidden {
		t.Fatalf("PUT COMPLETED status=%d", code)
	}
	if got, _ := f.svc.FindAssessment(assessment.ID); got.Status != AssessmentStatusDraft || got.CompletedAt != nil {
		t.Fatalf("refused bypass must not complete the assessment: %+v", got)
	}

	audit := f.inProgressAudit(t, "AUD-BYPASS")
	if code := grcJSONRequest(r, http.MethodPut, "/api/grc/audits/"+jsonUint(audit.ID),
		`{"reference":"AUD-BYPASS","title":"X","type":"INTERNAL","status":"COMPLETED"}`).Code; code != http.StatusForbidden {
		t.Fatalf("PUT audit COMPLETED status=%d", code)
	}
	if got, _ := f.svc.FindAuditEngagement(audit.ID); got.Status != AuditEngagementStatusInProgress || got.CompletedAt != nil {
		t.Fatalf("refused bypass must not complete the audit: %+v", got)
	}

	finding := f.createFinding(t, f.findingRequest("FND-BYPASS", audit.ID))
	for _, status := range []string{"CLOSED", "ACCEPTED"} {
		code := grcJSONRequest(r, http.MethodPut, "/api/grc/findings/"+jsonUint(finding.ID),
			`{"reference":"FND-BYPASS","title":"X","auditEngagementId":`+jsonUint(audit.ID)+
				`,"category":"OBSERVATION","severity":"LOW","status":"`+status+`","closureNote":"x"}`).Code
		if code != http.StatusForbidden {
			t.Fatalf("PUT finding %s status=%d", status, code)
		}
	}
	if got, _ := f.svc.FindFinding(finding.ID); got.Status != FindingStatusOpen || got.ClosedAt != nil {
		t.Fatalf("refused bypass must not close the finding: %+v", got)
	}
}
