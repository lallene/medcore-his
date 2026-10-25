package grc

import (
	"net/http"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/auth"
	"github.com/lallene/medcore-his/backend/internal/modules/organization"
	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

var evidenceTestNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

type evidenceServiceFixture struct {
	assetFixture
	svc            *GRCService
	implementation ControlImplementation
	other          ControlImplementation
}

func newEvidenceServiceFixture(t *testing.T) evidenceServiceFixture {
	t.Helper()

	f, svc := newComplianceServiceFixture(t)
	if err := f.db.AutoMigrate(&Evidence{}, &RemediationAction{}); err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return evidenceTestNow }

	control := createControl(t, svc, f, "CTL-EVD", ControlStatusActive)
	asset := createComplianceAsset(t, svc, f, "AST-EVD", AssetStatusActive)
	otherAsset := createComplianceAsset(t, svc, f, "AST-EVD-2", AssetStatusActive)

	implementation, err := svc.SaveControlImplementation(0, 10, ControlImplementationRequest{
		ControlID: control.ID, AssetID: asset.ID, Status: ControlImplementationStatusImplemented,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.SaveControlImplementation(0, 10, ControlImplementationRequest{
		ControlID: control.ID, AssetID: otherAsset.ID, Status: ControlImplementationStatusPlanned,
	})
	if err != nil {
		t.Fatal(err)
	}

	return evidenceServiceFixture{assetFixture: f, svc: svc, implementation: *implementation, other: *other}
}

func (f evidenceServiceFixture) request(reference string) EvidenceRequest {
	return EvidenceRequest{
		Reference:               reference,
		Title:                   "Preuve " + reference,
		ControlImplementationID: f.implementation.ID,
		Type:                    EvidenceTypeReport,
		Status:                  EvidenceStatusDraft,
	}
}

func (f evidenceServiceFixture) create(t *testing.T, r EvidenceRequest) Evidence {
	t.Helper()

	evidence, err := f.svc.SaveEvidence(0, 10, r)
	if err != nil {
		t.Fatal(err)
	}
	return *evidence
}

func createExtraProfile(t *testing.T, f evidenceServiceFixture, code string) staff.Profile {
	t.Helper()

	user := auth.User{Name: code, Email: code + "@test.local", PasswordHash: "x", Role: "staff", IsActive: true}
	if err := f.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	profile := staff.Profile{UserID: user.ID, EmployeeCode: code, Active: true}
	if err := f.db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	return profile
}

func createExtraService(t *testing.T, f evidenceServiceFixture, code string) organization.Service {
	t.Helper()

	service := organization.Service{
		DepartmentID: f.service.DepartmentID,
		Code:         code,
		Name:         "Service " + code,
		ServiceType:  organization.TypeAdministrative,
		Active:       true,
		CreatedBy:    1,
		UpdatedBy:    1,
	}
	if err := f.db.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	return service
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func TestSaveEvidenceCreatesWithActorsAndRelations(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	r := f.request(" evd-001 ")
	r.Title = " Rapport de sauvegarde "
	r.Type = " report "
	r.Status = " valid "
	r.ExternalReference = " DOC-SEC-2026-0042 "
	r.OwnerProfileID = &f.profile.ID
	r.OwningServiceID = &f.service.ID

	created, err := f.svc.SaveEvidence(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}

	if created.Reference != "EVD-001" ||
		created.Title != "Rapport de sauvegarde" ||
		created.Type != EvidenceTypeReport ||
		created.Status != EvidenceStatusValid ||
		created.ExternalReference != "DOC-SEC-2026-0042" ||
		created.CreatedBy != 41 ||
		created.UpdatedBy != 41 {
		t.Fatalf("unexpected evidence: %+v", created)
	}
	if created.ControlImplementation == nil ||
		created.ControlImplementation.Control == nil ||
		created.ControlImplementation.Asset == nil ||
		created.OwnerProfile == nil ||
		created.OwningService == nil {
		t.Fatalf("relations not preloaded: %+v", created)
	}
}

func TestSaveEvidenceRejectsInvalidCreations(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	f.create(t, f.request("EVD-DUP"))
	unknown := uint(999999)

	cases := []struct {
		name   string
		status int
		mutate func(*EvidenceRequest)
	}{
		{"duplicate reference", http.StatusConflict, func(r *EvidenceRequest) { r.Reference = " evd-dup " }},
		{"unknown control implementation", http.StatusNotFound, func(r *EvidenceRequest) { r.ControlImplementationID = unknown }},
		{"unknown owner", http.StatusNotFound, func(r *EvidenceRequest) { r.OwnerProfileID = &unknown }},
		{"inactive owner", http.StatusConflict, func(r *EvidenceRequest) { r.OwnerProfileID = &f.inactiveProfile.ID }},
		{"unknown service", http.StatusNotFound, func(r *EvidenceRequest) { r.OwningServiceID = &unknown }},
		{"inactive service", http.StatusConflict, func(r *EvidenceRequest) { r.OwningServiceID = &f.inactiveService.ID }},
		{"invalid status", http.StatusBadRequest, func(r *EvidenceRequest) { r.Status = "EXPIRED" }},
		{"external reference with query", http.StatusBadRequest, func(r *EvidenceRequest) {
			r.ExternalReference = "https://ged.example.local/doc?token=abc"
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request("EVD-NEW")
			tc.mutate(&r)

			_, err := f.svc.SaveEvidence(0, 10, r)
			expectAppStatus(t, err, tc.status)
		})
	}

	_, err := f.svc.SaveEvidence(999999, 10, f.request("EVD-MISSING"))
	expectAppStatus(t, err, http.StatusNotFound)
}

func TestSaveEvidenceUpdatesAndRevalidatesChangedRelations(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	r := f.request("EVD-UPD")
	created, err := f.svc.SaveEvidence(0, 41, r)
	if err != nil {
		t.Fatal(err)
	}

	r.Title = "Titre modifié"
	r.ControlImplementationID = f.other.ID
	updated, err := f.svc.SaveEvidence(created.ID, 52, r)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreatedBy != 41 || updated.UpdatedBy != 52 ||
		updated.Title != "Titre modifié" || updated.ControlImplementationID != f.other.ID {
		t.Fatalf("unexpected update: %+v", updated)
	}

	unknown := uint(999999)
	cases := []struct {
		name   string
		status int
		mutate func(*EvidenceRequest)
	}{
		{"unknown control implementation", http.StatusNotFound, func(r *EvidenceRequest) { r.ControlImplementationID = unknown }},
		{"inactive owner", http.StatusConflict, func(r *EvidenceRequest) { r.OwnerProfileID = &f.inactiveProfile.ID }},
		{"inactive service", http.StatusConflict, func(r *EvidenceRequest) { r.OwningServiceID = &f.inactiveService.ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := r
			tc.mutate(&next)

			_, err := f.svc.SaveEvidence(created.ID, 53, next)
			expectAppStatus(t, err, tc.status)
		})
	}

	reloaded, err := f.svc.FindEvidence(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ControlImplementationID != f.other.ID || reloaded.OwnerProfileID != nil || reloaded.UpdatedBy != 52 {
		t.Fatalf("rejected updates must not be persisted: %+v", reloaded)
	}
}

func TestSaveEvidenceKeepsHistoricalOwnerAndService(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	owner := createExtraProfile(t, f, "GRC-EVD-OWNER")
	service := createExtraService(t, f, "EVD-SVC")

	r := f.request("EVD-HIST")
	r.OwnerProfileID = &owner.ID
	r.OwningServiceID = &service.ID
	created := f.create(t, r)

	if err := f.db.Model(&staff.Profile{}).Where("id = ?", owner.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&organization.Service{}).Where("id = ?", service.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}

	r.Title = "Titre historique modifié"
	updated, err := f.svc.SaveEvidence(created.ID, 60, r)
	if err != nil {
		t.Fatalf("historical evidence update rejected: %v", err)
	}
	if updated.Title != "Titre historique modifié" ||
		updated.OwnerProfileID == nil || *updated.OwnerProfileID != owner.ID ||
		updated.OwningServiceID == nil || *updated.OwningServiceID != service.ID {
		t.Fatalf("unexpected historical update: %+v", updated)
	}
}

func TestSaveEvidenceRevokedIsTerminal(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	for _, initial := range []string{EvidenceStatusDraft, EvidenceStatusValid} {
		t.Run(initial, func(t *testing.T) {
			r := f.request("EVD-REVOKE-" + initial)
			r.Status = initial
			created := f.create(t, r)

			r.Status = EvidenceStatusRevoked
			revoked, err := f.svc.SaveEvidence(created.ID, 70, r)
			if err != nil {
				t.Fatalf("%s -> REVOKED rejected: %v", initial, err)
			}
			if revoked.Status != EvidenceStatusRevoked {
				t.Fatalf("status=%s want REVOKED", revoked.Status)
			}

			_, err = f.svc.SaveEvidence(created.ID, 71, r)
			expectAppStatus(t, err, http.StatusConflict)

			r.Title = "Modification après révocation"
			_, err = f.svc.SaveEvidence(created.ID, 71, r)
			expectAppStatus(t, err, http.StatusConflict)

			r.Status = EvidenceStatusValid
			_, err = f.svc.SaveEvidence(created.ID, 71, r)
			expectAppStatus(t, err, http.StatusConflict)

			reloaded, err := f.svc.FindEvidence(created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.Status != EvidenceStatusRevoked || reloaded.UpdatedBy != 70 || reloaded.Title != created.Title {
				t.Fatalf("revoked evidence changed: %+v", reloaded)
			}
		})
	}
}

func TestFindEvidence(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	_, err := f.svc.FindEvidence(0)
	expectAppStatus(t, err, http.StatusBadRequest)

	_, err = f.svc.FindEvidence(999999)
	expectAppStatus(t, err, http.StatusNotFound)

	r := f.request("EVD-FIND")
	r.Status = EvidenceStatusValid
	r.ExpiresAt = timePtr(evidenceTestNow.Add(time.Hour))
	r.OwnerProfileID = &f.profile.ID
	r.OwningServiceID = &f.service.ID
	created := f.create(t, r)

	found, err := f.svc.FindEvidence(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ControlImplementation == nil || found.ControlImplementation.Control == nil ||
		found.ControlImplementation.Asset == nil || found.OwnerProfile == nil || found.OwningService == nil {
		t.Fatalf("relations not preloaded: %+v", found)
	}
	if found.Expired {
		t.Fatal("evidence expiring in one hour must not be expired")
	}

	f.svc.now = func() time.Time { return evidenceTestNow.Add(2 * time.Hour) }
	found, err = f.svc.FindEvidence(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !found.Expired {
		t.Fatal("evidence must be expired with the later clock")
	}
}

func TestListEvidencePaginationOrderAndFilters(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	owner := createExtraProfile(t, f, "GRC-EVD-LIST")
	service := createExtraService(t, f, "EVD-LIST")

	b := f.request("EVD-B")
	b.Title = "Configuration pare-feu"
	b.Type = EvidenceTypeConfiguration
	b.OwnerProfileID = &owner.ID
	f.create(t, b)

	a := f.request("EVD-A")
	a.Title = "Rapport annuel"
	a.Status = EvidenceStatusValid
	a.ExpiresAt = timePtr(evidenceTestNow.AddDate(0, 0, 10))
	a.OwningServiceID = &service.ID
	f.create(t, a)

	c := f.request("EVD-C")
	c.ControlImplementationID = f.other.ID
	c.ExpiresAt = timePtr(evidenceTestNow.AddDate(0, 0, 40))
	f.create(t, c)

	page, err := f.svc.ListEvidence(EvidenceFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.TotalPages != 2 || len(page.Items) != 2 ||
		page.Items[0].Reference != "EVD-A" || page.Items[1].Reference != "EVD-B" {
		t.Fatalf("unexpected first page: %+v", page)
	}
	if page.Items[0].ControlImplementation == nil || page.Items[0].ControlImplementation.Asset == nil ||
		page.Items[0].OwningService == nil {
		t.Fatalf("list relations not preloaded: %+v", page.Items[0])
	}

	page, err = f.svc.ListEvidence(EvidenceFilter{Page: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Reference != "EVD-C" {
		t.Fatalf("unexpected second page: %+v", page)
	}

	single := func(filter EvidenceFilter, want string) {
		t.Helper()
		page, err := f.svc.ListEvidence(filter)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 1 || page.Items[0].Reference != want {
			t.Fatalf("filter %+v: got %+v, want %s", filter, page.Items, want)
		}
	}

	single(EvidenceFilter{Search: "PARE-FEU"}, "EVD-B")
	single(EvidenceFilter{Search: "evd-a"}, "EVD-A")
	single(EvidenceFilter{ControlImplementationID: &f.other.ID}, "EVD-C")
	single(EvidenceFilter{OwnerProfileID: &owner.ID}, "EVD-B")
	single(EvidenceFilter{OwningServiceID: &service.ID}, "EVD-A")
	single(EvidenceFilter{Type: " configuration "}, "EVD-B")
	single(EvidenceFilter{Status: "valid"}, "EVD-A")
	single(EvidenceFilter{ExpiresBefore: timePtr(evidenceTestNow.AddDate(0, 0, 10))}, "EVD-A")
}

func TestListEvidenceExpiredFilterUsesOneClock(t *testing.T) {
	f := newEvidenceServiceFixture(t)

	expired := f.request("EVD-EXPIRED")
	expired.Status = EvidenceStatusValid
	expired.ExpiresAt = timePtr(evidenceTestNow.Add(-time.Second))
	f.create(t, expired)

	atNow := f.request("EVD-AT-NOW")
	atNow.Status = EvidenceStatusValid
	atNow.ExpiresAt = timePtr(evidenceTestNow)
	f.create(t, atNow)

	draftPast := f.request("EVD-DRAFT-PAST")
	draftPast.ExpiresAt = timePtr(evidenceTestNow.Add(-time.Hour))
	f.create(t, draftPast)

	noExpiry := f.request("EVD-NO-EXPIRY")
	noExpiry.Status = EvidenceStatusValid
	f.create(t, noExpiry)

	calls := 0
	f.svc.now = func() time.Time {
		calls++
		return evidenceTestNow
	}

	yes := true
	page, err := f.svc.ListEvidence(EvidenceFilter{Expired: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Reference != "EVD-EXPIRED" || !page.Items[0].Expired {
		t.Fatalf("expired=true: %+v", page.Items)
	}
	if calls != 1 {
		t.Fatalf("ListEvidence read the clock %d times, want 1", calls)
	}

	no := false
	page, err = f.svc.ListEvidence(EvidenceFilter{Expired: &no})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Fatalf("expired=false total=%d want 3: %+v", page.Total, page.Items)
	}
	for _, item := range page.Items {
		if item.Expired {
			t.Fatalf("expired=false returned an expired item: %+v", item)
		}
		if item.Reference == "EVD-EXPIRED" {
			t.Fatal("expired=false must exclude EVD-EXPIRED")
		}
	}

	// Moving the clock flips EVD-AT-NOW for both the filter and the field.
	f.svc.now = func() time.Time { return evidenceTestNow.Add(time.Second) }
	page, err = f.svc.ListEvidence(EvidenceFilter{Expired: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("expired=true later total=%d want 2", page.Total)
	}
	for _, item := range page.Items {
		if !item.Expired {
			t.Fatalf("filtered item not marked expired: %+v", item)
		}
	}
}

func TestListEvidenceRejectsInvalidFilters(t *testing.T) {
	f := newEvidenceServiceFixture(t)
	zero := uint(0)

	for name, filter := range map[string]EvidenceFilter{
		"zero control implementation": {ControlImplementationID: &zero},
		"zero owner":                  {OwnerProfileID: &zero},
		"zero service":                {OwningServiceID: &zero},
		"invalid type":                {Type: "PASSWORD"},
		"invalid status":              {Status: "EXPIRED"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.ListEvidence(filter)
			expectAppStatus(t, err, http.StatusBadRequest)
		})
	}
}
