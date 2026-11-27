#!/usr/bin/env bash
# Start the full local stack (postgres, temporal dev server, migrate+seed, mocks, receiver, api, worker, relay, ops-ui).
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

cd "$ROOT"
[[ -f .env ]] || cp .env.example .env 2>/dev/null || true # compose reads .env; the example holds safe local defaults
log "building and starting the stack"
$DC up -d --build --remove-orphans
wait_http "$API_URL/healthz" 120
wait_http "$OPS_UI_URL/healthz" 120
wait_http "$MOCKS_URL_LOCAL/healthz" 60
# Seeding runs inside the migrate service; running it again is a no-op.
log "stack is up"
log "  ops UI      $OPS_UI_URL"
log "  Temporal UI $TEMPORAL_UI_URL_LOCAL"
log "  Connect API $API_URL (Bearer $API_TOKEN)"
