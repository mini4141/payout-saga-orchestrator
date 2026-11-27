# AI usage

This repository was built with Claude Code working autonomously from `SPECS.md` and `CLAUDE.md`.

- **What the tool did**: wrote the Go services, SQL, Terraform, scripts and docs, and ran the drills.
- **How output was reviewed**: every claim in the proof comes from a script run (drills, `check-invariants.sh`, test reports), not from the model's description. Generated code (`buf`) is regenerated in CI and diffed.
- **How it was tested**: Temporal test-suite workflow tests, Postgres integration tests for the ledger and idempotent replay, a Lambda handler unit test, an e2e smoke through Connect, and scripted failure drills whose logs are committed under `proof/`.
- **Human oversight**: decisions that deviate from the spec are recorded in `docs/DECISIONS.md` for review.
