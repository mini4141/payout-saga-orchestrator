# ADR 0001: Ledger holds and per-step idempotency records

Status: accepted

## Context
A payout debits the merchant and sends money through an external bank. If we debit first and the bank rejects,
we need a refund. If we send first and the debit fails, we have paid out money we do not have. Retries by the
orchestrator (and by us after a crash) must never repeat a side effect.

## Decision
1. **Holds.** `Reserve` creates a hold (`available = balance − holds`). `Capture` converts the hold into a debit
   after the bank succeeded. `Release` deletes it. A failed payout therefore leaves the ledger untouched.
2. **Idempotency records.** Every activity is keyed by `(payout_id, step)` in `step_results`. A replay returns the
   stored result. Ledger steps write their result in the same transaction as the ledger change.
3. **Idempotency keys on external calls** (`<payout_id>:<step>`), honoured by the mocks.
4. **Outbox** rows are written in the same transaction as the status change.

## Consequences
- Positive: the invariant `balance = sum(entries)` is checkable by one query; failure cases do not need refunds.
- Positive: orchestrator-agnostic. The same guarantees hold under Temporal and Step Functions.
- Negative: one extra table write per step, and `step_results` grows with payouts (archive after reconciliation).
- Negative: a hold ties up funds while the saga runs; stuck sagas (`NEEDS_REVIEW`) must be handled by operators.
