#!/usr/bin/env bash
# LOT30-A′ — Financial PostgreSQL Release Gate (fail-closed).
#
# Runs the PostgreSQL suites that carry MedCore financial / clinical-financial
# invariants (concurrency, idempotency, numbering, constraints) with
# MEDCORE_REQUIRE_POSTGRES_TESTS=1, then verifies with cmd/pgtest-gate that each
# required package really executed PostgreSQL tests. TEST_DATABASE_URL is never printed.
set -euo pipefail

cd "$(dirname "$0")/../.."

if [ -z "${TEST_DATABASE_URL:-}" ]; then
	echo "TEST_DATABASE_URL: ABSENT — financial PostgreSQL gate cannot run"
	exit 1
fi
case "$TEST_DATABASE_URL" in
*@127.0.0.1:* | *@localhost:* | *@\[::1\]:* | *host=/*) echo "TEST_DATABASE_URL: PRESENT (local/isolated host)" ;;
*) echo "TEST_DATABASE_URL: PRESENT (non-local host)" ;;
esac

out_dir="${PG_GATE_OUT_DIR:-$(mktemp -d)}"
mkdir -p "$out_dir"
events="$out_dir/financial-pg-events.json"
evidence="$out_dir/financial-pg-evidence.txt"
: >"$evidence"

required="modules/billing,modules/cash,modules/performed_acts,modules/insurance/authorization,modules/insurance_receivables,modules/receivables,modules/pharmacy,modules/laboratory,modules/imaging,modules/hospitalizations,modules/consultations,cmd/migrate"

packages=(
	./internal/modules/billing/
	./internal/modules/cash/
	./internal/modules/performed_acts/
	./internal/modules/insurance/...
	./internal/modules/insurance_receivables/
	./internal/modules/receivables/
	./internal/modules/pharmacy/
	./internal/modules/laboratory/
	./internal/modules/imaging/
	./internal/modules/hospitalizations/
	./internal/modules/consultations/
	./internal/core/rbac/
	./internal/testsupport/...
	./cmd/migrate/
	./cmd/pgtest-gate/
)

set +e
MEDCORE_REQUIRE_POSTGRES_TESTS=1 MEDCORE_PG_EVIDENCE_FILE="$evidence" \
	go test -count=1 -p 2 -json "${packages[@]}" >"$events"
test_status=$?

go run ./cmd/pgtest-gate -events "$events" -evidence "$evidence" -require "$required"
gate_status=$?
set -e

if [ "$test_status" -ne 0 ]; then
	echo "go test exit status $test_status — failing tests:"
	grep -E '"Action":"fail"' "$events" | grep -oE '"Package":"[^"]*","Test":"[^"]*"' | sort -u | head -50 || true
	exit "$test_status"
fi
exit "$gate_status"
