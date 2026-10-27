package backuprehearsal

import "testing"

func TestForbiddenDatabaseNames(t *testing.T) {
	for _, name := range []string{ForbiddenMedcore, ForbiddenMedcoreHIS} {
		err := CheckNames(name, "medcore_restore_test_1000000000", "medcore_restore_corrupt_1000000000")
		if err == nil {
			t.Fatalf("%s was accepted as a source", name)
		}
		err = CheckNames("medcore_b03_source_1000000000", name, "medcore_restore_corrupt_1000000000")
		if err == nil {
			t.Fatalf("%s was accepted as a restore target", name)
		}
	}
}

func TestMissingDatabaseNameRefuses(t *testing.T) {
	if err := CheckNames("", "medcore_restore_test_1000000000", "medcore_restore_corrupt_1000000000"); err == nil {
		t.Fatal("missing source was accepted")
	}
	if err := CheckNames("medcore_b03_source_1000000000", "", "medcore_restore_corrupt_1000000000"); err == nil {
		t.Fatal("missing restore target was accepted")
	}
}

func TestRestoreTargetCollisionRefuses(t *testing.T) {
	err := CheckNames(
		"medcore_b03_source_1000000000",
		"medcore_b03_source_1000000000",
		"medcore_restore_corrupt_1000000000",
	)
	if err == nil {
		t.Fatal("source and restore collision was accepted")
	}
}

func TestSafeRehearsalNamesProceed(t *testing.T) {
	err := CheckNames(
		"medcore_b03_source_1000000000",
		"medcore_restore_test_1000000000",
		"medcore_restore_corrupt_1000000000",
	)
	if err != nil {
		t.Fatal(err)
	}
}
