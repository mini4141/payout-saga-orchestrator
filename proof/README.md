# Proof index

Every artifact is produced by a script (`scripts/capture-proof.sh`, drills, `scripts/deploy.sh`), not by hand.
Logs have 12-digit account IDs, access keys and the deployer's public IP redacted.

## Phase 1: local (`phase1-local/`)

Produced by `scripts/capture-proof.sh phase1` on a fresh Docker Compose stack.

| Artifact | What it proves |
|---|---|
| `tests/junit.xml`, `tests/go-test.log` | Temporal testsuite (happy path, `BankRejected` → compensations in reverse), Postgres integration (hold → capture, release, insufficient funds, idempotent activity replay), Lambda handlers, ASL structure, e2e smoke through Connect |
| `logs/drill-worker-kill.log` | worker killed while 10 payouts were inside `SendBankTransfer`: all 10 end SUCCEEDED, exactly one debit and one bank transfer each; the retries were served by the bank's idempotency record |
| `logs/drill-bank-reject.log` | bank `REJECTED` → `CancelFX` then `Release`, balance unchanged, no debit, `payout.failed` event written |
| `logs/drill-bank-timeout.log` | two bank timeouts (transfer processed, reply lost) → retry with backoff → success, a single settled transfer |
| `logs/drill-recon.log` | reconciliation clean → mismatch seeded at the bank mock is reported → clean again |
| `logs/check-invariants.txt` | after all drills: `balance = sum(entries)`, no leaked holds, one debit per success, bank settled == SUCCEEDED, one outbox event per payout, receiver saw each exactly once |
| `logs/connect-examples.txt` | `buf curl` and `grpcurl` calls against the Connect API |
| `logs/metrics-*.txt` | Prometheus metrics from the api, worker and relay |
| `logs/service-*.log` | structured JSON logs (and OpenTelemetry spans) from each service |
| `screenshots/01-payouts.png` | ops UI payout list with status chips |
| `screenshots/02-payout-timeline-retries.png` | step timeline: two timed-out bank attempts, then success, with the stored idempotency records |
| `screenshots/03-compensated.png` | a COMPENSATED payout: `CancelFX` and `Release` ran |
| `screenshots/04-inject.png` | failure-injection sliders for the three mocks |
| `screenshots/05-recon.png` | reconciliation reports: clean, discrepancy (seeded), clean |
| `screenshots/06-invariants.png` | per-merchant balance vs sum of entries, open holds, outbox counts |
| `screenshots/07-temporal-ui.png` | Temporal UI: payout, notify (child) and reconciliation (cron) workflows |

## Phase 2: AWS (`phase2-cloud/`)

See the section added after the cloud run.

## Phase 3: destroy (`phase3-destroy/`)

See the section added after the destroy run.
