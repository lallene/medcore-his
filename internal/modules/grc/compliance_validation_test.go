package grc

import (
	"reflect"
	"testing"
)

type statusValidationCase struct {
	name    string
	valid   func(string) bool
	allowed []string
	unknown []string
}

func complianceStatusValidationCases() []statusValidationCase {
	return []statusValidationCase{
		{
			name:  "framework",
			valid: isValidFrameworkStatus,
			allowed: []string{
				FrameworkStatusDraft,
				FrameworkStatusActive,
				FrameworkStatusArchived,
			},
			unknown: []string{"", "PUBLISHED", "INACTIVE", "draft"},
		},
		{
			name:  "requirement",
			valid: isValidRequirementStatus,
			allowed: []string{
				RequirementStatusActive,
				RequirementStatusInactive,
			},
			unknown: []string{"", "DRAFT", "RETIRED", "active"},
		},
		{
			name:  "control",
			valid: isValidControlStatus,
			allowed: []string{
				ControlStatusDraft,
				ControlStatusActive,
				ControlStatusInactive,
				ControlStatusRetired,
			},
			unknown: []string{"", "ARCHIVED", "COMPLIANT", "active"},
		},
		{
			name:  "control implementation",
			valid: isValidControlImplementationStatus,
			allowed: []string{
				ControlImplementationStatusPlanned,
				ControlImplementationStatusImplemented,
				ControlImplementationStatusPartial,
				ControlImplementationStatusNotImplemented,
				ControlImplementationStatusNotApplicable,
			},
			unknown: []string{"", "DONE", "COMPLIANT", "NON_COMPLIANT", "planned"},
		},
	}
}

func TestComplianceStatusSetsMatchConstants(t *testing.T) {
	sets := map[string]struct {
		set     map[string]bool
		allowed []string
	}{
		"framework": {validFrameworkStatuses, []string{
			FrameworkStatusDraft, FrameworkStatusActive, FrameworkStatusArchived,
		}},
		"requirement": {validRequirementStatuses, []string{
			RequirementStatusActive, RequirementStatusInactive,
		}},
		"control": {validControlStatuses, []string{
			ControlStatusDraft, ControlStatusActive, ControlStatusInactive, ControlStatusRetired,
		}},
		"control implementation": {validControlImplementationStatuses, []string{
			ControlImplementationStatusPlanned,
			ControlImplementationStatusImplemented,
			ControlImplementationStatusPartial,
			ControlImplementationStatusNotImplemented,
			ControlImplementationStatusNotApplicable,
		}},
	}

	for name, tc := range sets {
		if len(tc.set) != len(tc.allowed) {
			t.Fatalf("%s statuses: got %d values, want %d", name, len(tc.set), len(tc.allowed))
		}
		for _, value := range tc.allowed {
			if !tc.set[value] {
				t.Fatalf("%s statuses: missing %q", name, value)
			}
		}
	}
}

func TestComplianceStatusValidationAcceptsAllowedValues(t *testing.T) {
	for _, tc := range complianceStatusValidationCases() {
		for _, value := range tc.allowed {
			if !tc.valid(value) {
				t.Fatalf("%s status %q must be accepted", tc.name, value)
			}
		}
	}
}

func TestComplianceStatusValidationRejectsUnknownValues(t *testing.T) {
	for _, tc := range complianceStatusValidationCases() {
		for _, value := range tc.unknown {
			if tc.valid(value) {
				t.Fatalf("%s status %q must be rejected", tc.name, value)
			}
		}
	}
}

func TestComplianceStatusValidationAfterNormalizeEnum(t *testing.T) {
	cases := []struct {
		valid func(string) bool
		input string
	}{
		{isValidFrameworkStatus, " archived "},
		{isValidRequirementStatus, " inactive "},
		{isValidControlStatus, " retired "},
		{isValidControlImplementationStatus, " not_applicable "},
	}

	for _, tc := range cases {
		if tc.valid(tc.input) {
			t.Fatalf("raw status %q must be rejected before normalization", tc.input)
		}
		if !tc.valid(normalizeEnum(tc.input)) {
			t.Fatalf("normalized status %q must be accepted", normalizeEnum(tc.input))
		}
	}
}

func TestComplianceRelationRequestsExposeOnlyIDs(t *testing.T) {
	cases := []struct {
		dto  any
		want []string
	}{
		{RequirementControlRequest{}, []string{"RequirementID", "ControlID"}},
		{ControlRiskRequest{}, []string{"ControlID", "RiskID"}},
	}

	for _, tc := range cases {
		typ := reflect.TypeOf(tc.dto)
		if typ.NumField() != len(tc.want) {
			t.Fatalf("%s: got %d fields, want %v", typ.Name(), typ.NumField(), tc.want)
		}
		for i, name := range tc.want {
			field := typ.Field(i)
			if field.Name != name || field.Type.Kind() != reflect.Uint {
				t.Fatalf("%s field %d = %s %s, want %s uint", typ.Name(), i, field.Name, field.Type, name)
			}
		}
	}
}

func TestComplianceMainRequestsHaveNoRelationSlices(t *testing.T) {
	for _, dto := range []any{
		FrameworkRequest{},
		RequirementRequest{},
		ControlRequest{},
		ControlImplementationRequest{},
	} {
		typ := reflect.TypeOf(dto)
		for i := 0; i < typ.NumField(); i++ {
			if field := typ.Field(i); field.Type.Kind() == reflect.Slice {
				t.Fatalf("%s.%s must not be a slice", typ.Name(), field.Name)
			}
		}
	}
}
