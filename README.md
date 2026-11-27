# Payout Saga Orchestrator

[![ci](https://github.com/mini4141/payout-saga-orchestrator/actions/workflows/ci.yml/badge.svg)](https://github.com/mini4141/payout-saga-orchestrator/actions/workflows/ci.yml)

Paying a merchant out takes six steps across four systems, and any of them can fail. This Go backend runs the payout as a **Temporal saga** with idempotent activities, compensation, a transactional outbox and reconciliation, and shows that killing the worker or breaking the bank mid-flight never loses or duplicates money. The same saga also runs on **AWS Step Functions** for comparison.

Status: work in progress, see [PROGRESS.md](PROGRESS.md).
