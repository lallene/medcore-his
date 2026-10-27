package grc

import (
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/staff"
)

func newPolicyFixture(t *testing.T) grc01DServiceFixture {
	t.Helper()
	f := newGRC01DServiceFixture(t)
	if err := f.db.AutoMigrate(&GovernancePolicy{}, &GovernancePolicyFramework{}, &GovernancePolicyControl{}); err != nil {
		t.Fatal(err)
	}
	return f
}

func policyIDs(values ...uint) *[]uint {
	out := append([]uint(nil), values...)
	return &out
}

func policyRequest(reference string) GovernancePolicyRequest {
	return GovernancePolicyRequest{
		Reference:   reference,
		Title:       "Politique " + reference,
		Description: "secret-uniquement",
		Version:     "1.0",
	}
}

func TestGovernancePolicyCreateAndRelations(t *testing.T) {
	f := newPolicyFixture(t)
	framework := createFramework(t, f.svc, "FW-POL", "1.0")
	other := createFramework(t, f.svc, "FW-POL-2", "1.0")
	controlID := f.implementation.ControlID
	otherControl := createControl(t, f.svc, f.assetFixture, "CTL-POL", ControlStatusRetired)

	req := policyRequest(" pol-a ")
	req.Status = GovernancePolicyStatusApproved
	req.FrameworkIDs = policyIDs(other.ID, framework.ID, framework.ID)
	req.ControlIDs = policyIDs(controlID)
	req.OwningServiceID = &f.service.ID
	req.OwnerProfileID = &f.profile.ID
	req.ApproverProfileID = &f.profile.ID
	from := evidenceTestNow.Add(-24 * time.Hour)
	req.EffectiveFrom = &from
	created, err := f.svc.CreateGovernancePolicy(4, req)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != GovernancePolicyStatusDraft || created.Reference != "POL-A" || created.ApprovedAt != nil ||
		created.FrameworkCount != 2 || created.ControlCount != 1 || created.Frameworks[0].ID != framework.ID {
		t.Fatalf("create: %+v", created)
	}

	kept, err := f.svc.UpdateGovernancePolicy(created.ID, 5, policyRequest("POL-A"))
	if err != nil || kept.FrameworkCount != 2 || kept.Title != "Politique POL-A" {
		t.Fatalf("omitted relations must stay: %v %+v", err, kept)
	}

	cleared := policyRequest("POL-A")
	cleared.FrameworkIDs = policyIDs()
	cleared.ControlIDs = policyIDs(otherControl.ID)
	cleared.Status = GovernancePolicyStatusInReview
	replaced, err := f.svc.UpdateGovernancePolicy(created.ID, 5, cleared)
	if err != nil || replaced.FrameworkCount != 0 || replaced.ControlCount != 1 || replaced.Controls[0].ID != otherControl.ID || replaced.Status != GovernancePolicyStatusInReview {
		t.Fatalf("replace: %v %+v", err, replaced)
	}

	unknown := policyRequest("POL-A")
	unknown.Status = GovernancePolicyStatusInReview
	unknown.FrameworkIDs = policyIDs(999999)
	_, err = f.svc.UpdateGovernancePolicy(created.ID, 5, unknown)
	expectAppStatus(t, err, 404)
	still, err := f.svc.FindGovernancePolicy(created.ID)
	if err != nil || still.ControlCount != 1 || still.FrameworkCount != 0 {
		t.Fatalf("failed relation change must roll back: %v %+v", err, still)
	}

	badControl := policyRequest("POL-B")
	badControl.ControlIDs = policyIDs(999999)
	_, err = f.svc.CreateGovernancePolicy(4, badControl)
	expectAppStatus(t, err, 404)

	dup := policyRequest("POL-A")
	_, err = f.svc.CreateGovernancePolicy(4, dup)
	expectAppStatus(t, err, 409)
}

func TestGovernancePolicyProfilesValidateOnChange(t *testing.T) {
	f := newPolicyFixture(t)
	req := policyRequest("POL-OWNER")
	req.OwnerProfileID = &f.profile.ID
	req.OwningServiceID = &f.service.ID
	req.ApproverProfileID = &f.profile.ID
	created, err := f.svc.CreateGovernancePolicy(4, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&staff.Profile{}).Where("id = ?", f.profile.ID).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&f.service).Update("active", false).Error; err != nil {
		t.Fatal(err)
	}
	same := policyRequest("POL-OWNER")
	same.OwnerProfileID = &f.profile.ID
	same.OwningServiceID = &f.service.ID
	same.ApproverProfileID = &f.profile.ID
	same.Title = "Toujours éditable"
	if _, err = f.svc.UpdateGovernancePolicy(created.ID, 5, same); err != nil {
		t.Fatalf("historical inactive relations must stay editable: %v", err)
	}
	changed := same
	changed.ApproverProfileID = &f.inactiveProfile.ID
	_, err = f.svc.UpdateGovernancePolicy(created.ID, 5, changed)
	expectAppStatus(t, err, 409)
	missing := same
	missing.OwnerProfileID = policyUint(999999)
	_, err = f.svc.UpdateGovernancePolicy(created.ID, 5, missing)
	expectAppStatus(t, err, 404)
}

func policyUint(id uint) *uint { return &id }

func TestGovernancePolicyLifecycle(t *testing.T) {
	f := newPolicyFixture(t)
	created, err := f.svc.CreateGovernancePolicy(4, policyRequest("POL-LIFE"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.ApproveGovernancePolicy(created.ID, 5)
	expectAppStatus(t, err, 409)

	review := policyRequest("POL-LIFE")
	review.Status = GovernancePolicyStatusInReview
	if _, err = f.svc.UpdateGovernancePolicy(created.ID, 5, review); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.ApproveGovernancePolicy(created.ID, 5)
	expectAppStatus(t, err, 409)

	review.ApproverProfileID = &f.profile.ID
	if _, err = f.svc.UpdateGovernancePolicy(created.ID, 5, review); err != nil {
		t.Fatal(err)
	}
	approved, err := f.svc.ApproveGovernancePolicy(created.ID, 6)
	if err != nil || approved.Status != GovernancePolicyStatusApproved || approved.ApprovedAt == nil || !approved.ApprovedAt.Equal(evidenceTestNow) {
		t.Fatalf("approve: %v %+v", err, approved)
	}
	_, err = f.svc.PublishGovernancePolicy(created.ID, 6)
	expectAppStatus(t, err, 409)

	back := policyRequest("POL-LIFE")
	back.Status = GovernancePolicyStatusInReview
	back.ApproverProfileID = &f.profile.ID
	reopened, err := f.svc.UpdateGovernancePolicy(created.ID, 5, back)
	if err != nil || reopened.Status != GovernancePolicyStatusInReview || reopened.ApprovedAt != nil || reopened.PublishedAt != nil {
		t.Fatalf("return to review: %v %+v", err, reopened)
	}
	approved, err = f.svc.ApproveGovernancePolicy(created.ID, 6)
	if err != nil {
		t.Fatal(err)
	}

	from := evidenceTestNow
	meta := policyRequest("POL-LIFE")
	meta.ApproverProfileID = &f.profile.ID
	meta.EffectiveFrom = &from
	meta.Version = "1.1"
	if _, err = f.svc.UpdateGovernancePolicy(created.ID, 5, meta); err != nil {
		t.Fatalf("approved metadata: %v", err)
	}
	published, err := f.svc.PublishGovernancePolicy(created.ID, 7)
	if err != nil || published.Status != GovernancePolicyStatusPublished || published.PublishedAt == nil ||
		!published.PublishedAt.Equal(evidenceTestNow) || published.ApprovedAt == nil || published.EffectiveFrom == nil {
		t.Fatalf("publish: %v %+v", err, published)
	}
	_, err = f.svc.RetireGovernancePolicy(created.ID, 8)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := f.svc.FindGovernancePolicy(created.ID)
	if err != nil || retired.Status != GovernancePolicyStatusRetired || retired.RetiredAt == nil || !retired.RetiredAt.Equal(evidenceTestNow) || retired.PublishedAt == nil {
		t.Fatalf("retire: %v %+v", err, retired)
	}

	for _, status := range []string{GovernancePolicyStatusApproved, GovernancePolicyStatusPublished, GovernancePolicyStatusRetired} {
		req := policyRequest("POL-LIFE")
		req.Status = status
		_, err = f.svc.UpdateGovernancePolicy(created.ID, 5, req)
		expectAppStatus(t, err, 403)
	}
	meta.Title = "Retirée"
	_, err = f.svc.UpdateGovernancePolicy(created.ID, 5, meta)
	expectAppStatus(t, err, 409)
	_, err = f.svc.ApproveGovernancePolicy(created.ID, 5)
	expectAppStatus(t, err, 409)
	_, err = f.svc.PublishGovernancePolicy(created.ID, 5)
	expectAppStatus(t, err, 409)

	draft, err := f.svc.CreateGovernancePolicy(4, policyRequest("POL-EARLY"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.RetireGovernancePolicy(draft.ID, 5)
	expectAppStatus(t, err, 409)
}

func TestGovernancePolicyListFilters(t *testing.T) {
	f := newPolicyFixture(t)
	framework := createFramework(t, f.svc, "FW-LIST", "1.0")
	controlID := f.implementation.ControlID
	first := policyRequest("POL-ALPHA")
	first.Status = GovernancePolicyStatusDraft
	first.FrameworkIDs = policyIDs(framework.ID)
	first.OwningServiceID = &f.service.ID
	first.OwnerProfileID = &f.profile.ID
	if _, err := f.svc.CreateGovernancePolicy(4, first); err != nil {
		t.Fatal(err)
	}
	second := policyRequest("POL-BETA")
	second.Version = "9.9"
	second.ControlIDs = policyIDs(controlID)
	second.ApproverProfileID = &f.profile.ID
	created, err := f.svc.CreateGovernancePolicy(4, second)
	if err != nil {
		t.Fatal(err)
	}
	review := second
	review.Reference = "POL-BETA"
	review.Status = GovernancePolicyStatusInReview
	if _, err = f.svc.UpdateGovernancePolicy(created.ID, 5, review); err != nil {
		t.Fatal(err)
	}

	page, err := f.svc.ListGovernancePolicies(GovernancePolicyFilter{Search: "alpha", Status: GovernancePolicyStatusDraft})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "POL-ALPHA" {
		t.Fatalf("search+status: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{Search: "9.9", Status: GovernancePolicyStatusInReview})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "POL-BETA" {
		t.Fatalf("search+version: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{Search: "pol", FrameworkID: &framework.ID})
	if err != nil || page.Total != 1 || page.Items[0].Reference != "POL-ALPHA" || page.Items[0].FrameworkCount != 1 {
		t.Fatalf("search+framework: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{ControlID: &controlID, ApproverProfileID: &f.profile.ID})
	if err != nil || page.Total != 1 || page.Items[0].ControlCount != 1 {
		t.Fatalf("control+approver: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{OwningServiceID: &f.service.ID, OwnerProfileID: &f.profile.ID})
	if err != nil || page.Total != 1 {
		t.Fatalf("service+owner: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{Search: "secret-uniquement"})
	if err != nil || page.Total != 0 || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("description must not be searched: %v %+v", err, page)
	}
	page, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{Page: 2, Limit: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].Reference != "POL-BETA" {
		t.Fatalf("page: %v %+v", err, page)
	}
	_, err = f.svc.ListGovernancePolicies(GovernancePolicyFilter{Status: "REOPENED"})
	expectAppStatus(t, err, 400)
}
