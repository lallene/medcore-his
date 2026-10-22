package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pkg = "github.com/lallene/medcore-his/backend/internal/modules/billing"

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, events, evidence string) (bool, string) {
	t.Helper()
	stats, err := readEvents(writeFile(t, "events.json", events))
	if err != nil {
		t.Fatal(err)
	}
	evPath := filepath.Join(t.TempDir(), "absent.txt")
	if evidence != "" {
		evPath = writeFile(t, "evidence.txt", evidence)
	}
	if err := readEvidence(evPath, stats); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ok := report(&out, stats, []string{"modules/billing"})
	return ok, out.String()
}

// A′-PG06: every PostgreSQL test skipped (no DSN, no evidence) must not pass the gate.
func TestGateFailsClosedWhenAllPostgresTestsSkipped(t *testing.T) {
	events := `{"Action":"skip","Package":"` + pkg + `","Test":"TestPayIdempotency"}
{"Action":"pass","Package":"` + pkg + `","Test":"TestUnitOnly"}
{"Action":"pass","Package":"` + pkg + `"}`
	ok, out := run(t, events, "")
	if ok || !strings.Contains(out, "NO-PG") {
		t.Fatalf("gate must fail without PostgreSQL evidence: ok=%v out=%s", ok, out)
	}
}

func TestGatePassesWhenPostgresTestsExecuted(t *testing.T) {
	events := `{"Action":"pass","Package":"` + pkg + `","Test":"TestPayIdempotency"}
{"Action":"pass","Package":"` + pkg + `"}`
	ok, out := run(t, events, "billing.test\tTestPayIdempotency\n")
	if !ok {
		t.Fatalf("gate must pass: %s", out)
	}
}

func TestGateFailsOnFailedPostgresTest(t *testing.T) {
	events := `{"Action":"fail","Package":"` + pkg + `","Test":"TestPayIdempotency"}
{"Action":"fail","Package":"` + pkg + `"}`
	ok, out := run(t, events, "billing.test\tTestPayIdempotency\n")
	if ok || !strings.Contains(out, "FAILED") {
		t.Fatalf("gate must fail: %s", out)
	}
}

func TestGateFailsWhenRequiredPackageAbsent(t *testing.T) {
	ok, out := run(t, `{"Action":"pass","Package":"other"}`, "")
	if ok || !strings.Contains(out, "MISSING") {
		t.Fatalf("gate must fail on missing package: %s", out)
	}
}

func TestGateFailsWhenEvidenceTestDidNotPass(t *testing.T) {
	events := `{"Action":"skip","Package":"` + pkg + `","Test":"TestPayIdempotency"}
{"Action":"pass","Package":"` + pkg + `"}`
	ok, out := run(t, events, "billing.test\tTestPayIdempotency\n")
	if ok || !strings.Contains(out, "PG-NOT-PASSED") {
		t.Fatalf("gate must fail: %s", out)
	}
}
