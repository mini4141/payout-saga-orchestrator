# CLAUDE.md — payout-saga-orchestrator

These are the engineering rules for Claude Code on this repository. You work **fully autonomously**. Don't stop to ask the user questions. When something is ambiguous, pick the simplest option that keeps every stack item in §2 covered. Record it in `docs/DECISIONS.md` (one line: date, decision, reason) and keep going. Read `SPECS.md` end to end before writing any code. It is the source of truth for features, architecture and proof.

## 1. Project at a glance

| | |
|---|---|
| Project | Payout Saga Orchestrator: correct, recoverable multi-step payouts with Temporal and AWS Step Functions |
| Repo | public GitHub repo `payout-saga-orchestrator`. `origin` is already configured and the default branch is `main` |
| Target role | Backend Developer, early-stage fintech (Go, PostgreSQL, distributed systems, workflow orchestration) |
| Pitch | Paying a merchant out takes six steps across four systems, and any of them can fail. This Go backend runs the payout as a Temporal saga with idempotent activities, compensation, a transactional outbox and reconciliation. It can prove that killing the worker or breaking the bank mid-flight never loses or duplicates money. The same saga also runs on AWS Step Functions for comparison. |
| Priority | **Breadth of the tech stack beats depth of business logic.** Every technology in §2 must appear in running code and be visible in the proof. Full validation, edge cases and complete domain rules are out of scope. |

## 2. Mandatory tech stack

| Area | Technology |
|---|---|
| Language | Go 1.25 |
| API | **ConnectRPC + Protobuf** (`buf` lint/breaking/generate), plus grpcurl/buf curl examples |
| Orchestration | **Temporal** Go SDK: saga with compensation, signals, queries, child workflows, cron reconciliation workflow; **AWS Step Functions** (Standard) implementation of the same saga with Go Lambdas (`provided.al2023`) |
| Database | **PostgreSQL 17**: schema design for the ledger with holds, idempotency keys, outbox; `pgx` + **sqlc** + `goose` migrations |
| Mock dependencies | Go mock services: FX quotes, compliance screening, bank transfer rail. Each has a failure/latency injection admin API |
| Ops UI | Minimal Go `html/template` + htmx dashboard: payouts, step timeline, inject failures, reconciliation report |
| Observability | `slog` JSON logs, OpenTelemetry traces (Temporal interceptor), Prometheus metrics |
| AWS | ECS Fargate (api, worker, temporal dev server, mocks), ALB, RDS PostgreSQL, Step Functions + Lambda, EventBridge Scheduler (reconciliation trigger for the SFN variant), CloudWatch, ECR |
| IaC / CI | **Terraform** · GitHub Actions |
| Docs | Design proposal `docs/design/0001-payout-saga.md`, failure-mode table, ADR comparing Temporal vs Step Functions |

Don't swap an item for an alternative. If a library is unavailable or broken, use the closest equivalent and log the swap in `docs/DECISIONS.md`.

## 3. Environment

- The host is a **Windows 11 PC**. Claude Code's shell is Git Bash, and the container runtime is Docker Desktop (WSL2 backend). Write automation as POSIX `bash` scripts under `scripts/` and use forward-slash paths. Add a `.gitattributes` with `* text=auto eol=lf` so scripts keep LF line endings.
- Prerequisites: Go 1.25, buf, Docker Desktop, Temporal CLI, Terraform, AWS CLI v2. If one is missing, install it with `winget` or the official installer and record the version in `docs/DECISIONS.md`.
- The `gh` CLI is logged in to the owner's GitHub account, and AWS CLI v2 is installed.
- Don't use Bitnami charts or images (their free catalogue has been retired). Use official upstream images and charts.

## 4. AWS account rules

- Profile: **`payout-saga-orchestrator-deployer`**. Always pass it explicitly, either as `--profile payout-saga-orchestrator-deployer` or by exporting `AWS_PROFILE=payout-saga-orchestrator-deployer` inside the scripts. Never use the default profile or any other profile.
- Region: **`ap-southeast-2`** for everything except global services such as CloudFront and IAM.
- The profile has `PowerUserAccess` + `IAMFullAccess`. Start Phase 2 with `aws sts get-caller-identity --profile payout-saga-orchestrator-deployer`. If that fails, stop and write the reason at the top of `PROGRESS.md`.
- Use AWS only. Where a requirement names Azure, GCP or another hyperscaler, use the AWS equivalent stated in `SPECS.md`.
- IaC: Terraform (`infra/`). Don't create resources by hand in the console: every cloud resource comes from code in `infra/`. Keep IaC state local and gitignored.
- Tag every resource with `Project=payout-saga-orchestrator`, `Ephemeral=true`, `ManagedBy=<tool>` and `Owner=alvieirvansyah`.
- Deployment runs **from this PC via scripts** (`scripts/deploy.sh`, `scripts/destroy.sh`). GitHub Actions only builds and tests, and never holds AWS credentials.

## 5. Phases and gates

Work strictly in order. A phase is done only when its proof exists (§8) and its items are ticked in `PROGRESS.md`. Commit and push at the end of every phase.

1. **Phase 0, scaffold.** Set up the repo layout from `SPECS.md` §4. Create `README.md`, `PROGRESS.md` (copy every checklist item from `SPECS.md` §2 and §9–§11 into it), `docs/DECISIONS.md`, `.gitignore`, `.gitattributes`, `.env.example` and the CI workflow.
2. **Phase 1, local.** Build every feature in `SPECS.md` §5. Everything runs on this PC with Docker and local tooling, and makes **no AWS calls**: use the local stand-ins listed in `SPECS.md`. Run the full test suite, then capture proof into `proof/phase1-local/`. Phase 1 counts as "ready for next" only when `scripts/test-all.sh` is green, the CI run on `main` is green, and the proof index is complete.
3. **Phase 2, AWS showcase.** Deploy with `scripts/deploy.sh`, run the smoke/e2e tests against the cloud endpoints and capture proof into `proof/phase2-cloud/`. Keep the stack alive only as long as capturing takes, with a target of **under 2 hours**.
4. **Phase 3, destroy.** Right after capturing, run `scripts/destroy.sh` and then `scripts/verify-destroyed.sh`, and save the output to `proof/phase3-destroy/`. This phase is **mandatory even if Phase 2 partly failed**. Never end a session with cloud resources running.
5. **Phase 4, write-up.** Write `docs/PORTFOLIO.md`. It needs the problem, an architecture diagram (Mermaid), a stack → requirement table, the key screenshots (relative links into `proof/`), what was hard, the approximate AWS cost and how to reproduce. This file becomes a technical page on alvieirvansyah.com.

## 6. Engineering rules

- Keep it simple and conventional: idiomatic layout for each language, small modules, no premature abstraction, no dead code.
- Configure through environment variables, with a committed `.env.example`. Real `.env` files are gitignored.
- **Never commit secrets.** That includes credentials, IaC state, `.terraform/`, `cdk.out/`, kubeconfigs, private keys and certificates. Generate local dev keys and certs into a gitignored `.local/` folder.
- Every HTTP service exposes a health endpoint (`/healthz` or the framework idiom) and logs structured JSON to stdout.
- Pin versions: commit lockfiles and pin container images to at least a minor tag.
- Use one command for each job:
  - `scripts/dev-up.sh` starts everything locally and `scripts/dev-down.sh` stops it.
  - `scripts/seed.sh` loads demo data. Start-up may run it automatically so that demos never start empty.
  - `scripts/test-all.sh` runs every test layer.
  - `scripts/capture-proof.sh <phase>` captures the proof for a phase.
- Keep `README.md` current. It covers what the project is, the architecture diagram, a quickstart, deploy/destroy, a link to `proof/README.md` and `docs/PORTFOLIO.md`, and the CI badge.
- Don't put the hiring company's name anywhere in the repo. Describe the target role generically.
- `docs/AI_USAGE.md`: briefly note how AI coding tools were used and how their output was reviewed and tested.

## 7. Testing rules

- Tests are **shallow by design**: one happy-path test per feature or flow is enough. Don't write exhaustive validation or edge-case tests.
- Required layers: Temporal testsuite (happy path; compensation path when the bank rejects), Go integration (ledger hold/capture/release + idempotent activity replay against Postgres), an SFN definition test (validate ASL with `aws stepfunctions validate-state-machine-definition` in Phase 2, plus a Go unit test for each Lambda handler), and an e2e smoke through Connect (create payout → SUCCEEDED).
- `scripts/test-all.sh` runs every local layer and exits non-zero on failure. CI runs every layer that doesn't need the cloud.
- Save test output as files (JUnit XML, an HTML report or a plain log) under the current phase's `proof/.../tests/`.

## 8. Proof rules

Proof is the deliverable. Use this folder layout:

```
proof/
  README.md                 # index: each artifact on one line, with what it proves
  phase1-local/
    screenshots/            # PNG, named NN-what-it-shows.png
    logs/                   # captured service logs and CLI output
    tests/                  # test reports
  phase2-cloud/
    screenshots/  logs/  tests/
    resources.txt           # deployed AWS resources (from IaC outputs / describe calls)
  phase3-destroy/
    verify-destroyed.txt
```

- Capture screenshots with a script (`scripts/capture-proof.sh`, driving Playwright headless), never by hand, so they can be reproduced. The screenshots listed in `SPECS.md` are the minimum.
- There is no AWS console login, so render AWS-side visuals from the CLI. For example, `aws cloudwatch get-metric-widget-image` renders a metrics graph as a PNG. Otherwise save CLI `describe`/`get` output as logs.
- Before saving logs or CLI output, redact 12-digit AWS account IDs (`sed -E 's/[0-9]{12}/<ACCOUNT_ID>/g'`), access keys, tokens, passwords and the deployer's public IP.
- Proof must show the system **working**, with real data flowing through it, and not just "deployed".

## 9. Cost and safety guardrails (AWS)

- Use the smallest viable sizes: RDS `db.t4g.micro`, Fargate 0.25–0.5 vCPU × 1 per service, one ALB, Step Functions Standard (≤ 50 executions), Lambda 128–256 MB.
- **No NAT gateways.** Also no multi-AZ databases, provisioned IOPS, reserved capacity or Savings Plans, Route 53 domains, or Marketplace subscriptions.
- Databases: `deletion_protection = false`, skip the final snapshot and keep backup retention minimal. S3 buckets are force-destroyable and ECR repos force-deletable. CloudWatch log groups get 1-day retention and are created by IaC, so they get destroyed with it.
- Public app endpoints are fine for the showcase. Keep databases private: their security groups admit only the app tier. Don't open SSH or database ports to `0.0.0.0/0` and don't use SSH at all. Use SSM if you need a shell.
- Don't send real emails or SMS, and don't call paid third-party APIs. Anything external is mocked unless `SPECS.md` says otherwise.
- `scripts/destroy.sh` must be idempotent and safe to re-run. It first deletes anything that would block the IaC destroy: stop running Step Functions executions, scale ECS services to 0, empty the ECR repos.
- `scripts/verify-destroyed.sh` lists anything still tagged `Project=payout-saga-orchestrator` (`aws resourcegroupstaggingapi get-resources`) and runs the specific checks in `SPECS.md` §11. It exits non-zero if anything billable remains. The tagging API can show resources for a few minutes after deletion, so confirm each hit with a direct describe call before failing.

## 10. Git and CI

- Use Conventional Commits (`feat:`, `fix:`, `test:`, `docs:`, `infra:`, `chore:`). Commit small and often, and push to `origin main` at least at the end of every phase.
- `.github/workflows/ci.yml` runs build, lint and the cloud-free tests on every push and pull request. Keep it green.
- Never force-push `main`. Never commit build output, dependency folders (`node_modules`, `target`, `bin`, `obj`, `.venv`) or state files.

## 11. Project-specific rules

- **Correctness is the product.** Every activity is idempotent, keyed by `(payout_id, step)` in `step_results`: a replayed activity returns the stored result. External calls carry an idempotency key, and the mocks honour it.
- The ledger uses holds: `Reserve` creates a hold (available = balance − holds), `Capture` converts the hold into a debit, `Release` deletes it. The invariant `balance = sum(entries)` is asserted after every drill by `scripts/check-invariants.sh`.
- Events (`payout.succeeded`, `payout.failed`) are written to `outbox` in the same transaction as the state change. A relay publishes them (stdout plus a webhook to a receiver) and marks them sent.
- Failure drills are scripted and their outputs are proof: (a) kill the worker during `SendBankTransfer`, so the payout resumes and completes exactly once; (b) bank returns `REJECTED`, so the compensation releases the hold and cancels the FX lock; (c) bank timeout, so the activity retries with backoff and then succeeds; (d) reconciliation detects a seeded mismatch.
- Step Functions variant: same steps as Lambda tasks with `Retry`/`Catch` blocks implementing the compensation. The Lambdas reuse the Go activity package, so the business code is shared. Compare both in `docs/adr/0002-temporal-vs-step-functions.md` (visibility, testing, cost, local dev, versioning).
- Temporal runs as the dev server (`temporal server start-dev`) in both phases. Document that production would use a persistent cluster or Temporal Cloud.

## 12. Definition of done

- [ ] Every stack item in §2 is used in running code. Cross-check with the coverage matrix in `SPECS.md` §2 and tick each row in `PROGRESS.md`.
- [ ] Phase 1 proof is complete, and all tests are green locally and in CI.
- [ ] Phase 2 proof is complete: cloud screenshots, a cloud test run and the resource list.
- [ ] Phase 3: everything is destroyed and `verify-destroyed.txt` shows nothing billable left.
- [ ] `docs/PORTFOLIO.md` and `README.md` are written, and everything is committed and pushed.
