#!/usr/bin/env bash
# Drill (d): reconciliation is clean, then detects a mismatch seeded directly at the bank mock.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
log "== drill: reconciliation detects a seeded mismatch =="

run_recon() { # starts a one-off ReconciliationWorkflow from the ops UI and prints the newest report
  local before after
  before=$(psql_q "select coalesce(max(id),0) from recon_reports")
  curl -sS -X POST "$OPS_UI_URL/recon/run" >/dev/null 2>&1 || true
  for _ in $(seq 1 30); do
    after=$(psql_q "select coalesce(max(id),0) from recon_reports"); ((after > before)) && break; sleep 1
  done
  psql_q "select id,matched,missing,mismatched,details->>'details' from recon_reports order by id desc limit 1"
}

mock_reset
curl -fsS -X POST "$MOCKS_URL_LOCAL/admin/bank/clear-seeded" >/dev/null
log "1) baseline run"
IFS='|' read -r _ matched missing mismatched _ < <(run_recon)
expect "baseline: missing" "$missing" 0
expect "baseline: mismatched" "$mismatched" 0
log "   matched=$matched"

log "2) seed a transfer at the bank that the ledger knows nothing about"
curl -fsS -X POST "$MOCKS_URL_LOCAL/admin/bank/seed-settled" -H 'Content-Type: application/json' \
  -d '{"reference":"po_seeded_mismatch","amount_minor":99999,"currency":"USD"}'; echo
IFS='|' read -r _ _ _ mismatched details < <(run_recon)
log "   report details: $details"
expect "seeded mismatch is reported (mismatched)" "$mismatched" 1

log "3) remove the seeded transfer, reconciliation is clean again"
curl -fsS -X POST "$MOCKS_URL_LOCAL/admin/bank/clear-seeded" >/dev/null
IFS='|' read -r _ _ missing mismatched _ < <(run_recon)
expect "clean again: missing+mismatched" "$((missing + mismatched))" 0
finish
