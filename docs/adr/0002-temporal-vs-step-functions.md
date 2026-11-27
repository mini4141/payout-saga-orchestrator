# ADR 0002: Temporal vs AWS Step Functions for the payout saga

Status: accepted (Temporal is the primary orchestrator; Step Functions is the comparison implementation)

## Context
The payout saga is implemented twice on top of the same Go activity package: `workflows/payout.go` (Temporal) and
`sfn/payout.asl.json` (Step Functions Standard, with nine Go Lambdas on `provided.al2023`). Both were exercised by
the same failure drills, so this comparison comes from running both.

## Comparison

| Dimension | Temporal | Step Functions (Standard) |
|---|---|---|
| **Authoring** | Workflow is ordinary Go: loops, `if`, a compensation stack, `defer` | Declarative ASL JSON: every transition and `Catch` target is spelled out; compensation is an explicit branch |
| **Retries** | `RetryPolicy` per activity; non-retryable error types by name (`BankRejected`) | `Retry` / `Catch` blocks per state; error names are matched against the Lambda `errorType`; we added a `Transient` type to tell retryable from definite errors |
| **Compensation** | Code: push after step N, run in reverse on failure | Hand-wired states (`CompensateCancelFX → CompensateRelease`); two entry points needed for different failure depths |
| **Signals / queries** | First-class: `cancel` signal, `status` query | None. Cancel would need `StopExecution` plus compensation logic outside the machine; state is read via `DescribeExecution` |
| **Child workflows / cron** | Child workflow for step 6, cron workflow for reconciliation | Nested state machines possible; schedules come from EventBridge Scheduler (separate resource) |
| **Visibility** | Temporal UI: history, stack traces, search attributes; self-hosted | Console/API execution history with per-state input/output; built in, no operations burden |
| **Testing** | `testsuite` runs workflows in-process with time skipping and mocked activities (milliseconds) | ASL is data: we test structure (reachability, Catch targets) in Go and validate with `validate-state-machine-definition`; behaviour needs Step Functions Local or a deployed run |
| **Local dev** | `temporal server start-dev` in one container | No official faithful emulator for all features; most iteration happens in AWS |
| **Versioning** | Deterministic replay: changing workflow code requires `workflow.GetVersion` / patching for running executions | Immutable definitions: new executions use the new version; in-flight executions finish on the old one. Simpler |
| **Operations** | You run Temporal (or pay Temporal Cloud): persistence, upgrades, workers | Fully managed; workers are Lambdas |
| **Limits** | History size limits per workflow (continue-as-new); activity can run for days | 25,000 events per execution, 1-year max; payload 256 KB; Lambda 15 min per task |
| **Cost** | Server infra + workers (here: a 0.5 vCPU Fargate dev server) | ~$0.025 per 1,000 state transitions (Standard) + Lambda; a 10-payout drill costs cents |
| **Idempotency** | Required in activities (at-least-once) | Required in Lambdas (at-least-once on retry): identical work in `internal/activities` |

## Decision
Use Temporal for the product path because control flow, signals, queries and unit-testability matter most for a
money-moving saga that will keep changing. Keep the Step Functions implementation as a managed-service alternative:
it needs no servers to operate and its immutable definitions make rollouts simple, which would win for a small team
that does not want to run a workflow engine.

## Consequences
- The correctness guarantees live in the shared activity package and the database, not in either orchestrator.
  Swapping orchestrators does not change the money invariants.
- Temporal in this repo is the *dev server* (single node, in-memory): fine for the showcase, not for production.
- Two implementations double the surface to keep in sync; the drills (`scripts/`) are the safety net.
