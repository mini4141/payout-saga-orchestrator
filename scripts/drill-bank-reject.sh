#!/usr/bin/env bash
# Drill (b): the bank returns REJECTED. The saga compensates in reverse order: CancelFX, then Release.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
log "== drill: bank rejects the transfer =="

mock_reset
mock_cfg bank '{"reject_next":1}'
before=$(psql_q "select balance_minor from merchants where id='m_bluebird'")
cancelled_before=$(mock_state | jq -r '.fx_quotes_cancelled')

id=$(create_payout m_bluebird 55000)
log "created payout $id"
wait_terminal "$id"
log "attempt timeline:"
psql_q "select id, step, attempt, outcome, left(error,90) from step_attempts where payout_id='$id' order by id"
mock_reset

expect "payout ends COMPENSATED" "$(payout_status "$id")" COMPENSATED
expect "hold released (no hold row)" "$(psql_q "select count(*) from holds where payout_id='$id'")" 0
expect "merchant balance unchanged" "$(psql_q "select balance_minor from merchants where id='m_bluebird'")" "$before"
expect "no debit entry written" "$(psql_q "select count(*) from ledger_entries where payout_id='$id' and kind='debit'")" 0
expect "FX lock cancelled at the mock" "$(( $(mock_state | jq -r '.fx_quotes_cancelled') - cancelled_before ))" 1
order=$(psql_q "select string_agg(step, '>' order by id) from step_attempts where payout_id='$id' and step in ('CancelFX','Release')")
expect "compensations ran in reverse order" "$order" "CancelFX>Release"
expect "bank was not retried after BankRejected (SendBankTransfer attempts)" "$(psql_q "select count(*) from step_attempts where payout_id='$id' and step='SendBankTransfer'")" 1
sleep 2
expect "payout.failed event written to the outbox" "$(psql_q "select count(*) from outbox where aggregate_id='$id' and type='payout.failed'")" 1
finish
