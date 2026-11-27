# Contributing

## Workflow
1. Branch from `main`, use Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `infra:`, `chore:`).
2. `scripts/test-all.sh` must pass; CI runs the cloud-free layers.
3. Open a PR; the template carries the review checklist.

## Code review checklist
- **Money**: ledger changes only through `internal/ledger` (reserve / capture / release); the invariant `balance = sum(entries)` must hold.
- **Idempotency**: a replayed activity returns the stored `step_results` row; external calls send `Idempotency-Key`.
- **Determinism**: workflow code touches no clock, randomness or I/O; use `workflow.GetVersion` for changes to running workflows.
- **Failure paths**: each side-effecting step has a compensation, and a drill or test that exercises it.
- **Outbox**: events are inserted in the same transaction as the state change.
- **Operability**: structured logs (`slog`), metrics, and a health endpoint for every service.
- **Design changes**: update `docs/design/0001-payout-saga.md` or add an ADR in `docs/adr/`.
