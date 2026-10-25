package grc

import (
	"encoding/json"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/core/audit"
	"github.com/lallene/medcore-his/backend/internal/core/events"
)

type captureBus struct {
	events []events.Event
}

func (b *captureBus) Publish(event events.Event) error {
	b.events = append(b.events, event)
	return nil
}

func (b *captureBus) Subscribe(string, events.Handler) {}

func withCaptureBus(t *testing.T) *captureBus {
	t.Helper()

	previous := events.DefaultBus
	bus := &captureBus{}
	events.DefaultBus = bus

	t.Cleanup(func() {
		events.DefaultBus = previous
	})

	return bus
}

func requireAuditEvent(t *testing.T, bus *captureBus, index int) audit.AuditEvent {
	t.Helper()

	if len(bus.events) <= index {
		t.Fatalf("missing audit event %d: captured=%d", index, len(bus.events))
	}

	event, ok := bus.events[index].(audit.AuditEvent)
	if !ok {
		t.Fatalf("event %d type=%T, want audit.AuditEvent", index, bus.events[index])
	}

	return event
}

func decodeAuditJSON(t *testing.T, raw string) map[string]any {
	t.Helper()

	if raw == "" {
		t.Fatal("audit JSON is empty")
	}

	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("invalid audit JSON: %v raw=%q", err, raw)
	}

	return value
}

func TestAssetAuditCreateAndUpdate(t *testing.T) {
	fx := newAssetFixture(t)
	svc := NewService(fx.db)
	bus := withCaptureBus(t)

	req := validAssetRequest(fx)
	created, err := svc.SaveAsset(0, 41, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(bus.events) != 1 {
		t.Fatalf("create audit events=%d want=1", len(bus.events))
	}

	createEvent := requireAuditEvent(t, bus, 0)
	if createEvent.Module != auditModuleGRCAsset ||
		createEvent.Action != auditActionCreate ||
		createEvent.RecordID != created.ID {
		t.Fatalf("unexpected create event: %+v", createEvent)
	}
	if createEvent.UserID == nil || *createEvent.UserID != 41 {
		t.Fatalf("create user=%v want=41", createEvent.UserID)
	}
	if createEvent.OldValue != "" {
		t.Fatalf("create old value=%q want empty", createEvent.OldValue)
	}

	createValue := decodeAuditJSON(t, createEvent.NewValue)
	if createValue["code"] != created.Code ||
		createValue["criticality"] != created.Criticality ||
		createValue["status"] != created.Status {
		t.Fatalf("unexpected create snapshot: %#v", createValue)
	}
	if _, exists := createValue["createdBy"]; exists {
		t.Fatal("audit snapshot must not expose createdBy")
	}
	if _, exists := createValue["updatedBy"]; exists {
		t.Fatal("audit snapshot must not expose updatedBy")
	}
	if _, exists := createValue["description"]; exists {
		t.Fatal("asset audit snapshot must not expose description")
	}

	update := req
	update.Name = "Application GRC mise à jour"
	update.Criticality = AssetCriticalityHigh

	updated, err := svc.SaveAsset(created.ID, 52, update)
	if err != nil {
		t.Fatal(err)
	}

	if len(bus.events) != 2 {
		t.Fatalf("update audit events=%d want=2", len(bus.events))
	}

	updateEvent := requireAuditEvent(t, bus, 1)
	if updateEvent.Module != auditModuleGRCAsset ||
		updateEvent.Action != auditActionUpdate ||
		updateEvent.RecordID != created.ID {
		t.Fatalf("unexpected update event: %+v", updateEvent)
	}
	if updateEvent.UserID == nil || *updateEvent.UserID != 52 {
		t.Fatalf("update user=%v want=52", updateEvent.UserID)
	}

	oldValue := decodeAuditJSON(t, updateEvent.OldValue)
	newValue := decodeAuditJSON(t, updateEvent.NewValue)

	if oldValue["name"] != created.Name {
		t.Fatalf("old name=%v want=%q", oldValue["name"], created.Name)
	}
	if newValue["name"] != updated.Name {
		t.Fatalf("new name=%v want=%q", newValue["name"], updated.Name)
	}
	if oldValue["criticality"] == newValue["criticality"] {
		t.Fatalf("criticality did not change: old=%v new=%v",
			oldValue["criticality"], newValue["criticality"])
	}
}

func TestRiskAuditCreateAndUpdate(t *testing.T) {
	fx := newAssetFixture(t)
	svc := NewService(fx.db)
	bus := withCaptureBus(t)

	asset := createRiskAsset(t, fx)
	req := validRiskRequest(fx, asset.ID)

	created, err := svc.SaveRisk(0, 61, req)
	if err != nil {
		t.Fatal(err)
	}

	if len(bus.events) != 2 {
		t.Fatalf("asset+risk create audit events=%d want=2", len(bus.events))
	}

	createEvent := requireAuditEvent(t, bus, 1)
	if createEvent.Module != auditModuleGRCRisk ||
		createEvent.Action != auditActionCreate ||
		createEvent.RecordID != created.ID {
		t.Fatalf("unexpected risk create event: %+v", createEvent)
	}
	if createEvent.UserID == nil || *createEvent.UserID != 61 {
		t.Fatalf("risk create user=%v want=61", createEvent.UserID)
	}
	if createEvent.OldValue != "" {
		t.Fatalf("risk create old value=%q want empty", createEvent.OldValue)
	}

	createValue := decodeAuditJSON(t, createEvent.NewValue)
	if createValue["reference"] != created.Reference {
		t.Fatalf("reference=%v want=%q", createValue["reference"], created.Reference)
	}
	if createValue["inherentScore"] != float64(created.InherentScore) {
		t.Fatalf("inherentScore=%v want=%d", createValue["inherentScore"], created.InherentScore)
	}
	if createValue["residualScore"] != float64(created.ResidualScore) {
		t.Fatalf("residualScore=%v want=%d", createValue["residualScore"], created.ResidualScore)
	}
	for _, sensitiveField := range []string{"description", "threat", "vulnerability"} {
		if _, exists := createValue[sensitiveField]; exists {
			t.Fatalf("risk audit snapshot must not expose %s", sensitiveField)
		}
	}

	update := req
	update.Title = "Risque GRC mis à jour"
	update.Likelihood = 5
	update.Impact = 4
	update.ResidualLikelihood = 2
	update.ResidualImpact = 2

	updated, err := svc.SaveRisk(created.ID, 62, update)
	if err != nil {
		t.Fatal(err)
	}

	if len(bus.events) != 3 {
		t.Fatalf("risk update audit events=%d want=3", len(bus.events))
	}

	updateEvent := requireAuditEvent(t, bus, 2)
	if updateEvent.Module != auditModuleGRCRisk ||
		updateEvent.Action != auditActionUpdate ||
		updateEvent.RecordID != created.ID {
		t.Fatalf("unexpected risk update event: %+v", updateEvent)
	}
	if updateEvent.UserID == nil || *updateEvent.UserID != 62 {
		t.Fatalf("risk update user=%v want=62", updateEvent.UserID)
	}

	oldValue := decodeAuditJSON(t, updateEvent.OldValue)
	newValue := decodeAuditJSON(t, updateEvent.NewValue)

	if oldValue["title"] != created.Title {
		t.Fatalf("old title=%v want=%q", oldValue["title"], created.Title)
	}
	if newValue["title"] != updated.Title {
		t.Fatalf("new title=%v want=%q", newValue["title"], updated.Title)
	}
	if newValue["inherentScore"] != float64(20) {
		t.Fatalf("new inherentScore=%v want=20", newValue["inherentScore"])
	}
	if newValue["residualScore"] != float64(4) {
		t.Fatalf("new residualScore=%v want=4", newValue["residualScore"])
	}
}

func TestRejectedGRCMutationsDoNotPublishAudit(t *testing.T) {
	fx := newAssetFixture(t)
	svc := NewService(fx.db)
	bus := withCaptureBus(t)

	badAsset := validAssetRequest(fx)
	badAsset.Type = "INVALID"

	if _, err := svc.SaveAsset(0, 71, badAsset); err == nil {
		t.Fatal("expected invalid asset error")
	}
	if len(bus.events) != 0 {
		t.Fatalf("invalid asset published %d events", len(bus.events))
	}

	asset := createRiskAsset(t, fx)
	baseline := len(bus.events)

	badRisk := validRiskRequest(fx, asset.ID)
	badRisk.Likelihood = 6

	if _, err := svc.SaveRisk(0, 72, badRisk); err == nil {
		t.Fatal("expected invalid risk error")
	}
	if len(bus.events) != baseline {
		t.Fatalf("invalid risk published audit: before=%d after=%d",
			baseline, len(bus.events))
	}
}

func TestFailedGRCDatabaseMutationsDoNotPublishAudit(t *testing.T) {
	fx := newAssetFixture(t)
	svc := NewService(fx.db)

	// Prépare les données avant d'installer le bus de capture :
	// leur création ne fait donc pas partie de ce que ce test mesure.
	asset, err := svc.SaveAsset(0, 81, validAssetRequest(fx))
	if err != nil {
		t.Fatal(err)
	}

	riskReq := validRiskRequest(fx, asset.ID)
	riskReq.Reference = "RISK-AUDIT-DUPLICATE"

	if _, err := svc.SaveRisk(0, 82, riskReq); err != nil {
		t.Fatal(err)
	}

	bus := withCaptureBus(t)

	duplicateAsset := validAssetRequest(fx)
	duplicateAsset.Name = "Duplicate asset rejected by database"

	if _, err := svc.SaveAsset(0, 83, duplicateAsset); err == nil {
		t.Fatal("expected duplicate asset database error")
	}
	if len(bus.events) != 0 {
		t.Fatalf(
			"failed duplicate asset mutation published %d audit events",
			len(bus.events),
		)
	}

	duplicateRisk := validRiskRequest(fx, asset.ID)
	duplicateRisk.Reference = " risk-audit-duplicate "
	duplicateRisk.Title = "Duplicate risk rejected by database"

	if _, err := svc.SaveRisk(0, 84, duplicateRisk); err == nil {
		t.Fatal("expected duplicate risk database error")
	}
	if len(bus.events) != 0 {
		t.Fatalf(
			"failed duplicate risk mutation published %d audit events",
			len(bus.events),
		)
	}
}
