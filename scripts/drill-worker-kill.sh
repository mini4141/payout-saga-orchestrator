#!/usr/bin/env bash
# Drill (a): kill the worker while payouts are inside SendBankTransfer. After the worker restarts,
# every payout resumes and completes exactly once: one bank transfer and one debit per payout.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
log "== drill: kill the worker during SendBankTransfer =="

mock_reset
# The bank records each transfer, then holds the reply for 12 s (longer than the 10 s activity timeout),
# so killing the worker now loses the response of a transfer that already went out.
mock_cfg bank '{"timeout_next":10}'

merchants=(m_acme m_bluebird m_cobalt m_delta m_ember)
ids=()
for i in $(seq 0 9); do ids+=("$(create_payout "${merchants[$((i % 5))]}" $((20000 + 1000 * i)))"); done
log "created ${#ids[@]} payouts: ${ids[*]}"

log "waiting until transfers are in flight at the bank"
for _ in $(seq 1 60); do
  inflight=$(mock_state | jq -r '.bank_inflight'); ((inflight > 0)) && break; sleep 0.5
done
log "bank in-flight transfers: $inflight; killing worker NOW"
kill_worker
sleep 4
log "worker is down; payouts in flight:"
psql_q "select id,status from payouts where id in ($(printf "'%s'," "${ids[@]}" | sed 's/,$//')) order by id"
log "restarting worker"
start_worker

wait_terminal "${ids[@]}"
list=$(printf "'%s'," "${ids[@]}"); list="${list%,}"
log "final statuses:"
psql_q "select status, count(*) from payouts where id in ($list) group by status"
log "SendBankTransfer attempts per payout (a 'lost' attempt is the one the killed worker never finished):"
psql_q "select payout_id, string_agg(attempt || ':' || outcome, ' ' order by id) from step_attempts where step='SendBankTransfer' and payout_id in ($list) group by payout_id order by payout_id"

state=$(mock_state)
log "bank state: $(echo "$state" | jq -c '{bank_transfers, bank_settled, idempotent_replays, calls}')"
mock_reset

expect "all 10 payouts SUCCEEDED" "$(psql_q "select count(*) from payouts where id in ($list) and status='SUCCEEDED'")" 10
expect "exactly one debit per payout (no double spend)" "$(psql_q "select count(*) from ledger_entries where kind='debit' and payout_id in ($list)")" 10
expect "payouts with more than one debit" "$(psql_q "select count(*) from (select payout_id from ledger_entries where kind='debit' and payout_id in ($list) group by 1 having count(*)>1) x")" 0
settled_refs=$(curl -fsS "$MOCKS_URL_LOCAL/bank/settled" | jq -r '.transfers[].reference' | grep -cE "$(IFS='|'; echo "${ids[*]}")" || true)
expect "bank settled each of the 10 payouts exactly once" "$settled_refs" 10
replays=$(echo "$state" | jq -r '.idempotent_replays.bank // 0')
expect "retries after the kill were served by the bank's idempotency record (>0)" "$((replays > 0))" 1
finish
