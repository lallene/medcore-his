package examcategories

import "testing"

func TestIsLaboratoryMatchesCanonicalList(t *testing.T) {
	t.Parallel()
	for _, c := range []string{"Laboratoire", "BIOLOGIE", " Hématologie "} {
		if !IsLaboratory(c) {
			t.Fatalf("%q should be laboratory", c)
		}
	}
	if IsLaboratory("Imagerie") || IsLaboratory("") || IsLaboratory("Cardiologie") {
		t.Fatal("non-lab categories accepted")
	}
}

func TestIsImaging(t *testing.T) {
	t.Parallel()
	if !IsImaging("Imagerie") || !IsImaging("imagerie") || !IsImaging(" IMAGERIE ") {
		t.Fatal("imaging category rejected")
	}
	if IsImaging("Laboratoire") || IsImaging("") {
		t.Fatal("non-imaging accepted")
	}
}
