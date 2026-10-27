package grc

import (
	"net/http"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
	"gorm.io/gorm"
)

var (
	// pgNow and pgNowParis are the same instant written with two offsets.
	pgNow      = time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	pgParis    = time.FixedZone("UTC+2", 2*3600)
	pgNowParis = pgNow.In(pgParis)
)

type pgEvidenceActionFixture struct {
	db             *gorm.DB
	svc            *GRCService
	service        organization.Service
	profile        staff.Profile
	implementation ControlImplementation
	other          ControlImplementation
	unlinked       ControlImplementation
	risk           Risk
	otherRisk      Risk
}

// newPGEvidenceActionFixture reuses the isolated GRC schema (pgtest DSN,
// unique schema, search_path, DROP SCHEMA on cleanup) and adds GRC-01C tables.
func newPGEvidenceActionFixture(t *testing.T) pgEvidenceActionFixture {
	t.Helper()

	db := grcCompliancePostgres(t)
	if err := db.AutoMigrate(&Evidence{}, &RemediationAction{}); err != nil {
		t.Fatal(err)
	}
	// Keep the global bus untouched: audit publication is covered by unit tests.
	withCaptureBus(t)

	svc := NewService(db)
	svc.now = func() time.Time { return pgNow }
	f := pgEvidenceActionFixture{db: db, svc: svc}

	department := organization.Department{Code: "GRC", Name: "GRC", Active: true, CreatedBy: 1, UpdatedBy: 1}
	if err := db.Create(&department).Error; err != nil {
		t.Fatal(err)
	}
	f.service = organization.Service{
		DepartmentID: department.ID, Code: "SEC", Name: "Sécurité",
		ServiceType: organization.TypeAdministrative, Active: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&f.service).Error; err != nil {
		t.Fatal(err)
	}
	user := auth.User{Name: "GRC 01C", Email: "grc.pg.01c@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	f.profile = staff.Profile{UserID: user.ID, EmployeeCode: "GRC-PG-01C", Active: true}
	if err := db.Create(&f.profile).Error; err != nil {
		t.Fatal(err)
	}

	control, err := svc.SaveControl(0, 1, ControlRequest{Code: "CTL-PG-01C", Name: "Contrôle", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	otherControl, err := svc.SaveControl(0, 1, ControlRequest{Code: "CTL-PG-01C-UNLINKED", Name: "Contrôle non lié", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := svc.SaveAsset(0, 1, AssetRequest{Code: "AST-PG-01C", Name: "Actif", Type: "APPLICATION", Criticality: "HIGH", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}
	otherAsset, err := svc.SaveAsset(0, 1, AssetRequest{Code: "AST-PG-01C-2", Name: "Actif 2", Type: "SERVER", Criticality: "LOW", Status: "ACTIVE"})
	if err != nil {
		t.Fatal(err)
	}

	saveImplementation := func(controlID, assetID uint) ControlImplementation {
		t.Helper()
		implementation, err := svc.SaveControlImplementation(0, 1, ControlImplementationRequest{
			ControlID: controlID, AssetID: assetID, Status: ControlImplementationStatusImplemented,
		})
		if err != nil {
			t.Fatal(err)
		}
		return *implementation
	}
	f.implementation = saveImplementation(control.ID, asset.ID)
	f.other = saveImplementation(control.ID, otherAsset.ID)
	f.unlinked = saveImplementation(otherControl.ID, asset.ID)

	saveRisk := func(reference string) Risk {
		t.Helper()
		risk, err := svc.SaveRisk(0, 1, RiskRequest{
			Reference: reference, Title: "Risque", Description: "Risque PG",
			Likelihood: 3, Impact: 4, ResidualLikelihood: 2, ResidualImpact: 2,
			Treatment: "REDUCE", Status: "OPEN",
		})
		if err != nil {
			t.Fatal(err)
		}
		return *risk
	}
	f.risk = saveRisk("RISK-PG-01C")
	f.otherRisk = saveRisk("RISK-PG-01C-OTHER")
	if _, err = svc.LinkControlRisk(1, ControlRiskRequest{ControlID: control.ID, RiskID: f.risk.ID}); err != nil {
		t.Fatal(err)
	}

	return f
}

func TestPostgresGRCEvidenceServiceAndConstraints(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	svc := f.svc

	// Same instant as now, written at +02:00: not expired yet.
	atNow, err := svc.SaveEvidence(0, 11, EvidenceRequest{
		Reference: "evd-pg-now", Title: "Preuve à échéance", ControlImplementationID: f.implementation.ID,
		Type: "report", Status: "valid", ValidFrom: timePtr(pgNow.Add(-24 * time.Hour)), ExpiresAt: timePtr(pgNowParis),
		OwnerProfileID: &f.profile.ID, OwningServiceID: &f.service.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if atNow.Expired || !atNow.ExpiresAt.Equal(pgNow) {
		t.Fatalf("expiry at now (+02:00) must equal now and not be expired: %+v", atNow.ExpiresAt)
	}
	if atNow.ControlImplementation == nil || atNow.ControlImplementation.Control == nil ||
		atNow.ControlImplementation.Asset == nil || atNow.OwnerProfile == nil || atNow.OwningService == nil {
		t.Fatalf("relations not preloaded from PostgreSQL: %+v", atNow)
	}

	// One second before now, written at +02:00: textually later than "10:00Z", yet expired.
	expired, err := svc.SaveEvidence(0, 11, EvidenceRequest{
		Reference: "EVD-PG-EXPIRED", Title: "Preuve expirée", ControlImplementationID: f.implementation.ID,
		Type: "CERTIFICATE", Status: "VALID", ExpiresAt: timePtr(pgNowParis.Add(-time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !expired.Expired {
		t.Fatal("evidence expiring one second before now must be expired")
	}

	draft, err := svc.SaveEvidence(0, 11, EvidenceRequest{
		Reference: "EVD-PG-DRAFT", Title: "Brouillon", ControlImplementationID: f.other.ID,
		Type: "DOCUMENT", Status: "DRAFT", ExpiresAt: timePtr(pgNow.Add(-time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}

	listRefs := func(filter EvidenceFilter) []string {
		t.Helper()
		page, err := svc.ListEvidence(filter)
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, item := range page.Items {
			refs = append(refs, item.Reference)
			if item.Expired != EvidenceExpired(item.Status, item.ExpiresAt, pgNow) {
				t.Fatalf("item %s Expired=%v inconsistent with the clock", item.Reference, item.Expired)
			}
		}
		return refs
	}
	yes, no := true, false
	if got := listRefs(EvidenceFilter{Expired: &yes}); len(got) != 1 || got[0] != "EVD-PG-EXPIRED" {
		t.Fatalf("expired=true returned %v", got)
	}
	if got := listRefs(EvidenceFilter{Expired: &no}); len(got) != 2 || got[0] != "EVD-PG-DRAFT" || got[1] != "EVD-PG-NOW" {
		t.Fatalf("expired=false returned %v", got)
	}
	if got := listRefs(EvidenceFilter{ExpiresBefore: timePtr(pgNowParis)}); len(got) != 3 {
		t.Fatalf("expiresBefore=now(+02:00) returned %v", got)
	}
	if got := listRefs(EvidenceFilter{ExpiresBefore: timePtr(pgNowParis.Add(-time.Second))}); len(got) != 2 {
		t.Fatalf("expiresBefore=now-1s returned %v", got)
	}

	// Update, duplicate and service-level relation checks.
	updated, err := svc.SaveEvidence(atNow.ID, 12, EvidenceRequest{
		Reference: "EVD-PG-NOW", Title: "Preuve modifiée", ControlImplementationID: f.other.ID,
		Type: "REPORT", Status: "VALID", ExpiresAt: atNow.ExpiresAt,
		OwnerProfileID: &f.profile.ID, OwningServiceID: &f.service.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedBy != 11 || updated.UpdatedBy != 12 || updated.ControlImplementationID != f.other.ID {
		t.Fatalf("unexpected update: %+v", updated)
	}

	_, err = svc.SaveEvidence(0, 11, EvidenceRequest{
		Reference: " evd-pg-now ", Title: "Doublon", ControlImplementationID: f.implementation.ID, Type: "REPORT", Status: "DRAFT",
	})
	requireServiceError(t, err, http.StatusConflict)

	_, err = svc.SaveEvidence(0, 11, EvidenceRequest{
		Reference: "EVD-PG-ORPHAN", Title: "Orpheline", ControlImplementationID: 999999, Type: "REPORT", Status: "DRAFT",
	})
	requireServiceError(t, err, http.StatusNotFound)

	// PostgreSQL itself: unique, foreign keys and CHECK constraints.
	direct := func(mutate func(*Evidence)) error {
		row := Evidence{
			Reference: "EVD-PG-DIRECT", Title: "Directe", ControlImplementationID: f.implementation.ID,
			Type: EvidenceTypeReport, Status: EvidenceStatusDraft, CreatedBy: 1, UpdatedBy: 1,
		}
		mutate(&row)
		return f.db.Create(&row).Error
	}
	unknown := uint(999999)
	requirePostgresError(t, direct(func(e *Evidence) { e.Reference = "EVD-PG-NOW" }), pgUniqueViolation, "")
	requirePostgresError(t, direct(func(e *Evidence) { e.ControlImplementationID = unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(e *Evidence) { e.OwnerProfileID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(e *Evidence) { e.OwningServiceID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(e *Evidence) { e.Type = "PASSWORD" }), pgCheckViolation, "grc_evidence_type_valid")
	requirePostgresError(t, direct(func(e *Evidence) { e.Status = "EXPIRED" }), pgCheckViolation, "grc_evidence_status_valid")
	requirePostgresError(t, direct(func(e *Evidence) {
		e.ValidFrom = timePtr(pgNow)
		e.ExpiresAt = timePtr(pgNowParis.Add(-time.Second))
	}), pgCheckViolation, "grc_evidence_validity_range")

	// RESTRICT: referenced parents survive a direct delete.
	requirePostgresError(t, f.db.Delete(&ControlImplementation{}, f.other.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&staff.Profile{}, f.profile.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&organization.Service{}, f.service.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, f.db, &ControlImplementation{}, f.other.ID)
	requireRowExists(t, f.db, &staff.Profile{}, f.profile.ID)
	requireRowExists(t, f.db, &organization.Service{}, f.service.ID)

	// REVOKED is terminal.
	revoked, err := svc.SaveEvidence(draft.ID, 13, EvidenceRequest{
		Reference: draft.Reference, Title: draft.Title, ControlImplementationID: draft.ControlImplementationID,
		Type: draft.Type, Status: "REVOKED", ExpiresAt: draft.ExpiresAt,
	})
	if err != nil || revoked.Status != EvidenceStatusRevoked {
		t.Fatalf("DRAFT -> REVOKED: %v %+v", err, revoked)
	}
	_, err = svc.SaveEvidence(draft.ID, 14, EvidenceRequest{
		Reference: draft.Reference, Title: "Après révocation", ControlImplementationID: draft.ControlImplementationID,
		Type: draft.Type, Status: "REVOKED",
	})
	requireServiceError(t, err, http.StatusConflict)
}

func TestPostgresGRCRemediationActionServiceAndConstraints(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	svc := f.svc

	save := func(r RemediationActionRequest) (*RemediationAction, error) {
		if r.Title == "" {
			r.Title = "Action " + r.Reference
		}
		if r.Priority == "" {
			r.Priority = RemediationActionPriorityHigh
		}
		if r.Status == "" {
			r.Status = RemediationActionStatusOpen
		}
		return svc.SaveRemediationAction(0, 21, r)
	}

	// Due exactly at now (+02:00): not overdue yet.
	controlOnly, err := save(RemediationActionRequest{
		Reference: "ACT-PG-CI", ControlImplementationID: &f.implementation.ID,
		AssigneeProfileID: &f.profile.ID, OwningServiceID: &f.service.ID, DueAt: timePtr(pgNowParis),
	})
	if err != nil {
		t.Fatal(err)
	}
	if controlOnly.Overdue || !controlOnly.DueAt.Equal(pgNow) {
		t.Fatalf("due at now (+02:00) must equal now and not be overdue: %+v", controlOnly.DueAt)
	}
	if controlOnly.ControlImplementation == nil || controlOnly.ControlImplementation.Control == nil ||
		controlOnly.ControlImplementation.Asset == nil || controlOnly.AssigneeProfile == nil || controlOnly.OwningService == nil {
		t.Fatalf("relations not preloaded from PostgreSQL: %+v", controlOnly)
	}

	riskOnly, err := save(RemediationActionRequest{
		Reference: "ACT-PG-RISK", RiskID: &f.otherRisk.ID, Status: "IN_PROGRESS",
		DueAt: timePtr(pgNowParis.Add(-time.Second)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !riskOnly.Overdue || riskOnly.Risk == nil {
		t.Fatalf("risk-only action due one second ago must be overdue: %+v", riskOnly)
	}

	both, err := save(RemediationActionRequest{
		Reference: "ACT-PG-BOTH", ControlImplementationID: &f.implementation.ID, RiskID: &f.risk.ID,
	})
	if err != nil {
		t.Fatalf("coherent two-target action rejected: %v", err)
	}

	_, err = save(RemediationActionRequest{Reference: "ACT-PG-INCOHERENT", ControlImplementationID: &f.unlinked.ID, RiskID: &f.risk.ID})
	requireServiceError(t, err, http.StatusConflict)
	_, err = save(RemediationActionRequest{Reference: " act-pg-ci ", ControlImplementationID: &f.implementation.ID})
	requireServiceError(t, err, http.StatusConflict)
	_, err = save(RemediationActionRequest{
		Reference: "ACT-PG-FUTURE", RiskID: &f.risk.ID, Status: "DONE", CompletedAt: timePtr(pgNowParis.Add(time.Second)),
	})
	requireServiceError(t, err, http.StatusBadRequest)

	// DONE without CompletedAt: filled with now; DONE -> OPEN clears the column.
	done, err := svc.SaveRemediationAction(both.ID, 22, RemediationActionRequest{
		Reference: both.Reference, Title: both.Title, ControlImplementationID: both.ControlImplementationID,
		RiskID: both.RiskID, Priority: both.Priority, Status: "DONE", DueAt: timePtr(pgNow.Add(-2 * time.Hour)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedAt == nil || !done.CompletedAt.Equal(pgNow) || done.Overdue {
		t.Fatalf("DONE must be completed at now and not overdue: %+v", done)
	}

	yes, no := true, false
	listRefs := func(filter RemediationActionFilter) []string {
		t.Helper()
		page, err := svc.ListRemediationActions(filter)
		if err != nil {
			t.Fatal(err)
		}
		refs := []string{}
		for _, item := range page.Items {
			refs = append(refs, item.Reference)
			if item.Overdue != RemediationActionOverdue(item.Status, item.DueAt, pgNow) {
				t.Fatalf("item %s Overdue=%v inconsistent with the clock", item.Reference, item.Overdue)
			}
		}
		return refs
	}
	if got := listRefs(RemediationActionFilter{Overdue: &yes}); len(got) != 1 || got[0] != "ACT-PG-RISK" {
		t.Fatalf("overdue=true returned %v", got)
	}
	if got := listRefs(RemediationActionFilter{Overdue: &no}); len(got) != 2 {
		t.Fatalf("overdue=false returned %v", got)
	}
	// Ordering by instant: DONE (-2h), RISK (-1s), CI (now).
	if got := listRefs(RemediationActionFilter{}); len(got) != 3 ||
		got[0] != "ACT-PG-BOTH" || got[1] != "ACT-PG-RISK" || got[2] != "ACT-PG-CI" {
		t.Fatalf("order by due instant returned %v", got)
	}
	if got := listRefs(RemediationActionFilter{DueBefore: timePtr(pgNowParis.Add(-time.Second))}); len(got) != 2 {
		t.Fatalf("dueBefore=now-1s(+02:00) returned %v", got)
	}

	reopened, err := svc.SaveRemediationAction(both.ID, 23, RemediationActionRequest{
		Reference: both.Reference, Title: both.Title, ControlImplementationID: both.ControlImplementationID,
		RiskID: both.RiskID, Priority: both.Priority, Status: "OPEN",
	})
	if err != nil || reopened.CompletedAt != nil {
		t.Fatalf("DONE -> OPEN: %v %+v", err, reopened)
	}
	var cleared int64
	if err = f.db.Model(&RemediationAction{}).Where("id = ? AND completed_at IS NULL", both.ID).Count(&cleared).Error; err != nil || cleared != 1 {
		t.Fatalf("completed_at must be NULL in PostgreSQL after reopening (count=%d err=%v)", cleared, err)
	}

	// PostgreSQL itself: unique, foreign keys and CHECK constraints.
	direct := func(mutate func(*RemediationAction)) error {
		row := RemediationAction{
			Reference: "ACT-PG-DIRECT", Title: "Directe", ControlImplementationID: &f.implementation.ID,
			Priority: RemediationActionPriorityLow, Status: RemediationActionStatusOpen, CreatedBy: 1, UpdatedBy: 1,
		}
		mutate(&row)
		return f.db.Create(&row).Error
	}
	unknown := uint(999999)
	completed := pgNow
	requirePostgresError(t, direct(func(a *RemediationAction) { a.Reference = "ACT-PG-CI" }), pgUniqueViolation, "")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.ControlImplementationID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.RiskID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.AssigneeProfileID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.OwningServiceID = &unknown }), pgForeignKeyViolation, "")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.ControlImplementationID = nil }),
		pgCheckViolation, RemediationActionTargetConstraint)
	requirePostgresError(t, direct(func(a *RemediationAction) { a.Priority = "URGENT" }),
		pgCheckViolation, "grc_remediation_action_priority_valid")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.Status = "CLOSED" }),
		pgCheckViolation, "grc_remediation_action_status_valid")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.Status = RemediationActionStatusDone }),
		pgCheckViolation, "grc_remediation_action_completion")
	requirePostgresError(t, direct(func(a *RemediationAction) { a.CompletedAt = &completed }),
		pgCheckViolation, "grc_remediation_action_completion")

	// RESTRICT: referenced parents survive a direct delete.
	requirePostgresError(t, f.db.Delete(&ControlImplementation{}, f.implementation.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&Risk{}, f.otherRisk.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&staff.Profile{}, f.profile.ID).Error, pgForeignKeyViolation, "")
	requirePostgresError(t, f.db.Delete(&organization.Service{}, f.service.ID).Error, pgForeignKeyViolation, "")
	requireRowExists(t, f.db, &ControlImplementation{}, f.implementation.ID)
	requireRowExists(t, f.db, &Risk{}, f.otherRisk.ID)
	requireRowExists(t, f.db, &RemediationAction{}, riskOnly.ID)
}

func TestPostgresGRCEvidenceActionHTTP(t *testing.T) {
	f := newPGEvidenceActionFixture(t)
	r := grcServiceRouter(f.svc, []string{
		"grc.evidence.read", "grc.evidence.manage",
		"grc.action.read", "grc.action.create", "grc.action.update",
	})

	// Evidence: POST -> GET -> PUT -> LIST.
	w := grcJSONRequest(r, http.MethodPost, "/api/grc/evidence", `{
		"reference": "EVD-PG-HTTP", "title": "Preuve HTTP",
		"controlImplementationId": `+jsonUint(f.implementation.ID)+`,
		"type": "REPORT", "status": "VALID", "expiresAt": "2026-10-25T11:59:59+02:00"
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST evidence status=%d body=%s", w.Code, w.Body.String())
	}
	evidence := decodeJSON[Evidence](t, w.Body.Bytes())
	if !evidence.Expired || evidence.CreatedBy != 7 {
		t.Fatalf("unexpected evidence: %+v", evidence)
	}
	path := "/api/grc/evidence/" + jsonUint(evidence.ID)
	if w = grcRequest(r, http.MethodGet, path); w.Code != http.StatusOK {
		t.Fatalf("GET evidence status=%d", w.Code)
	}
	w = grcJSONRequest(r, http.MethodPut, path, `{
		"reference": "EVD-PG-HTTP", "title": "Preuve HTTP modifiée",
		"controlImplementationId": `+jsonUint(f.implementation.ID)+`,
		"type": "REPORT", "status": "VALID", "expiresAt": "2026-10-25T12:00:00+02:00"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT evidence status=%d body=%s", w.Code, w.Body.String())
	}
	if updated := decodeJSON[Evidence](t, w.Body.Bytes()); updated.Expired {
		t.Fatal("evidence expiring exactly at now must not be expired")
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/evidence?expired=false&expiresBefore=2026-10-25T10%3A00%3A00Z")
	if page := decodeJSON[EvidencePage](t, w.Body.Bytes()); w.Code != http.StatusOK || page.Total != 1 {
		t.Fatalf("LIST evidence status=%d body=%s", w.Code, w.Body.String())
	}

	// Action: POST -> GET -> PUT -> LIST.
	w = grcJSONRequest(r, http.MethodPost, "/api/grc/actions", `{
		"reference": "ACT-PG-HTTP", "title": "Action HTTP",
		"controlImplementationId": `+jsonUint(f.implementation.ID)+`, "riskId": `+jsonUint(f.risk.ID)+`,
		"priority": "HIGH", "status": "OPEN", "dueAt": "2026-10-25T11:59:59+02:00"
	}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST action status=%d body=%s", w.Code, w.Body.String())
	}
	action := decodeJSON[RemediationAction](t, w.Body.Bytes())
	if !action.Overdue || action.CreatedBy != 7 {
		t.Fatalf("unexpected action: %+v", action)
	}
	path = "/api/grc/actions/" + jsonUint(action.ID)
	if w = grcRequest(r, http.MethodGet, path); w.Code != http.StatusOK {
		t.Fatalf("GET action status=%d", w.Code)
	}
	w = grcJSONRequest(r, http.MethodPut, path, `{
		"reference": "ACT-PG-HTTP", "title": "Action HTTP",
		"controlImplementationId": `+jsonUint(f.implementation.ID)+`, "riskId": `+jsonUint(f.risk.ID)+`,
		"priority": "HIGH", "status": "DONE", "dueAt": "2026-10-25T11:59:59+02:00"
	}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT action status=%d body=%s", w.Code, w.Body.String())
	}
	if done := decodeJSON[RemediationAction](t, w.Body.Bytes()); done.CompletedAt == nil || !done.CompletedAt.Equal(pgNow) || done.Overdue {
		t.Fatalf("unexpected completed action: %+v", done)
	}
	w = grcRequest(r, http.MethodGet, "/api/grc/actions?overdue=false&dueBefore=2026-10-25T12%3A00%3A00%2B02%3A00")
	if page := decodeJSON[RemediationActionPage](t, w.Body.Bytes()); w.Code != http.StatusOK || page.Total != 1 {
		t.Fatalf("LIST action status=%d body=%s", w.Code, w.Body.String())
	}
}
