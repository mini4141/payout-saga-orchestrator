#!/usr/bin/env bash
# Shared helpers for the local scripts and drills. Source this file; do not run it.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export MSYS_NO_PATHCONV=1 # keep Git Bash from rewriting container paths

API_URL="${API_URL:-http://localhost:18080}"
MOCKS_URL_LOCAL="${MOCKS_URL_LOCAL:-http://localhost:18090}"
RECEIVER_URL_LOCAL="${RECEIVER_URL_LOCAL:-http://localhost:18095}"
OPS_UI_URL="${OPS_UI_URL:-http://localhost:18081}"
TEMPORAL_UI_URL_LOCAL="${TEMPORAL_UI_URL_LOCAL:-http://localhost:18233}"
API_TOKEN="${API_TOKEN:-dev-token}"
cd "$ROOT"
DC="docker compose"

# TARGET=cloud points every helper at the deployed stack (written by scripts/deploy.sh).
TARGET="${TARGET:-local}"
if [[ "$TARGET" == "cloud" && -f .local/cloud.env ]]; then source .local/cloud.env; fi

# jq.exe on Windows ends lines with CRLF; strip the CR so values compare cleanly.
jq() { command jq "$@" | sed 's/[[:cntrl:]]$//'; }

log() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*"; }

# psql <sql> prints tuples-only, unaligned output.
# In the cloud RDS is private, so the same queries go through the ops UI's read-only /debug/sql endpoint.
psql_q() {
  if [[ "$TARGET" == "cloud" ]]; then
    curl -fsS -G "$OPS_UI_URL/debug/sql" -H "Authorization: Bearer $API_TOKEN" --data-urlencode "q=$1"
  else
    $DC exec -T postgres psql -U payout -d payout -At -F '|' -c "$1"
  fi
}

# kill_worker / start_worker: docker compose locally, ECS stop-task in the cloud (the service replaces the task).
kill_worker() {
  if [[ "$TARGET" == "cloud" ]]; then
    local t; t=$(aws ecs list-tasks --cluster payout-saga --service-name worker --query 'taskArns[0]' --output text)
    aws ecs stop-task --cluster payout-saga --task "$t" --reason "worker-kill drill" >/dev/null
  else
    $DC kill worker
  fi
}
start_worker() {
  if [[ "$TARGET" == "cloud" ]]; then
    log "ECS service replaces the stopped worker task; waiting for it to be running"
    for _ in $(seq 1 60); do
      [[ "$(aws ecs describe-services --cluster payout-saga --services worker --query 'services[0].runningCount' --output text)" == "1" ]] && return 0
      sleep 5
    done
  else
    $DC up -d worker
  fi
}

# rpc <Method> <json> calls the Connect API (JSON over HTTP/1.1).
rpc() {
  curl -fsS -X POST "$API_URL/payout.v1.PayoutService/$1" \
    -H 'Content-Type: application/json' -H "Authorization: Bearer $API_TOKEN" -d "$2"
}

# create_payout <merchant> <amount_minor> [key] -> payout id
create_payout() {
  local key="${3:-k-$(date +%s%N)-$RANDOM}"
  rpc CreatePayout "{\"merchantId\":\"$1\",\"amountMinor\":$2,\"currency\":\"AUD\",\"targetCurrency\":\"USD\",\"bankAccount\":\"062-000-1234\",\"idempotencyKey\":\"$key\",\"engine\":\"${ENGINE:-temporal}\"}" |
    sed -E 's/.*"payoutId":"([^"]+)".*/\1/'
}

payout_status() { psql_q "select status from payouts where id='$1'"; }

# wait_terminal <ids...> waits until every payout has left PENDING/PROCESSING (timeout 150 s).
wait_terminal() {
  local deadline=$((SECONDS + 150)) pending list
  list=$(printf "'%s'," "$@"); list="${list%,}"
  while ((SECONDS < deadline)); do
    pending=$(psql_q "select count(*) from payouts where id in ($list) and status in ('PENDING','PROCESSING')")
    [[ "$pending" == "0" ]] && return 0
    sleep 1
  done
  log "TIMEOUT waiting for payouts: $*"
  return 1
}

mock_cfg() { # mock_cfg <service> <json>
  curl -fsS -X POST "$MOCKS_URL_LOCAL/admin/$1/config" -H 'Content-Type: application/json' -d "$2" >/dev/null
}
mock_reset() { curl -fsS -X POST "$MOCKS_URL_LOCAL/admin/reset" >/dev/null; }
mock_state() { curl -fsS "$MOCKS_URL_LOCAL/admin/state"; }

wait_http() { # wait_http <url> <seconds>
  local deadline=$((SECONDS + ${2:-120}))
  until curl -fsS "$1" >/dev/null 2>&1; do
    ((SECONDS < deadline)) || { log "timeout waiting for $1"; return 1; }
    sleep 1
  done
}

# expect <description> <actual> <expected>: PASS/FAIL line, non-zero exit on FAIL.
FAILED=0
expect() {
  if [[ "$2" == "$3" ]]; then log "PASS  $1 (got $2)"; else log "FAIL  $1 (got '$2', want '$3')"; FAILED=1; fi
}
finish() { if ((FAILED)); then log "RESULT: FAIL"; exit 1; else log "RESULT: PASS"; fi; }
