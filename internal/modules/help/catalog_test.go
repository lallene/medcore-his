package help

import (
	"strings"
	"testing"
)

func validEntry(key string) CatalogEntry {
	return CatalogEntry{Key: key, Module: "billing", Page: "/billing", ElementType: ElementField, Label: "Libellé", Short: "Aide courte."}
}

func TestEmbeddedCatalogIsValidAndUnique(t *testing.T) {
	entries, version, err := LoadCatalog()
	if err != nil {
		t.Fatalf("embedded catalog invalid: %v", err)
	}
	if len(entries) < 50 {
		t.Fatalf("catalog unexpectedly small: %d", len(entries))
	}
	if len(version) != 16 {
		t.Fatalf("version %q", version)
	}
	seen := map[string]bool{}
	modules := map[string]int{}
	for _, e := range entries {
		if seen[e.Key] {
			t.Fatalf("duplicate key %s", e.Key)
		}
		seen[e.Key] = true
		modules[e.Module]++
	}
	for _, m := range []string{"consultations", "billing", "cash", "insurance", "queue", "laboratory", "imaging", "pharmacy", "agenda", "tickets", "performed_acts", "administration", "patients", "hospitalizations"} {
		if modules[m] == 0 {
			t.Errorf("module %s has no help entry", m)
		}
	}
	_, again, err := LoadCatalog()
	if err != nil || again != version {
		t.Fatalf("catalog version must be stable: %s vs %s (%v)", version, again, err)
	}
}

func TestValidateCatalogRejectsUnsafeOrInvalidEntries(t *testing.T) {
	cases := map[string]func(*CatalogEntry){
		"bad_key":            func(e *CatalogEntry) { e.Key = "Billing Payment" },
		"key_not_in_module":  func(e *CatalogEntry) { e.Key = "cash.session.x" },
		"unknown_module":     func(e *CatalogEntry) { e.Module = "crm"; e.Key = "crm.x.y" },
		"bad_element_type":   func(e *CatalogEntry) { e.ElementType = "widget" },
		"page_not_route":     func(e *CatalogEntry) { e.Page = "billing" },
		"empty_short":        func(e *CatalogEntry) { e.Short = " " },
		"long_short":         func(e *CatalogEntry) { e.Short = strings.Repeat("a", 241) },
		"unknown_permission": func(e *CatalogEntry) { e.RequiredPermissions = []string{"billing.everything"} },
		"password":           func(e *CatalogEntry) { e.Detailed = "mot de passe: test" },
		"dsn":                func(e *CatalogEntry) { e.Detailed = "postgres://u:p@h/db" },
		"bearer":             func(e *CatalogEntry) { e.Action = "Bearer abcdefghijklmnop" },
		"email":              func(e *CatalogEntry) { e.Detailed = "écrire à jean.dupont@example.org" },
		"phone":              func(e *CatalogEntry) { e.Short = "Appeler le +225 07 07 07 07 07" },
		"secret_assignment":  func(e *CatalogEntry) { e.Detailed = "client_secret=xyz" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := validEntry("billing.payment.method")
			mutate(&e)
			if err := ValidateCatalog([]CatalogEntry{e}); err == nil {
				t.Fatalf("%s must be rejected", name)
			}
		})
	}
	if err := ValidateCatalog([]CatalogEntry{validEntry("billing.a.b"), validEntry("billing.a.b")}); err == nil {
		t.Fatal("duplicate keys must be rejected")
	}
	if err := ValidateCatalog([]CatalogEntry{validEntry("billing.a.b")}); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
}

func TestToEntryNormalizesPermissions(t *testing.T) {
	e := validEntry("billing.a.b")
	e.RequiredPermissions = []string{"billing.read", "billing.create"}
	row := e.toEntry()
	if row.RequiredPermissions != "billing.create,billing.read" || !row.IsActive {
		t.Fatalf("row=%+v", row)
	}
	if got := row.Permissions(); len(got) != 2 || got[0] != "billing.create" {
		t.Fatalf("permissions=%v", got)
	}
	if (Entry{}).Permissions() != nil {
		t.Fatal("empty permissions must mean public")
	}
}
