-- name: InsertPayout :one
INSERT INTO payouts (id, merchant_id, amount_minor, currency, target_currency, bank_account, engine, idempotency_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING *;

-- name: GetPayout :one
SELECT * FROM payouts WHERE id = $1;

-- name: GetPayoutByKey :one
SELECT * FROM payouts WHERE idempotency_key = $1;

-- name: ListPayouts :many
SELECT * FROM payouts ORDER BY created_at DESC, id DESC LIMIT $1;

-- name: ListPayoutsByStatus :many
SELECT * FROM payouts WHERE status = $1 ORDER BY created_at;

-- name: UpdatePayoutStatus :exec
UPDATE payouts SET status = $2 WHERE id = $1;

-- name: GetStepResult :one
SELECT * FROM step_results WHERE payout_id = $1 AND step = $2;

-- name: InsertStepResult :exec
INSERT INTO step_results (payout_id, step, result) VALUES ($1, $2, $3)
ON CONFLICT (payout_id, step) DO NOTHING;

-- name: ListStepResults :many
SELECT * FROM step_results WHERE payout_id = $1 ORDER BY at;

-- name: StartAttempt :one
INSERT INTO step_attempts (payout_id, step, attempt, outcome, started_at, duration_ms)
VALUES ($1, $2, $3, 'started', now(), 0)
RETURNING id;

-- name: FinishAttempt :exec
UPDATE step_attempts SET outcome = $2, error = $3, duration_ms = $4 WHERE id = $1;

-- name: ListAttempts :many
SELECT * FROM step_attempts WHERE payout_id = $1 ORDER BY started_at, id;

-- name: InsertOutbox :exec
INSERT INTO outbox (aggregate_id, type, payload) VALUES ($1, $2, $3);

-- name: ListUnsentOutbox :many
SELECT * FROM outbox WHERE sent_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxSent :exec
UPDATE outbox SET sent_at = now() WHERE id = $1;

-- name: CountOutbox :many
SELECT type, count(*)::bigint AS total, count(sent_at)::bigint AS sent FROM outbox GROUP BY type ORDER BY type;

-- name: InsertReconReport :one
INSERT INTO recon_reports (matched, missing, mismatched, details) VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListReconReports :many
SELECT * FROM recon_reports ORDER BY id DESC LIMIT $1;

-- name: CountAttempts :one
SELECT count(*)::bigint FROM step_attempts WHERE payout_id = $1 AND step = $2;
