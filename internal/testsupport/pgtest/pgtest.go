// Package pgtest centralizes the PostgreSQL integration-test contract (LOT30-A′).
//
// Local runs keep the historical behavior: TEST_DATABASE_URL absent → t.Skip.
// Release Gate runs set MEDCORE_REQUIRE_POSTGRES_TESTS=1 so an absent database
// is a hard failure, never a silent green, and record per-test evidence that a
// real PostgreSQL DSN was handed to the test (MEDCORE_PG_EVIDENCE_FILE).
//
// The DSN value is never logged.
package pgtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	// EnvDSN is the PostgreSQL test database URL.
	EnvDSN = "TEST_DATABASE_URL"
	// EnvRequire turns a missing EnvDSN into a test failure.
	EnvRequire = "MEDCORE_REQUIRE_POSTGRES_TESTS"
	// EnvEvidenceFile receives one line per test that obtained a DSN.
	EnvEvidenceFile = "MEDCORE_PG_EVIDENCE_FILE"
)

var evidenceMu sync.Mutex

// Required reports whether PostgreSQL suites are mandatory for this run.
func Required() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(EnvRequire)))
	return v == "1" || v == "true" || v == "yes"
}

// DSN returns the PostgreSQL test DSN, skipping (local) or failing (required)
// when it is absent.
func DSN(t testing.TB) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv(EnvDSN))
	if dsn == "" {
		if Required() {
			t.Fatalf("%s absent alors que %s=1 : suite PostgreSQL obligatoire non exécutable", EnvDSN, EnvRequire)
		}
		t.Skipf("%s absent: tests PostgreSQL ignorés", EnvDSN)
	}
	recordEvidence(t)
	return dsn
}

// recordEvidence appends "<test binary>\t<test name>" to EnvEvidenceFile when set.
func recordEvidence(t testing.TB) {
	path := strings.TrimSpace(os.Getenv(EnvEvidenceFile))
	if path == "" {
		return
	}
	evidenceMu.Lock()
	defer evidenceMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("pgtest evidence: %v", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%s\t%s\n", filepath.Base(os.Args[0]), t.Name()); err != nil {
		t.Fatalf("pgtest evidence: %v", err)
	}
}
