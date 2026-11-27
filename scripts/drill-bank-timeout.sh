#!/usr/bin/env bash
# Drill (c): the bank times out twice (transfer processed, reply lost). The activity retries with
# backoff and then succeeds; the bank's idempotency key guarantees a single transfer.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
log "== drill: bank timeout, retry with backoff, then success =="

mock_reset
mock_cfg bank '{"timeout_next":2}'
id=$(create_payout m_cobalt 33000)
log "created payout $id (takes ~25 s: two 9 s client timeouts plus 1 s and 2 s backoff)"
wait_terminal "$id"
log "attempt timeline:"
psql_q "select id, step, attempt, outcome, left(error,100), duration_ms from step_attempts where payout_id='$id' and step='SendBankTransfer' order by id"
state=$(mock_state)
mock_reset

expect "payout SUCCEEDED" "$(payout_status "$id")" SUCCEEDED
expect "SendBankTransfer attempts (2 timeouts + 1 success)" "$(psql_q "select count(*) from step_attempts where payout_id='$id' and step='SendBankTransfer'")" 3
expect "failed attempts" "$(psql_q "select count(*) from step_attempts where payout_id='$id' and step='SendBankTransfer' and outcome='error'")" 2
expect "bank holds exactly one settled transfer for the payout" "$(curl -fsS "$MOCKS_URL_LOCAL/bank/settled" | jq -r --arg id "$id" '[.transfers[] | select(.reference==$id)] | length')" 1
expect "retries were idempotent replays at the bank (>=2)" "$(( $(echo "$state" | jq -r '.idempotent_replays.bank // 0') >= 2 ))" 1
expect "exactly one debit" "$(psql_q "select count(*) from ledger_entries where payout_id='$id' and kind='debit'")" 1
finish
