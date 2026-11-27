# Tech debt and reliability backlog

| Item | Why it matters | Next step |
|---|---|---|
| Temporal runs as the dev server (SQLite, no HA) | History is lost if the container is replaced | Temporal Cloud or a persistent cluster on RDS |
| Single mocks process for all three rails | Failure injection is shared across services | Split per rail if the demo needs independent scaling |
| API auth is one static bearer token | No per-merchant authorization | OIDC + per-merchant scopes |
| Reconciliation compares whole tables | Fine for the demo, O(n) per run | Window by `created_at` and index the bank feed |
| Outbox relay is a single poller | One instance only to keep ordering | `FOR UPDATE SKIP LOCKED` with per-aggregate ordering |
| Ops UI reads the database directly | Couples UI to the schema | Read model behind the Connect API |
| Lambdas open a new Postgres connection per invocation | Cold-start latency, connection churn | RDS Proxy |
