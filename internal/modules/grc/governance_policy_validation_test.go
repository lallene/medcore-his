package grc

import (
	"strings"
	"testing"
	"time"
)

func TestGovernancePolicyRequestValidation(t *testing.T) {
	ok := GovernancePolicyRequest{Reference: " pol-1 ", Title: " Titre ", Version: " 1.0 ", Description: " libre "}
	normalizeGovernancePolicyRequest(&ok)
	if err := validateGovernancePolicyRequest(ok, true); err != nil {
		t.Fatal(err)
	}
	if ok.Reference != "POL-1" || ok.Title != "Titre" || ok.Version != "1.0" {
		t.Fatalf("normalization: %+v", ok)
	}

	cases := []struct {
		name    string
		mutate  func(*GovernancePolicyRequest)
		message string
	}{
		{"reference required", func(r *GovernancePolicyRequest) { r.Reference = "  " }, "obligatoires"},
		{"reference max", func(r *GovernancePolicyRequest) { r.Reference = strings.Repeat("A", 61) }, "trop longue"},
		{"title required", func(r *GovernancePolicyRequest) { r.Title = "" }, "obligatoires"},
		{"title max", func(r *GovernancePolicyRequest) { r.Title = strings.Repeat("é", 221) }, "trop long"},
		{"version required", func(r *GovernancePolicyRequest) { r.Version = " " }, "obligatoires"},
		{"version max", func(r *GovernancePolicyRequest) { r.Version = strings.Repeat("1", 41) }, "trop longue"},
		{"status invalid", func(r *GovernancePolicyRequest) { r.Status = "CANCELLED" }, "invalide"},
		{"effective end without start", func(r *GovernancePolicyRequest) {
			at := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
			r.EffectiveTo = &at
		}, "début"},
		{"effective order", func(r *GovernancePolicyRequest) {
			from := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
			to := from.Add(-time.Hour)
			r.EffectiveFrom, r.EffectiveTo = &from, &to
		}, "précéder"},
		{"zero framework", func(r *GovernancePolicyRequest) {
			ids := []uint{1, 0}
			r.FrameworkIDs = &ids
		}, "invalide"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := GovernancePolicyRequest{Reference: "POL-1", Title: "Titre", Version: "1.0"}
			tc.mutate(&r)
			normalizeGovernancePolicyRequest(&r)
			err := validateGovernancePolicyRequest(r, false)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v", err)
			}
		})
	}

	equal := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	same := GovernancePolicyRequest{Reference: "POL-1", Title: "Titre", Version: "1.0", EffectiveFrom: &equal, EffectiveTo: &equal}
	normalizeGovernancePolicyRequest(&same)
	if err := validateGovernancePolicyRequest(same, true); err != nil {
		t.Fatal(err)
	}
	ids := []uint{2, 1, 2}
	normalized, set, err := normalizePolicyIDList(&ids)
	if err != nil || !set || !samePolicyIDs(normalized, []uint{1, 2}) {
		t.Fatalf("duplicates: %v %v", normalized, err)
	}
	if _, set, err = normalizePolicyIDList(nil); err != nil || set {
		t.Fatalf("omitted list: set=%v err=%v", set, err)
	}
}

func TestGovernancePolicyTransitions(t *testing.T) {
	allowed := [][2]string{
		{"DRAFT", "DRAFT"}, {"DRAFT", "IN_REVIEW"},
		{"IN_REVIEW", "IN_REVIEW"}, {"IN_REVIEW", "DRAFT"}, {"IN_REVIEW", "APPROVED"},
		{"APPROVED", "APPROVED"}, {"APPROVED", "IN_REVIEW"}, {"APPROVED", "PUBLISHED"},
		{"PUBLISHED", "PUBLISHED"}, {"PUBLISHED", "RETIRED"},
		{"RETIRED", "RETIRED"},
	}
	for _, edge := range allowed {
		if err := validateGovernancePolicyTransition(edge[0], edge[1]); err != nil {
			t.Fatalf("%s -> %s: %v", edge[0], edge[1], err)
		}
	}
	forbidden := [][2]string{
		{"DRAFT", "APPROVED"}, {"DRAFT", "PUBLISHED"}, {"DRAFT", "RETIRED"},
		{"IN_REVIEW", "PUBLISHED"}, {"IN_REVIEW", "RETIRED"},
		{"APPROVED", "DRAFT"}, {"APPROVED", "RETIRED"},
		{"PUBLISHED", "DRAFT"}, {"PUBLISHED", "IN_REVIEW"}, {"PUBLISHED", "APPROVED"},
		{"RETIRED", "DRAFT"}, {"RETIRED", "IN_REVIEW"}, {"RETIRED", "APPROVED"}, {"RETIRED", "PUBLISHED"},
	}
	for _, edge := range forbidden {
		if err := validateGovernancePolicyTransition(edge[0], edge[1]); err == nil {
			t.Fatalf("%s -> %s accepted", edge[0], edge[1])
		}
	}
	for _, status := range []string{"APPROVED", "PUBLISHED", "RETIRED"} {
		if err := governancePolicyReservedStatus(status); err == nil {
			t.Fatalf("%s must be reserved", status)
		}
	}
	if err := governancePolicyReservedStatus(""); err != nil {
		t.Fatal(err)
	}
	if err := governancePolicyReservedStatus("DRAFT"); err != nil || governancePolicyReservedStatus("IN_REVIEW") != nil {
		t.Fatal("generic statuses must stay on the update route")
	}
}

func TestGovernancePolicyFilterValidation(t *testing.T) {
	f := GovernancePolicyFilter{Status: " published "}
	normalizeGovernancePolicyFilter(&f)
	if err := validateGovernancePolicyFilter(f); err != nil || f.Status != "PUBLISHED" {
		t.Fatalf("filter: %v %+v", err, f)
	}
	f.Status = "REOPENED"
	if err := validateGovernancePolicyFilter(f); err == nil {
		t.Fatal("invalid status accepted")
	}
	zero := uint(0)
	if err := validateGovernancePolicyFilter(GovernancePolicyFilter{FrameworkID: &zero}); err == nil {
		t.Fatal("zero framework filter accepted")
	}
}
