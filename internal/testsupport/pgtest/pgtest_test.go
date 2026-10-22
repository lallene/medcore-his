package pgtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A′-PG06: with MEDCORE_REQUIRE_POSTGRES_TESTS=1 an absent DSN fails instead of skipping.
func TestDSNFailsWhenRequiredAndAbsent(t *testing.T) {
	if os.Getenv("PGTEST_CHILD") == "1" {
		DSN(t)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDSNFailsWhenRequiredAndAbsent$", "-test.v")
	cmd.Env = append(filteredEnv(), "PGTEST_CHILD=1", EnvRequire+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("required + absent DSN must fail, got success:\n%s", out)
	}
	if !strings.Contains(string(out), "--- FAIL") || strings.Contains(string(out), "--- SKIP") {
		t.Fatalf("expected FAIL not SKIP:\n%s", out)
	}
}

func TestDSNSkipsLocallyWhenAbsent(t *testing.T) {
	if os.Getenv("PGTEST_CHILD") == "1" {
		DSN(t)
		t.Fatal("unreachable after skip")
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDSNSkipsLocallyWhenAbsent$", "-test.v")
	cmd.Env = append(filteredEnv(), "PGTEST_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- SKIP") {
		t.Fatalf("expected local skip: err=%v\n%s", err, out)
	}
}

func TestDSNRecordsEvidenceWithoutValue(t *testing.T) {
	evidence := filepath.Join(t.TempDir(), "evidence.txt")
	t.Setenv(EnvDSN, "postgres://user:secret@127.0.0.1:5432/db")
	t.Setenv(EnvEvidenceFile, evidence)
	if got := DSN(t); got == "" {
		t.Fatal("dsn")
	}
	raw, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatal(err)
	}
	line := string(raw)
	if !strings.Contains(line, "\tTestDSNRecordsEvidenceWithoutValue") {
		t.Fatalf("evidence line %q", line)
	}
	if strings.Contains(line, "secret") || strings.Contains(line, "postgres://") {
		t.Fatal("evidence must never contain the DSN")
	}
}

func filteredEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, EnvDSN+"=") || strings.HasPrefix(kv, EnvRequire+"=") || strings.HasPrefix(kv, EnvEvidenceFile+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
