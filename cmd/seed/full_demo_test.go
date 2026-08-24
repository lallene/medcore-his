package main

import "testing"

func TestValidateDemoTarget(t *testing.T) {
	tests := []struct {
		name    string
		allowed bool
	}{
		{"medcore_full_demo", true},
		{"medcore_lot12_demo", true},
		{"medcore", false},
		{"postgres", false},
		{"neondb", false},
		{"medcore_neon_production", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateDemoTarget(test.name)
			if test.allowed && err != nil {
				t.Fatalf("expected %s to be allowed: %v", test.name, err)
			}
			if !test.allowed && err == nil {
				t.Fatalf("expected %s to be rejected", test.name)
			}
		})
	}
}

func TestValidateSeedEnvironment(t *testing.T) {
	if err := validateSeedEnvironment("development"); err != nil {
		t.Fatalf("development should be allowed: %v", err)
	}
	for _, value := range []string{"production", "PRODUCTION", " production "} {
		if err := validateSeedEnvironment(value); err == nil {
			t.Fatalf("%q should be rejected", value)
		}
	}
}
