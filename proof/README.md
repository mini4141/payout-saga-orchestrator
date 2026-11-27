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

Produced by `scripts/deploy.sh` (infra, images, migrations, drills, Step Functions drill) and `scripts/capture-proof.sh phase2`.
Account IDs and the deployer IP are redacted. Everything was destroyed right afterwards (Phase 3).

| Artifact | What it proves |
|---|---|
| `resources.txt` | deployed resources: 7 ECS Fargate services, ALB, RDS `db.t4g.micro` (private), 9 Lambdas (arm64, `provided.al2023`), Step Functions state machine, EventBridge schedule, ECR images, tagged-resource list |
| `tests/asl-validate.json` | `aws stepfunctions validate-state-machine-definition` on the deployed definition: `OK`, no diagnostics |
| `tests/e2e-cloud.log`, `tests/e2e-cloud-junit.xml` | the e2e smoke test (create payout through Connect, repeat the idempotency key, poll to SUCCEEDED) against the ALB |
| `logs/cloud-drills.log` | the Temporal drills run against the cloud: bank reject, bank timeout, worker kill (ECS `stop-task`), reconciliation with a seeded mismatch, and the invariant check |
| `logs/sfn-drill.log`, `logs/sfn-executions.json` | five Step Functions executions with injected failures and their outcomes |
| `logs/sfn-history-compensation.json` | `get-execution-history` of the bank-rejected execution: `Catch` into `CompensateCancelFX` → `CompensateRelease` → `FinalizeCompensated` |
| `logs/sfn-history-bank-timeout-retry.json`, `sfn-history-fx-errors-retry.json` | histories showing `LambdaFunctionFailed` (`Transient`) followed by the retried attempts and success |
| `logs/sfn-history-*.json` | the remaining execution histories (happy path, compliance blocked) |
| `logs/eventbridge-recon.log` | the reconciliation Lambda invoked every 5 minutes by EventBridge Scheduler (the first run predates the migration and fails, later runs report matched counts) |
| `logs/ecs-service-sample.log` | structured logs of the ECS services from CloudWatch |
| `screenshots/01-cloud-ops-ui.png` | cloud ops UI: Temporal and Step Functions payouts, SUCCEEDED and COMPENSATED |
| `screenshots/02-cloud-timeline.png` | a Step Functions payout's step timeline: two timed-out bank attempts, then success |
| `screenshots/03-cloud-invariants.png` | ledger invariants in the cloud |
| `screenshots/04-cloudwatch-sfn-lambda.png` | CloudWatch graph rendered from the CLI: executions, bank Lambda errors, recon Lambda invocations |

## Phase 3: destroy (`phase3-destroy/`)

| Artifact | What it proves |
|---|---|
| `verify-destroyed.txt` | state machines, Lambdas, ECS cluster, ALB, RDS, EventBridge schedules, ECR, log groups, VPC, NAT gateways and everything tagged `Project=payout-saga-orchestrator` are gone |
