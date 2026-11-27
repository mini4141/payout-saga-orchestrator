# Decisions

One line per decision: date, decision, reason.

- 2026-11-27 Go 1.26 instead of 1.25 (`go.mod` says `go 1.26.0`, Docker builds use `golang:1.26`, CI uses 1.26). Reason: current releases of the Temporal SDK, OpenTelemetry, grpc, goose, connect and prometheus all require Go 1.26; pinning old versions of all of them would be worse. Host toolchain is 1.27.4.
- 2026-11-27 Temporal CLI is not installed; the Temporal dev server runs as the official `temporalio/temporal:1.10.1` image (`server start-dev`) in compose and on Fargate. Reason: same `start-dev` server, no host install needed.
- 2026-11-27 One multi-binary Docker image (`payout-saga`) holds every service; compose/ECS pick the binary with `command`. Reason: one build, one ECR repo.
- 2026-11-27 One `cmd/mocks` process serves FX, compliance and bank mocks on one port with path prefixes from SPECS §7. Reason: simplest option that keeps three mocks with separate admin APIs.
- 2026-11-27 Added `cmd/receiver` (webhook receiver) and `cmd/migrate` (goose up + seed). Reason: SPECS §9 lists a receiver; ECS needs a one-off migration task because RDS is private.
- 2026-11-27 Added `step_attempts` table (per-attempt outcome and timing) beyond SPECS §6. Reason: the step timeline must show retries and attempts, which `step_results` (one row per step) cannot.
- 2026-11-27 `CreatePayout` takes `engine` (`temporal` default, `sfn`). With `sfn` the API only creates the payout row; `scripts/sfn-drill.sh` starts the Step Functions execution. Reason: keeps the AWS SDK out of the API and Phase 1 free of AWS calls.
- 2026-11-27 Lambdas run in the VPC and reach the mocks and RDS over Cloud Map private DNS instead of the ALB. Reason: no NAT gateway, so VPC Lambdas cannot reach the public ALB.
- 2026-11-27 Added `notify` and `recon` Lambdas to the seven in SPECS §10. Reason: the SFN saga needs step 6 and an EventBridge-triggered reconciliation.
- 2026-11-27 Ledger opening balances are written as credit entries so that `balance = sum(entries)` holds literally. Reason: invariant in CLAUDE.md §11.
- 2026-11-27 Real DB access in the ops UI is read-only SQL plus proxying to the mocks admin API and starting a one-off reconciliation workflow. Reason: simplest dashboard that covers SPECS §5 F5.
