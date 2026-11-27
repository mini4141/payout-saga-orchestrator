# PROGRESS

Resume from here after a restart. Tick items as they are done.

## Phase status
- [x] Phase 0 scaffold
- [ ] Phase 1 local (tests green, CI green, proof index complete)
- [ ] Phase 2 AWS showcase
- [ ] Phase 3 destroy + verify
- [ ] Phase 4 write-up

## Requirement coverage matrix (SPECS §2)
- [ ] Go backend services: data model → schema → deploy → operate
- [ ] Multi-step processes that stay correct and recover (Temporal saga, idempotent activities, compensation)
- [ ] Design proposals, technical docs (`docs/design/0001`, ADRs, failure-mode table)
- [ ] Design and code reviews (PR template + CONTRIBUTING checklist)
- [ ] Reliability over time, tech debt (reconciliation, metrics, `docs/tech-debt.md`)
- [ ] Go + relational DB design (ledger holds, outbox, idempotency)
- [ ] Workflow orchestration: Temporal and Step Functions
- [ ] Payments correctness (invariant checks)
- [ ] Zero to production (full AWS deploy)
- [ ] Stack: Go, ConnectRPC/Protobuf, Temporal, PostgreSQL, AWS, Terraform

## Stack items (CLAUDE.md §2) used in running code
- [ ] Go 1.25
- [ ] ConnectRPC + Protobuf (buf lint/breaking/generate, grpcurl/buf curl examples)
- [ ] Temporal: saga, compensation, signals, queries, child workflow, cron reconciliation
- [ ] AWS Step Functions Standard + Go Lambdas (provided.al2023)
- [ ] PostgreSQL 17 + pgx + sqlc + goose
- [ ] Mock services (FX, compliance, bank) with failure/latency injection admin API
- [ ] Ops UI (html/template + htmx)
- [ ] slog JSON logs, OpenTelemetry traces, Prometheus metrics
- [ ] AWS: ECS Fargate, ALB, RDS, SFN + Lambda, EventBridge Scheduler, CloudWatch, ECR
- [ ] Terraform, GitHub Actions
- [ ] Docs: design proposal, failure-mode table, ADR Temporal vs SFN

## Phase 1 proof (SPECS §9)
- [ ] `tests/`: go test JUnit
- [ ] `logs/drill-worker-kill.log`
- [ ] `logs/drill-bank-reject.log`
- [ ] `logs/drill-bank-timeout.log`
- [ ] `logs/drill-recon.log`
- [ ] `logs/check-invariants.txt`
- [ ] `screenshots/01-payouts.png`
- [ ] `screenshots/02-payout-timeline-retries.png`
- [ ] `screenshots/03-compensated.png`
- [ ] `screenshots/04-inject.png`
- [ ] `screenshots/05-recon.png`
- [ ] `screenshots/06-invariants.png`
- [ ] `screenshots/07-temporal-ui.png`
- [ ] `scripts/test-all.sh` green
- [ ] CI green on `main`

## Phase 2 proof (SPECS §10)
- [ ] `screenshots/01-cloud-ops-ui.png`
- [ ] `screenshots/02-cloud-timeline.png`
- [ ] `screenshots/03-cloud-invariants.png`
- [ ] `logs/sfn-executions.json`
- [ ] `logs/sfn-history-compensation.json`
- [ ] `logs/cloud-drills.log`
- [ ] `resources.txt`
- [ ] ASL validated with `aws stepfunctions validate-state-machine-definition`

## Phase 3 proof (SPECS §11)
- [ ] `scripts/destroy.sh` run
- [ ] `proof/phase3-destroy/verify-destroyed.txt` shows nothing billable

## Phase 4
- [ ] `docs/PORTFOLIO.md`
- [ ] `README.md` final
- [ ] Everything committed and pushed
