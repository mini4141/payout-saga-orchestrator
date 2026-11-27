-- name: GetMerchantForUpdate :one
SELECT * FROM merchants WHERE id = $1 FOR UPDATE;

-- name: ListMerchants :many
SELECT * FROM merchants ORDER BY id;

-- name: UpsertMerchant :exec
INSERT INTO merchants (id, name, currency, balance_minor) VALUES ($1, $2, $3, 0)
ON CONFLICT (id) DO NOTHING;

-- name: AddBalance :exec
UPDATE merchants SET balance_minor = balance_minor + $2 WHERE id = $1;

-- name: SumHolds :one
SELECT COALESCE(SUM(amount_minor), 0)::bigint AS held FROM holds WHERE merchant_id = $1;

-- name: InsertHold :execrows
INSERT INTO holds (merchant_id, payout_id, amount_minor) VALUES ($1, $2, $3)
ON CONFLICT (payout_id) DO NOTHING;

-- name: DeleteHold :one
DELETE FROM holds WHERE payout_id = $1 RETURNING *;

-- name: InsertEntry :execrows
INSERT INTO ledger_entries (merchant_id, payout_id, kind, amount_minor) VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: CountEntries :one
SELECT count(*)::bigint FROM ledger_entries;

-- name: InvariantRows :many
SELECT m.id AS merchant_id,
       m.balance_minor,
       COALESCE((SELECT SUM(CASE WHEN e.kind = 'credit' THEN e.amount_minor ELSE -e.amount_minor END)
                 FROM ledger_entries e WHERE e.merchant_id = m.id), 0)::bigint AS entries_sum,
       COALESCE((SELECT SUM(h.amount_minor) FROM holds h WHERE h.merchant_id = m.id), 0)::bigint AS held
FROM merchants m ORDER BY m.id;

-- name: OrphanHolds :many
SELECT h.payout_id FROM holds h JOIN payouts p ON p.id = h.payout_id
WHERE p.status IN ('SUCCEEDED', 'COMPENSATED');

-- name: HasDebit :one
SELECT EXISTS (SELECT 1 FROM ledger_entries WHERE payout_id = $1 AND kind = 'debit') AS found;

-- name: GetHold :one
SELECT * FROM holds WHERE payout_id = $1;
