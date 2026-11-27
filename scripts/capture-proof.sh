#!/usr/bin/env bash
# Capture the proof for a phase into proof/<phase>/. Reproducible: run it again to regenerate.
#   scripts/capture-proof.sh phase1   local: fresh stack, all tests and drills, logs, screenshots
#   scripts/capture-proof.sh phase2   cloud: run after scripts/deploy.sh (see scripts/capture-cloud.sh)
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

PHASE="${1:?usage: capture-proof.sh phase1|phase2}"

ensure_playwright() {
  if [[ ! -d "$ROOT/.local/pw/node_modules/playwright" ]]; then
    log "installing Playwright into .local/pw"
    mkdir -p "$ROOT/.local/pw"
    (cd "$ROOT/.local/pw" && { [[ -f package.json ]] || npm init -y >/dev/null; } && npm i playwright@latest >/dev/null && npx playwright install chromium)
  fi
}

phase1() {
  local P="proof/phase1-local"
  mkdir -p "$P/screenshots" "$P/logs" "$P/tests"
  log "fresh stack (drops the local database)"
  bash "$ROOT/scripts/dev-down.sh" --volumes >/dev/null 2>&1 || true

  bash "$ROOT/scripts/test-all.sh" # starts the stack, runs every layer and the drills, writes tests/ and logs/

  log "service logs, metrics and API examples"
  for s in api worker relay mocks receiver ops-ui; do $DC logs --no-color "$s" >"$P/logs/service-$s.log" 2>&1 || true; done
  curl -fsS "$API_URL/metrics" | grep -E '^(api_requests_total|# HELP api_)' >"$P/logs/metrics-api.txt" || true
  docker compose exec -T worker curl -fsS http://localhost:9090/metrics 2>/dev/null | grep -E '^payout_' >"$P/logs/metrics-worker.txt" || true
  docker compose exec -T relay curl -fsS http://localhost:8096/metrics 2>/dev/null | grep -E '^outbox_' >"$P/logs/metrics-relay.txt" || true
  {
    echo '$ buf curl (Connect protocol, schema from ./proto)'
    buf curl --protocol connect --http2-prior-knowledge --schema proto -H "Authorization: Bearer $API_TOKEN" \
      -d '{"limit":3}' "$API_URL/payout.v1.PayoutService/ListPayouts"
    echo
    echo '$ grpcurl (gRPC over h2c, server reflection)'
    grpcurl -plaintext -H "Authorization: Bearer $API_TOKEN" -d '{"limit":2}' "${API_URL#http://}" payout.v1.PayoutService/ListPayouts
    echo '$ grpcurl list'
    grpcurl -plaintext "${API_URL#http://}" list
  } >"$P/logs/connect-examples.txt" 2>&1 || true

  log "screenshots"
  ensure_playwright
  local retry comp
  retry=$(psql_q "select payout_id from step_attempts where step='SendBankTransfer' and outcome='error' group by payout_id having count(*)>=2 order by max(id) desc limit 1")
  comp=$(psql_q "select id from payouts where status='COMPENSATED' order by created_at desc limit 1")
  # Show a non-default injection setup on the inject page, then reset it.
  mock_cfg bank '{"fail_rate":0.1,"latency_ms":500,"reject_next":2,"timeout_next":1}'
  mock_cfg fx '{"latency_ms":200}'
  node scripts/screenshots.mjs "$P/screenshots" "$OPS_UI_URL" "$TEMPORAL_UI_URL_LOCAL" "$retry" "$comp" local
  mock_reset
  log "phase1 proof written to $P"
}

case "$PHASE" in
  phase1) phase1 ;;
  phase2) bash "$ROOT/scripts/capture-cloud.sh" ;;
  *) echo "unknown phase $PHASE" >&2; exit 2 ;;
esac
