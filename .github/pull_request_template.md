## What and why

## Review checklist
- [ ] Every new activity is idempotent (keyed by `(payout_id, step)`) and external calls carry an idempotency key
- [ ] State change and outbox event are written in one transaction
- [ ] Workflow code is deterministic (no time, rand, I/O) and changes are versioned (`workflow.GetVersion`)
- [ ] Compensation covers any new step that has a side effect
- [ ] `scripts/check-invariants.sh` still passes after the drills
- [ ] Docs / failure-mode table updated
