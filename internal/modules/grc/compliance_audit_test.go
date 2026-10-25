package grc

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func assertAuditEvent(t *testing.T, bus *captureBus, index int, module, action string, recordID, actor uint) (map[string]any, map[string]any) {
	t.Helper()

	event := requireAuditEvent(t, bus, index)
	if event.Module != module || event.Action != action || event.RecordID != recordID {
		t.Fatalf("event %d = %s/%s/%d, want %s/%s/%d",
			index, event.Module, event.Action, event.RecordID, module, action, recordID)
	}
	if event.UserID == nil || *event.UserID != actor {
		t.Fatalf("event %d user=%v want=%d", index, event.UserID, actor)
	}

	var oldValue, newValue map[string]any
	if event.OldValue != "" {
		oldValue = decodeAuditJSON(t, event.OldValue)
	}
	if event.NewValue != "" {
		newValue = decodeAuditJSON(t, event.NewValue)
	}
	return oldValue, newValue
}

// assertSnapshotKeys fails when a snapshot holds a key outside allowed or one
// of the forbidden free-text keys.
func assertSnapshotKeys(t *testing.T, snapshot map[string]any, allowed []string, forbidden ...string) {
	t.Helper()

	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key := range snapshot {
		if !allowedSet[key] {
			t.Errorf("audit snapshot exposes unexpected key %q: %#v", key, snapshot)
		}
	}
	for _, key := range forbidden {
		if _, exists := snapshot[key]; exists {
			t.Errorf("audit snapshot must not expose %q", key)
		}
	}
}

func assertRawAuditExcludes(t *testing.T, bus *captureBus, secrets ...string) {
	t.Helper()

	for i := range bus.events {
		event := requireAuditEvent(t, bus, i)
		for _, secret := range secrets {
			if strings.Contains(event.OldValue, secret) || strings.Contains(event.NewValue, secret) {
				t.Errorf("audit event %d leaks %q", i, secret)
			}
		}
	}
}

func assertNoAudit(t *testing.T, bus *captureBus, context string) {
	t.Helper()

	if len(bus.events) != 0 {
		t.Fatalf("%s published %d audit events", context, len(bus.events))
	}
}

var (
	frameworkAuditKeys             = []string{"id", "code", "name", "version", "status", "publishedAt"}
	requirementAuditKeys           = []string{"id", "frameworkId", "code", "title", "parentRequirementId", "status"}
	controlAuditKeys               = []string{"id", "code", "name", "category", "ownerProfileId", "owningServiceId", "status"}
	controlImplementationAuditKeys = []string{"id", "controlId", "assetId", "owningServiceId", "ownerProfileId", "status", "implementedAt", "lastReviewedAt"}
)

func TestComplianceAuditSnapshotTypesHaveNoFreeTextFields(t *testing.T) {
	forbidden := map[string]bool{
		"Description":           true,
		"Source":                true,
		"ImplementationDetails": true,
		"CreatedBy":             true,
		"UpdatedBy":             true,
	}

	for _, snapshot := range []any{
		frameworkAuditSnapshot{},
		requirementAuditSnapshot{},
		controlAuditSnapshot{},
		controlImplementationAuditSnapshot{},
		requirementControlAuditSnapshot{},
		controlRiskAuditSnapshot{},
	} {
		typ := reflect.TypeOf(snapshot)
		for i := 0; i < typ.NumField(); i++ {
			if field := typ.Field(i); forbidden[field.Name] {
				t.Errorf("%s must not declare %s", typ.Name(), field.Name)
			}
		}
	}
}

func TestFrameworkAuditCreateAndUpdate(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	bus := withCaptureBus(t)

	published := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	req := validFrameworkRequest()
	req.Description = "DESCRIPTION-SECRETE"
	req.Source = "SOURCE-SECRETE"
	req.PublishedAt = &published

	created, err := svc.SaveFramework(0, 41, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 1 {
		t.Fatalf("create audit events=%d want=1", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCFramework, auditActionCreate, created.ID, 41)
	if oldValue != nil {
		t.Fatalf("create old value=%#v want empty", oldValue)
	}
	assertSnapshotKeys(t, newValue, frameworkAuditKeys, "description", "source", "createdBy", "updatedBy")
	if newValue["id"] != float64(created.ID) || newValue["code"] != "FW-TEST" || newValue["version"] != "1.0" {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}
	if newValue["publishedAt"] == nil {
		t.Fatalf("publishedAt missing from snapshot: %#v", newValue)
	}

	update := req
	update.Name = "Référentiel renommé"
	update.Status = FrameworkStatusActive

	if _, err = svc.SaveFramework(created.ID, 42, update); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("update audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue = assertAuditEvent(t, bus, 1, auditModuleGRCFramework, auditActionUpdate, created.ID, 42)
	assertSnapshotKeys(t, oldValue, frameworkAuditKeys, "description", "source")
	assertSnapshotKeys(t, newValue, frameworkAuditKeys, "description", "source")
	if oldValue["name"] != "Référentiel de test" || oldValue["status"] != FrameworkStatusDraft {
		t.Fatalf("unexpected old snapshot: %#v", oldValue)
	}
	if newValue["name"] != "Référentiel renommé" || newValue["status"] != FrameworkStatusActive {
		t.Fatalf("unexpected new snapshot: %#v", newValue)
	}

	assertRawAuditExcludes(t, bus, "DESCRIPTION-SECRETE", "SOURCE-SECRETE")
}

func TestFrameworkAuditSkipsRejectedMutations(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	createFramework(t, svc, "FW-DUP", "1.0")
	bus := withCaptureBus(t)

	invalid := validFrameworkRequest()
	invalid.Name = " "
	if _, err := svc.SaveFramework(0, 41, invalid); err == nil {
		t.Fatal("expected validation error")
	}
	assertNoAudit(t, bus, "invalid framework")

	duplicate := validFrameworkRequest()
	duplicate.Code = "FW-DUP"
	duplicate.Version = "1.0"
	if _, err := svc.SaveFramework(0, 41, duplicate); err == nil {
		t.Fatal("expected duplicate error")
	}
	assertNoAudit(t, bus, "duplicate framework")

	if _, err := svc.SaveFramework(999999, 41, validFrameworkRequest()); err == nil {
		t.Fatal("expected not found error")
	}
	assertNoAudit(t, bus, "unknown framework update")
}

func TestRequirementAuditCreateAndUpdate(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	framework := createFramework(t, svc, "FW-REQ", "1.0")
	parent := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	bus := withCaptureBus(t)

	req := RequirementRequest{
		FrameworkID:         framework.ID,
		Code:                "REQ-1.1",
		Title:               "Sous-exigence",
		Description:         "DESCRIPTION-EXIGENCE",
		ParentRequirementID: &parent.ID,
		Status:              RequirementStatusActive,
	}
	created, err := svc.SaveRequirement(0, 51, req)
	if err != nil {
		t.Fatal(err)
	}

	_, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCRequirement, auditActionCreate, created.ID, 51)
	assertSnapshotKeys(t, newValue, requirementAuditKeys, "description")
	if newValue["frameworkId"] != float64(framework.ID) || newValue["parentRequirementId"] != float64(parent.ID) {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}

	req.Title = "Sous-exigence modifiée"
	req.Status = RequirementStatusInactive
	if _, err = svc.SaveRequirement(created.ID, 52, req); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 1, auditModuleGRCRequirement, auditActionUpdate, created.ID, 52)
	assertSnapshotKeys(t, oldValue, requirementAuditKeys, "description")
	if oldValue["title"] != "Sous-exigence" || newValue["title"] != "Sous-exigence modifiée" ||
		newValue["status"] != RequirementStatusInactive {
		t.Fatalf("unexpected update snapshots: old=%#v new=%#v", oldValue, newValue)
	}

	assertRawAuditExcludes(t, bus, "DESCRIPTION-EXIGENCE")
}

func TestRequirementAuditSkipsRejectedMutations(t *testing.T) {
	_, svc := newComplianceServiceFixture(t)
	framework := createFramework(t, svc, "FW-ONE", "1.0")
	other := createFramework(t, svc, "FW-TWO", "1.0")
	foreign := createRequirement(t, svc, other.ID, "REQ-X", nil)
	a := createRequirement(t, svc, framework.ID, "REQ-A", nil)
	b := createRequirement(t, svc, framework.ID, "REQ-B", &a.ID)
	createRequirement(t, svc, framework.ID, "REQ-DUP", nil)
	bus := withCaptureBus(t)

	cases := map[string]func() error{
		"unknown framework": func() error {
			_, err := svc.SaveRequirement(0, 10, RequirementRequest{FrameworkID: 999999, Code: "R", Title: "T", Status: "ACTIVE"})
			return err
		},
		"parent from other framework": func() error {
			_, err := svc.SaveRequirement(0, 10, RequirementRequest{FrameworkID: framework.ID, Code: "R", Title: "T", ParentRequirementID: &foreign.ID, Status: "ACTIVE"})
			return err
		},
		"cycle": func() error {
			_, err := svc.SaveRequirement(a.ID, 10, RequirementRequest{FrameworkID: framework.ID, Code: a.Code, Title: a.Title, ParentRequirementID: &b.ID, Status: "ACTIVE"})
			return err
		},
		"duplicate": func() error {
			_, err := svc.SaveRequirement(0, 10, RequirementRequest{FrameworkID: framework.ID, Code: "REQ-DUP", Title: "T", Status: "ACTIVE"})
			return err
		},
	}

	for name, run := range cases {
		if err := run(); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		assertNoAudit(t, bus, name)
	}
}

func TestControlAuditCreateAndUpdate(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	bus := withCaptureBus(t)

	req := validControlRequest(f)
	req.Description = "DESCRIPTION-CONTROLE"
	created, err := svc.SaveControl(0, 61, req)
	if err != nil {
		t.Fatal(err)
	}

	_, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCControl, auditActionCreate, created.ID, 61)
	assertSnapshotKeys(t, newValue, controlAuditKeys, "description")
	if newValue["code"] != "CTL-001" ||
		newValue["category"] != "Continuité" ||
		newValue["ownerProfileId"] != float64(f.profile.ID) ||
		newValue["owningServiceId"] != float64(f.service.ID) {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}

	req.Status = ControlStatusRetired
	if _, err = svc.SaveControl(created.ID, 62, req); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 1, auditModuleGRCControl, auditActionUpdate, created.ID, 62)
	assertSnapshotKeys(t, oldValue, controlAuditKeys, "description")
	if oldValue["status"] != ControlStatusActive || newValue["status"] != ControlStatusRetired {
		t.Fatalf("unexpected update snapshots: old=%#v new=%#v", oldValue, newValue)
	}

	assertRawAuditExcludes(t, bus, "DESCRIPTION-CONTROLE")
}

func TestControlAuditSkipsRejectedMutations(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	createControl(t, svc, f, "CTL-DUP", ControlStatusActive)
	bus := withCaptureBus(t)

	cases := map[string]func(*ControlRequest){
		"inactive owner":   func(r *ControlRequest) { r.OwnerProfileID = &f.inactiveProfile.ID },
		"inactive service": func(r *ControlRequest) { r.OwningServiceID = &f.inactiveService.ID },
		"invalid status":   func(r *ControlRequest) { r.Status = "ARCHIVED" },
		"duplicate code":   func(r *ControlRequest) { r.Code = "CTL-DUP" },
	}

	for name, mutate := range cases {
		r := validControlRequest(f)
		mutate(&r)
		if _, err := svc.SaveControl(0, 10, r); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		assertNoAudit(t, bus, name)
	}
}

func TestControlImplementationAuditCreateAndUpdate(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)
	bus := withCaptureBus(t)

	implemented := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	req := ControlImplementationRequest{
		ControlID:             control.ID,
		AssetID:               asset.ID,
		OwningServiceID:       &f.service.ID,
		OwnerProfileID:        &f.profile.ID,
		Status:                ControlImplementationStatusImplemented,
		ImplementationDetails: "IP 10.0.0.5 compte svc_backup mot de passe en coffre",
		ImplementedAt:         &implemented,
	}
	created, err := svc.SaveControlImplementation(0, 71, req)
	if err != nil {
		t.Fatal(err)
	}

	_, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCControlImplementation, auditActionCreate, created.ID, 71)
	assertSnapshotKeys(t, newValue, controlImplementationAuditKeys, "implementationDetails")
	if newValue["controlId"] != float64(control.ID) || newValue["assetId"] != float64(asset.ID) ||
		newValue["implementedAt"] == nil {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}

	reviewed := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	req.Status = ControlImplementationStatusPartial
	req.ImplementationDetails = "Nouveau détail technique sensible"
	req.LastReviewedAt = &reviewed
	if _, err = svc.SaveControlImplementation(created.ID, 72, req); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 1, auditModuleGRCControlImplementation, auditActionUpdate, created.ID, 72)
	assertSnapshotKeys(t, oldValue, controlImplementationAuditKeys, "implementationDetails")
	assertSnapshotKeys(t, newValue, controlImplementationAuditKeys, "implementationDetails")
	if oldValue["status"] != ControlImplementationStatusImplemented ||
		newValue["status"] != ControlImplementationStatusPartial ||
		newValue["lastReviewedAt"] == nil {
		t.Fatalf("unexpected update snapshots: old=%#v new=%#v", oldValue, newValue)
	}

	assertRawAuditExcludes(t, bus, "10.0.0.5", "svc_backup", "mot de passe", "Nouveau détail")
}

func TestControlImplementationAuditSkipsRejectedMutations(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	retired := createControl(t, svc, f, "CTL-RETIRED", ControlStatusRetired)
	asset := createComplianceAsset(t, svc, f, "AST-1", AssetStatusActive)
	inactiveAsset := createComplianceAsset(t, svc, f, "AST-INACTIVE", AssetStatusInactive)
	if _, err := svc.SaveControlImplementation(0, 10, ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: ControlImplementationStatusPlanned,
	}); err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)

	cases := map[string]ControlImplementationRequest{
		"inactive asset":  {ControlID: control.ID, AssetID: inactiveAsset.ID, Status: "PLANNED"},
		"retired control": {ControlID: retired.ID, AssetID: asset.ID, Status: "PLANNED"},
		"invalid status":  {ControlID: control.ID, AssetID: asset.ID, Status: "COMPLIANT"},
		"duplicate":       {ControlID: control.ID, AssetID: asset.ID, Status: "PLANNED"},
	}

	for name, req := range cases {
		if _, err := svc.SaveControlImplementation(0, 10, req); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		assertNoAudit(t, bus, name)
	}
}

func TestRequirementControlAuditLinkAndUnlink(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	framework := createFramework(t, svc, "FW-LINK", "1.0")
	requirement := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	bus := withCaptureBus(t)

	req := RequirementControlRequest{RequirementID: requirement.ID, ControlID: control.ID}
	link, err := svc.LinkRequirementControl(81, req)
	if err != nil {
		t.Fatal(err)
	}

	oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCRequirementControl, auditActionLink, link.ID, 81)
	if oldValue != nil {
		t.Fatalf("link old value=%#v want empty", oldValue)
	}
	assertSnapshotKeys(t, newValue, []string{"id", "requirementId", "controlId"})
	if newValue["id"] != float64(link.ID) ||
		newValue["requirementId"] != float64(requirement.ID) ||
		newValue["controlId"] != float64(control.ID) {
		t.Fatalf("unexpected link snapshot: %#v", newValue)
	}

	if _, err = svc.LinkRequirementControl(81, req); err == nil {
		t.Fatal("expected duplicate link error")
	}
	if _, err = svc.LinkRequirementControl(81, RequirementControlRequest{RequirementID: 999999, ControlID: control.ID}); err == nil {
		t.Fatal("expected unknown requirement error")
	}
	if len(bus.events) != 1 {
		t.Fatalf("rejected links published audit: events=%d want=1", len(bus.events))
	}

	if err = svc.UnlinkRequirementControl(82, requirement.ID, control.ID); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("unlink audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue = assertAuditEvent(t, bus, 1, auditModuleGRCRequirementControl, auditActionUnlink, link.ID, 82)
	if newValue != nil {
		t.Fatalf("unlink new value=%#v want empty", newValue)
	}
	assertSnapshotKeys(t, oldValue, []string{"id", "requirementId", "controlId"})
	if oldValue["id"] != float64(link.ID) || oldValue["requirementId"] != float64(requirement.ID) {
		t.Fatalf("unexpected unlink snapshot: %#v", oldValue)
	}

	if err = svc.UnlinkRequirementControl(82, requirement.ID, control.ID); err == nil {
		t.Fatal("expected not found unlink error")
	}
	if len(bus.events) != 2 {
		t.Fatalf("missing unlink published audit: events=%d want=2", len(bus.events))
	}
}

func TestControlRiskAuditLinkAndUnlink(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	asset := createRiskAsset(t, f)
	risk, err := svc.SaveRisk(0, 10, validRiskRequest(f, asset.ID))
	if err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)

	req := ControlRiskRequest{ControlID: control.ID, RiskID: risk.ID}
	link, err := svc.LinkControlRisk(91, req)
	if err != nil {
		t.Fatal(err)
	}

	_, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCControlRisk, auditActionLink, link.ID, 91)
	assertSnapshotKeys(t, newValue, []string{"id", "controlId", "riskId"})
	if newValue["id"] != float64(link.ID) ||
		newValue["controlId"] != float64(control.ID) ||
		newValue["riskId"] != float64(risk.ID) {
		t.Fatalf("unexpected link snapshot: %#v", newValue)
	}

	if _, err = svc.LinkControlRisk(91, req); err == nil {
		t.Fatal("expected duplicate link error")
	}
	if _, err = svc.LinkControlRisk(91, ControlRiskRequest{ControlID: control.ID, RiskID: 999999}); err == nil {
		t.Fatal("expected unknown risk error")
	}
	if len(bus.events) != 1 {
		t.Fatalf("rejected links published audit: events=%d want=1", len(bus.events))
	}

	if err = svc.UnlinkControlRisk(92, control.ID, risk.ID); err != nil {
		t.Fatal(err)
	}

	oldValue, newValue := assertAuditEvent(t, bus, 1, auditModuleGRCControlRisk, auditActionUnlink, link.ID, 92)
	if newValue != nil {
		t.Fatalf("unlink new value=%#v want empty", newValue)
	}
	assertSnapshotKeys(t, oldValue, []string{"id", "controlId", "riskId"})
	if oldValue["id"] != float64(link.ID) || oldValue["riskId"] != float64(risk.ID) {
		t.Fatalf("unexpected unlink snapshot: %#v", oldValue)
	}

	if err = svc.UnlinkControlRisk(92, control.ID, risk.ID); err == nil {
		t.Fatal("expected not found unlink error")
	}
	if len(bus.events) != 2 {
		t.Fatalf("missing unlink published audit: events=%d want=2", len(bus.events))
	}
}

func TestComplianceUnlinkAPIAuditsAuthenticatedActor(t *testing.T) {
	f, svc := newComplianceServiceFixture(t)
	framework := createFramework(t, svc, "FW-API", "1.0")
	requirement := createRequirement(t, svc, framework.ID, "REQ-1", nil)
	control := createControl(t, svc, f, "CTL-1", ControlStatusActive)
	link, err := svc.LinkRequirementControl(10, RequirementControlRequest{RequirementID: requirement.ID, ControlID: control.ID})
	if err != nil {
		t.Fatal(err)
	}
	bus := withCaptureBus(t)

	r := grcServiceRouter(svc, []string{"grc.control.manage"})
	w := grcRequest(r, http.MethodDelete, "/api/grc/requirement-controls/"+jsonUint(requirement.ID)+"/"+jsonUint(control.ID))
	if w.Code != http.StatusNoContent {
		t.Fatalf("unlink status=%d body=%s", w.Code, w.Body.String())
	}

	assertAuditEvent(t, bus, 0, auditModuleGRCRequirementControl, auditActionUnlink, link.ID, 7)
}
