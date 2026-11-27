package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mini4141/payout-saga-orchestrator/internal/ledger"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
	"github.com/mini4141/payout-saga-orchestrator/internal/testdb"
)

// inTx runs fn in a transaction and commits when fn returns nil.
func inTx(t *testing.T, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, fn func(q *store.Queries) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(store.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func TestHoldThenCaptureChangesBalance(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 1000)
	ctx := context.Background()

	if err := inTx(t, pool, func(q *store.Queries) error { return ledger.Reserve(ctx, q, "m1", "p1", 400) }); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	m, _ := q.GetMerchantForUpdate(ctx, "m1") // outside a tx: lock is released immediately
	held, _ := q.SumHolds(ctx, "m1")
	if m.BalanceMinor != 1000 || held != 400 {
		t.Fatalf("after reserve: balance=%d held=%d, want 1000/400 (available 600)", m.BalanceMinor, held)
	}

	if err := inTx(t, pool, func(q *store.Queries) error { return ledger.Capture(ctx, q, "m1", "p1") }); err != nil {
		t.Fatal(err)
	}
	m, _ = q.GetMerchantForUpdate(ctx, "m1")
	held, _ = q.SumHolds(ctx, "m1")
	if m.BalanceMinor != 600 || held != 0 {
		t.Fatalf("after capture: balance=%d held=%d, want 600/0", m.BalanceMinor, held)
	}
	inv, err := ledger.CheckInvariants(ctx, q)
	if err != nil || len(inv) != 1 || !inv[0].OK {
		t.Fatalf("invariants: %+v err=%v", inv, err)
	}
}

func TestReleaseRestoresAvailable(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 1000)
	ctx := context.Background()
	_ = inTx(t, pool, func(q *store.Queries) error { return ledger.Reserve(ctx, q, "m1", "p1", 400) })
	if err := inTx(t, pool, func(q *store.Queries) error { return ledger.Release(ctx, q, "m1", "p1") }); err != nil {
		t.Fatal(err)
	}
	q := store.New(pool)
	m, _ := q.GetMerchantForUpdate(ctx, "m1")
	held, _ := q.SumHolds(ctx, "m1")
	if m.BalanceMinor != 1000 || held != 0 {
		t.Fatalf("balance=%d held=%d, want 1000/0", m.BalanceMinor, held)
	}
}

func TestReserveRejectsInsufficientFunds(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 500)
	ctx := context.Background()
	_ = inTx(t, pool, func(q *store.Queries) error { return ledger.Reserve(ctx, q, "m1", "p1", 400) })
	err := inTx(t, pool, func(q *store.Queries) error { return ledger.Reserve(ctx, q, "m1", "p2", 200) })
	var ins ledger.InsufficientFunds
	if !errors.As(err, &ins) {
		t.Fatalf("want InsufficientFunds, got %v", err)
	}
}
