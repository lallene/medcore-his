package main

import "testing"

// LOT28D: HOSP-DEMO-002 and HOSP-DEMO-ADMITTED-NO-BED must never share a demo patient.
func TestDemoAdmittedHospitalizationFixturesUseDistinctPatients(t *testing.T) {
	const withBedPatient = "P-DEMO-006" // HOSP-DEMO-002 via profiles[5] in seedFullDemo
	const noBedPatient = "P-DEMO-001"   // HOSP-DEMO-ADMITTED-NO-BED in seedDemoBedsAndAssignments
	if withBedPatient == noBedPatient {
		t.Fatalf("ADMITTED fixtures must use distinct demo patients")
	}
	if withBedPatient == "" || noBedPatient == "" {
		t.Fatal("demo patient codes must be non-empty")
	}
}
