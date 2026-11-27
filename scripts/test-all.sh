#!/usr/bin/env bash
# Run every test layer. Exits non-zero on the first failing layer.
#   scripts/test-all.sh          local: needs Docker; starts the stack, runs unit, integration, e2e and the failure drills
#   scripts/test-all.sh --ci     cloud-free layers only (Postgres from TEST_DATABASE_URL); no stack, no drills
# Reports are written to proof/phase1-local/tests and proof/phase1-local/logs.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

MODE="${1:-local}"
# Relative to $ROOT (lib.sh cds there): native Windows tools do not understand /c/... paths.
TESTS="proof/phase1-local/tests"
LOGS="proof/phase1-local/logs"
mkdir -p "$TESTS" "$LOGS"

log "[1/5] buf lint + generated code check + go vet + terraform fmt"
buf lint
buf generate
git diff --exit-code --stat gen >/dev/null || { log "gen/ is stale: commit the result of 'buf generate'"; exit 1; }
go vet ./...
if command -v terraform >/dev/null && [[ -d infra ]]; then terraform -chdir=infra fmt -check -recursive; fi

if [[ "$MODE" != "--ci" ]]; then
  log "[2/5] starting the local stack"
  bash "$ROOT/scripts/dev-up.sh"
  export TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://payout:payout@localhost:15432/payout?sslmode=disable}"
  export E2E_API_URL="${E2E_API_URL:-$API_URL}"
else
  log "[2/5] --ci: stack not started; TEST_DATABASE_URL=${TEST_DATABASE_URL:+set}"
fi

log "[3/5] go test (Temporal testsuite, ledger + replay integration, Lambda handlers, ASL, e2e smoke)"
set +e
go test -count=1 -v ./... 2>&1 | tee "$TESTS/go-test.log" | go-junit-report -out "$TESTS/junit.xml" -iocopy >/dev/null
rc=${PIPESTATUS[0]}
set -e
grep -E '^(--- |ok|FAIL|PASS)' "$TESTS/go-test.log" | tail -40
((rc == 0)) || { log "go test FAILED (see $TESTS/go-test.log)"; exit "$rc"; }

if [[ "$MODE" == "--ci" ]]; then log "ALL CLOUD-FREE TESTS PASSED"; exit 0; fi

log "[4/5] failure drills (each asserts its own outcome)"
bash "$ROOT/scripts/drill-bank-reject.sh" 2>&1 | tee "$LOGS/drill-bank-reject.log"
bash "$ROOT/scripts/drill-bank-timeout.sh" 2>&1 | tee "$LOGS/drill-bank-timeout.log"
bash "$ROOT/scripts/drill-worker-kill.sh" 2>&1 | tee "$LOGS/drill-worker-kill.log"
bash "$ROOT/scripts/drill-recon.sh" 2>&1 | tee "$LOGS/drill-recon.log"

log "[5/5] ledger invariants after all drills"
bash "$ROOT/scripts/check-invariants.sh" 2>&1 | tee "$LOGS/check-invariants.txt"
log "ALL TESTS PASSED"
