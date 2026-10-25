package grc

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	externalReferenceSentinel   = "EXTERNAL_REFERENCE_SECRET_SENTINEL"
	evidenceDescriptionSentinel = "EVIDENCE_DESCRIPTION_SECRET_SENTINEL"
	actionDescriptionSentinel   = "ACTION_DESCRIPTION_SECRET_SENTINEL"
)

var (
	evidenceAuditAllowlist = []string{
		"reference", "title", "controlImplementationId", "type", "status",
		"collectedAt", "validFrom", "expiresAt", "ownerProfileId", "owningServiceId",
	}
	remediationActionAuditAllowlist = []string{
		"reference", "title", "controlImplementationId", "riskId", "assigneeProfileId",
		"owningServiceId", "priority", "status", "dueAt", "completedAt",
	}
	evidenceAuditForbiddenKeys = []string{
		"id", "description", "externalReference", "expired",
		"createdBy", "updatedBy", "createdAt", "updatedAt",
		"controlImplementation", "ownerProfile", "owningService",
	}
	remediationActionAuditForbiddenKeys = []string{
		"id", "description", "overdue",
		"createdBy", "updatedBy", "createdAt", "updatedAt",
		"controlImplementation", "risk", "assigneeProfile", "owningService",
	}
)

// assertSnapshotKeysExactly locks a snapshot to its allowlist.
func assertSnapshotKeysExactly(t *testing.T, snapshot map[string]any, allowlist, forbidden []string) {
	t.Helper()

	assertSnapshotKeys(t, snapshot, allowlist, forbidden...)
	if len(snapshot) != len(allowlist) {
		t.Errorf("snapshot has %d keys, want %d: %#v", len(snapshot), len(allowlist), snapshot)
	}
}

// assertSerializedAuditExcludes checks every captured event as a whole.
func assertSerializedAuditExcludes(t *testing.T, bus *captureBus, sentinels ...string) {
	t.Helper()

	raw, err := json.Marshal(bus.events)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range sentinels {
		if strings.Contains(string(raw), sentinel) {
			t.Errorf("audit events leak a sentinel value")
		}
	}
	assertRawAuditExcludes(t, bus, sentinels...)
}

func TestEvidenceAndActionAuditSnapshotsAreAllowlisted(t *testing.T) {
	cases := []struct {
		snapshot any
		fields   []string
		keys     []string
	}{
		{
			evidenceAuditSnapshot{},
			[]string{"Reference", "Title", "ControlImplementationID", "Type", "Status",
				"CollectedAt", "ValidFrom", "ExpiresAt", "OwnerProfileID", "OwningServiceID"},
			evidenceAuditAllowlist,
		},
		{
			remediationActionAuditSnapshot{},
			[]string{"Reference", "Title", "ControlImplementationID", "RiskID", "AssigneeProfileID",
				"OwningServiceID", "Priority", "Status", "DueAt", "CompletedAt"},
			remediationActionAuditAllowlist,
		},
	}

	for _, tc := range cases {
		typ := reflect.TypeOf(tc.snapshot)
		var fields, keys []string
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			fields = append(fields, field.Name)
			keys = append(keys, strings.Split(field.Tag.Get("json"), ",")[0])
		}
		if strings.Join(fields, ",") != strings.Join(tc.fields, ",") {
			t.Errorf("%s fields=%v, want exactly %v", typ.Name(), fields, tc.fields)
		}

		sort.Strings(keys)
		want := append([]string(nil), tc.keys...)
		sort.Strings(want)
		if strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Errorf("%s json keys=%v, want exactly %v", typ.Name(), keys, want)
		}
	}
}

func TestEvidenceAuditCreateAndUpdate(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	bus := withCaptureBus(t)

	collected := evidenceTestNow.Add(-48 * time.Hour)
	r := f.request("EVD-AUDIT")
	r.Description = evidenceDescriptionSentinel
	r.ExternalReference = externalReferenceSentinel
	r.CollectedAt = &collected
	r.ExpiresAt = timePtr(evidenceTestNow.Add(-time.Hour))
	r.Status = EvidenceStatusValid
	r.OwnerProfileID = &f.profile.ID

	created, err := f.svc.SaveEvidence(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Expired {
		t.Fatal("fixture evidence must be expired so the audit proves Expired is excluded")
	}
	if len(bus.events) != 1 {
		t.Fatalf("create audit events=%d want=1", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCEvidence, auditActionCreate, created.ID, 41)
	if oldValue != nil {
		t.Fatalf("create old value=%#v want empty", oldValue)
	}
	assertSnapshotKeysExactly(t, newValue, evidenceAuditAllowlist, evidenceAuditForbiddenKeys)
	if newValue["reference"] != "EVD-AUDIT" ||
		newValue["status"] != EvidenceStatusValid ||
		newValue["controlImplementationId"] != float64(f.implementation.ID) ||
		newValue["ownerProfileId"] != float64(f.profile.ID) ||
		newValue["owningServiceId"] != nil ||
		newValue["collectedAt"] == nil {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}

	r.Title = "Titre modifié"
	r.Status = EvidenceStatusDraft
	r.ExternalReference = externalReferenceSentinel + "-V2"
	r.Description = evidenceDescriptionSentinel + "-V2"
	r.ControlImplementationID = f.other.ID
	if _, err = f.svc.SaveEvidence(created.ID, 42, r); err != nil {
		t.Fatal(err)
	}
	if len(bus.events) != 2 {
		t.Fatalf("update audit events=%d want=2", len(bus.events))
	}

	oldValue, newValue = assertAuditEvent(t, bus, 1, auditModuleGRCEvidence, auditActionUpdate, created.ID, 42)
	assertSnapshotKeysExactly(t, oldValue, evidenceAuditAllowlist, evidenceAuditForbiddenKeys)
	assertSnapshotKeysExactly(t, newValue, evidenceAuditAllowlist, evidenceAuditForbiddenKeys)
	if oldValue["title"] != created.Title || oldValue["status"] != EvidenceStatusValid ||
		oldValue["controlImplementationId"] != float64(f.implementation.ID) {
		t.Fatalf("unexpected old snapshot: %#v", oldValue)
	}
	if newValue["title"] != "Titre modifié" || newValue["status"] != EvidenceStatusDraft ||
		newValue["controlImplementationId"] != float64(f.other.ID) {
		t.Fatalf("unexpected new snapshot: %#v", newValue)
	}

	assertSerializedAuditExcludes(t, bus, externalReferenceSentinel, evidenceDescriptionSentinel)
}

func TestEvidenceAuditRevocation(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	for _, initial := range []string{EvidenceStatusDraft, EvidenceStatusValid} {
		t.Run(initial, func(t *testing.T) {
			r := f.request("EVD-AUDIT-REVOKE-" + initial)
			r.Status = initial
			created := f.create(t, r)
			bus := withCaptureBus(t)

			r.Status = EvidenceStatusRevoked
			if _, err := f.svc.SaveEvidence(created.ID, 50, r); err != nil {
				t.Fatal(err)
			}
			if len(bus.events) != 1 {
				t.Fatalf("revocation audit events=%d want=1", len(bus.events))
			}
			oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCEvidence, auditActionUpdate, created.ID, 50)
			if oldValue["status"] != initial || newValue["status"] != EvidenceStatusRevoked {
				t.Fatalf("revocation not visible: old=%#v new=%#v", oldValue, newValue)
			}

			r.Title = "Modification refusée"
			_, err := f.svc.SaveEvidence(created.ID, 51, r)
			expectAppStatus(t, err, http.StatusConflict)
			if len(bus.events) != 1 {
				t.Fatalf("rejected update after revocation published audit: events=%d", len(bus.events))
			}
		})
	}
}

func TestEvidenceAuditSkipsRejectedMutations(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	f.create(t, f.request("EVD-AUDIT-DUP"))
	bus := withCaptureBus(t)
	unknown := uint(999999)

	cases := map[string]func(*EvidenceRequest){
		"validation":                     func(r *EvidenceRequest) { r.Type = "PASSWORD" },
		"external reference":             func(r *EvidenceRequest) { r.ExternalReference = "https://ged.example.local/doc?token=x" },
		"unknown control implementation": func(r *EvidenceRequest) { r.ControlImplementationID = unknown },
		"inactive owner":                 func(r *EvidenceRequest) { r.OwnerProfileID = &f.inactiveProfile.ID },
		"duplicate":                      func(r *EvidenceRequest) { r.Reference = "EVD-AUDIT-DUP" },
	}
	for name, mutate := range cases {
		r := f.request("EVD-AUDIT-NEW")
		mutate(&r)
		if _, err := f.svc.SaveEvidence(0, 10, r); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		assertNoAudit(t, bus, name)
	}

	if _, err := f.svc.SaveEvidence(999999, 10, f.request("EVD-AUDIT-MISSING")); err == nil {
		t.Fatal("expected not found")
	}
	assertNoAudit(t, bus, "unknown evidence update")
}

func TestRemediationActionAuditCreateAndUpdate(t *testing.T) {
	f := newRemediationActionFixture(t)
	bus := withCaptureBus(t)

	r := f.actionRequest("ACT-AUDIT")
	r.Description = actionDescriptionSentinel
	r.RiskID = &f.risk.ID
	r.DueAt = timePtr(evidenceTestNow.Add(-time.Hour))

	created, err := f.svc.SaveRemediationAction(0, 61, r)
	if err != nil {
		t.Fatal(err)
	}
	if !created.Overdue {
		t.Fatal("fixture action must be overdue so the audit proves Overdue is excluded")
	}
	if len(bus.events) != 1 {
		t.Fatalf("create audit events=%d want=1", len(bus.events))
	}

	oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCRemediationAction, auditActionCreate, created.ID, 61)
	if oldValue != nil {
		t.Fatalf("create old value=%#v want empty", oldValue)
	}
	assertSnapshotKeysExactly(t, newValue, remediationActionAuditAllowlist, remediationActionAuditForbiddenKeys)
	if newValue["reference"] != "ACT-AUDIT" || newValue["riskId"] != float64(f.risk.ID) ||
		newValue["completedAt"] != nil || newValue["assigneeProfileId"] != nil {
		t.Fatalf("unexpected create snapshot: %#v", newValue)
	}

	r.Priority = RemediationActionPriorityCritical
	r.Status = RemediationActionStatusBlocked
	r.AssigneeProfileID = &f.profile.ID
	r.Description = actionDescriptionSentinel + "-V2"
	if _, err = f.svc.SaveRemediationAction(created.ID, 62, r); err != nil {
		t.Fatal(err)
	}

	oldValue, newValue = assertAuditEvent(t, bus, 1, auditModuleGRCRemediationAction, auditActionUpdate, created.ID, 62)
	assertSnapshotKeysExactly(t, oldValue, remediationActionAuditAllowlist, remediationActionAuditForbiddenKeys)
	assertSnapshotKeysExactly(t, newValue, remediationActionAuditAllowlist, remediationActionAuditForbiddenKeys)
	if oldValue["priority"] != RemediationActionPriorityHigh || oldValue["status"] != RemediationActionStatusOpen ||
		oldValue["assigneeProfileId"] != nil {
		t.Fatalf("unexpected old snapshot: %#v", oldValue)
	}
	if newValue["priority"] != RemediationActionPriorityCritical || newValue["status"] != RemediationActionStatusBlocked ||
		newValue["assigneeProfileId"] != float64(f.profile.ID) {
		t.Fatalf("unexpected new snapshot: %#v", newValue)
	}

	assertSerializedAuditExcludes(t, bus, actionDescriptionSentinel)
}

func TestRemediationActionAuditCompletionAndReopening(t *testing.T) {
	f := newRemediationActionFixture(t)
	r := f.actionRequest("ACT-AUDIT-DONE")
	created := f.createAction(t, r)
	bus := withCaptureBus(t)

	r.Status = RemediationActionStatusDone
	if _, err := f.svc.SaveRemediationAction(created.ID, 70, r); err != nil {
		t.Fatal(err)
	}
	oldValue, newValue := assertAuditEvent(t, bus, 0, auditModuleGRCRemediationAction, auditActionUpdate, created.ID, 70)
	if oldValue["completedAt"] != nil || oldValue["status"] != RemediationActionStatusOpen {
		t.Fatalf("OPEN snapshot must have no completedAt: %#v", oldValue)
	}
	completedAt, err := time.Parse(time.RFC3339Nano, newValue["completedAt"].(string))
	if err != nil || !completedAt.Equal(evidenceTestNow) || newValue["status"] != RemediationActionStatusDone {
		t.Fatalf("DONE snapshot completedAt=%v want %v: %#v", newValue["completedAt"], evidenceTestNow, newValue)
	}

	r.Status = RemediationActionStatusOpen
	if _, err = f.svc.SaveRemediationAction(created.ID, 71, r); err != nil {
		t.Fatal(err)
	}
	oldValue, newValue = assertAuditEvent(t, bus, 1, auditModuleGRCRemediationAction, auditActionUpdate, created.ID, 71)
	if oldValue["completedAt"] == nil || newValue["completedAt"] != nil || newValue["status"] != RemediationActionStatusOpen {
		t.Fatalf("reopening must clear completedAt: old=%#v new=%#v", oldValue, newValue)
	}
	if len(bus.events) != 2 {
		t.Fatalf("audit events=%d want=2", len(bus.events))
	}
}

func TestRemediationActionAuditSkipsRejectedMutations(t *testing.T) {
	f := newRemediationActionFixture(t)
	f.createAction(t, f.actionRequest("ACT-AUDIT-DUP"))
	bus := withCaptureBus(t)
	unknown := uint(999999)

	cases := map[string]func(*RemediationActionRequest){
		"validation":     func(r *RemediationActionRequest) { r.Priority = "URGENT" },
		"no target":      func(r *RemediationActionRequest) { r.ControlImplementationID = nil },
		"unknown target": func(r *RemediationActionRequest) { r.ControlImplementationID = &unknown },
		"control risk":   func(r *RemediationActionRequest) { r.RiskID = &f.otherRisk.ID },
		"duplicate":      func(r *RemediationActionRequest) { r.Reference = "ACT-AUDIT-DUP" },
		"future completion": func(r *RemediationActionRequest) {
			r.Status = RemediationActionStatusDone
			r.CompletedAt = timePtr(evidenceTestNow.Add(time.Second))
		},
	}
	for name, mutate := range cases {
		r := f.actionRequest("ACT-AUDIT-NEW")
		mutate(&r)
		if _, err := f.svc.SaveRemediationAction(0, 10, r); err == nil {
			t.Fatalf("%s: expected error", name)
		}
		assertNoAudit(t, bus, name)
	}
}

func TestEvidenceAndActionReadsAreNotAudited(t *testing.T) {
	f := newRemediationActionFixture(t)
	evidence := f.create(t, f.request("EVD-AUDIT-READ"))
	action := f.createAction(t, f.actionRequest("ACT-AUDIT-READ"))
	bus := withCaptureBus(t)

	if _, err := f.svc.FindEvidence(evidence.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListEvidence(EvidenceFilter{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.FindRemediationAction(action.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ListRemediationActions(RemediationActionFilter{}); err != nil {
		t.Fatal(err)
	}

	r := grcServiceRouter(f.svc, []string{"grc.evidence.read", "grc.action.read"})
	for _, path := range []string{
		"/api/grc/evidence",
		"/api/grc/evidence/" + jsonUint(evidence.ID),
		"/api/grc/actions",
		"/api/grc/actions/" + jsonUint(action.ID),
	} {
		if w := grcRequest(r, http.MethodGet, path); w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d", path, w.Code)
		}
	}

	assertNoAudit(t, bus, "reads")
}
