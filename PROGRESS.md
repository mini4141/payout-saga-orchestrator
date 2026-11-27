# PROGRESS

Resume from here after a restart. Tick items as they are done.

## Phase status
- [x] Phase 0 scaffold
- [x] Phase 1 local (tests green, CI green, proof index complete)
- [x] Phase 2 AWS showcase
- [ ] Phase 3 destroy + verify
- [ ] Phase 4 write-up

## Requirement coverage matrix (SPECS §2)
- [x] Go backend services: data model → schema → deploy → operate
- [x] Multi-step processes that stay correct and recover (Temporal saga, idempotent activities, compensation)
- [x] Design proposals, technical docs (`docs/design/0001`, ADRs, failure-mode table)
- [x] Design and code reviews (PR template + CONTRIBUTING checklist)
- [x] Reliability over time, tech debt (reconciliation, metrics, `docs/tech-debt.md`)
- [x] Go + relational DB design (ledger holds, outbox, idempotency)
- [x] Workflow orchestration: Temporal and Step Functions
- [x] Payments correctness (invariant checks)
- [x] Zero to production (full AWS deploy)
- [x] Stack: Go, ConnectRPC/Protobuf, Temporal, PostgreSQL, AWS, Terraform

## Stack items (CLAUDE.md §2) used in running code
- [x] Go 1.25
- [x] ConnectRPC + Protobuf (buf lint/breaking/generate, grpcurl/buf curl examples)
- [x] Temporal: saga, compensation, signals, queries, child workflow, cron reconciliation
- [x] AWS Step Functions Standard + Go Lambdas (provided.al2023)
- [x] PostgreSQL 17 + pgx + sqlc + goose
- [x] Mock services (FX, compliance, bank) with failure/latency injection admin API
- [x] Ops UI (html/template + htmx)
- [x] slog JSON logs, OpenTelemetry traces, Prometheus metrics
- [x] AWS: ECS Fargate, ALB, RDS, SFN + Lambda, EventBridge Scheduler, CloudWatch, ECR
- [x] Terraform, GitHub Actions
- [x] Docs: design proposal, failure-mode table, ADR Temporal vs SFN

## Phase 1 proof (SPECS §9)
- [x] `tests/`: go test JUnit
- [x] `logs/drill-worker-kill.log`
- [x] `logs/drill-bank-reject.log`
- [x] `logs/drill-bank-timeout.log`
- [x] `logs/drill-recon.log`
- [x] `logs/check-invariants.txt`
- [x] `screenshots/01-payouts.png`
- [x] `screenshots/02-payout-timeline-retries.png`
- [x] `screenshots/03-compensated.png`
- [x] `screenshots/04-inject.png`
- [x] `screenshots/05-recon.png`
- [x] `screenshots/06-invariants.png`
- [x] `screenshots/07-temporal-ui.png`
- [x] `scripts/test-all.sh` green
- [x] CI green on `main`

## Phase 2 proof (SPECS §10)
- [x] `screenshots/01-cloud-ops-ui.png`
- [x] `screenshots/02-cloud-timeline.png`
- [x] `screenshots/03-cloud-invariants.png`
- [x] `logs/sfn-executions.json`
- [x] `logs/sfn-history-compensation.json`
- [x] `logs/cloud-drills.log`
- [x] `resources.txt`
- [x] ASL validated with `aws stepfunctions validate-state-machine-definition`

## Phase 3 proof (SPECS §11)
- [ ] `scripts/destroy.sh` run
- [ ] `proof/phase3-destroy/verify-destroyed.txt` shows nothing billable

## Phase 4
- [ ] `docs/PORTFOLIO.md`
- [ ] `README.md` final
- [ ] Everything committed and pushed
