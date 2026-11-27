#!/usr/bin/env bash
# Assert the money invariants against the live local stack. Exit non-zero on any violation.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
log "== invariant check =="

expect "balance = sum(entries) for every merchant (violations)" \
  "$(psql_q "select count(*) from merchants m where m.balance_minor <> coalesce((select sum(case when kind='credit' then amount_minor else -amount_minor end) from ledger_entries e where e.merchant_id=m.id),0)")" 0
expect "balance covers open holds (violations)" \
  "$(psql_q "select count(*) from merchants m where m.balance_minor < coalesce((select sum(amount_minor) from holds h where h.merchant_id=m.id),0)")" 0
expect "no payout stuck in PENDING/PROCESSING" \
  "$(psql_q "select count(*) from payouts where status in ('PENDING','PROCESSING')")" 0
expect "no hold left behind for a finished payout" \
  "$(psql_q "select count(*) from holds h join payouts p on p.id=h.payout_id where p.status in ('SUCCEEDED','COMPENSATED')")" 0
expect "every SUCCEEDED payout has exactly one debit entry (violations)" \
  "$(psql_q "select count(*) from payouts p where p.status='SUCCEEDED' and (select count(*) from ledger_entries e where e.payout_id=p.id and e.kind='debit') <> 1")" 0
expect "no COMPENSATED payout was debited" \
  "$(psql_q "select count(*) from payouts p join ledger_entries e on e.payout_id=p.id where p.status='COMPENSATED' and e.kind='debit'")" 0

# Bank side: each SUCCEEDED payout settled exactly once, nothing else settled, no duplicate references.
settled=$(curl -fsS "$MOCKS_URL_LOCAL/bank/settled" | jq -r '.transfers[] | select(.transfer_id | startswith("txn_seed") | not) | .reference' | sort)
succeeded=$(psql_q "select id from payouts where status='SUCCEEDED' order by id" | sort)
expect "bank settled references == SUCCEEDED payouts (no duplicates, none missing)" "$(echo "$settled" | md5sum | cut -c1-32)" "$(echo "$succeeded" | md5sum | cut -c1-32)"
expect "bank settled count" "$(echo "$settled" | sed '/^$/d' | wc -l | tr -d ' ')" "$(psql_q "select count(*) from payouts where status='SUCCEEDED'")"

# FX: every compensated payout cancelled its quote.
cancelled=$(mock_state | jq -r '.fx_quotes_cancelled')
expect "FX quotes cancelled == CancelFX compensations run" "$cancelled" "$(psql_q "select count(*) from step_results where step='CancelFX'")"

# Outbox: exactly one event per finished payout, all published, receiver saw each exactly once.
for _ in $(seq 1 20); do
  unsent=$(psql_q "select count(*) from outbox where sent_at is null"); [[ "$unsent" == "0" ]] && break; sleep 1
done
expect "outbox rows still unsent" "$unsent" 0
expect "exactly one outbox event per finished payout (violations)" \
  "$(psql_q "select count(*) from payouts p where p.status in ('SUCCEEDED','COMPENSATED') and (select count(*) from outbox o where o.aggregate_id=p.id) <> 1")" 0
delivered=$(curl -fsS "$RECEIVER_URL_LOCAL/delivered" | jq -r '.delivered | length')
expect "receiver delivered events == outbox events" "$delivered" "$(psql_q "select count(*) from outbox")"

finish
