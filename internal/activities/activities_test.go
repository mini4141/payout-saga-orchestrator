package activities_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/internal/mock"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
	"github.com/mini4141/payout-saga-orchestrator/internal/testdb"
)

// A replayed activity returns the stored result instead of doing the work twice.
func TestActivityReplayReturnsStoredResult(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 10_000)
	testdb.Payout(t, pool, "p1", "m1", 2_500)
	srv := httptest.NewServer(mock.New(time.Second).Handler())
	defer srv.Close()
	a := activities.New(pool, srv.URL)
	ctx := context.Background()

	if _, err := a.Reserve(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	first, err := a.Capture(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed {
		t.Fatal("first run must not be a replay")
	}
	second, err := a.Capture(ctx, "p1") // e.g. the worker died after commit and Temporal re-ran the activity
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed {
		t.Fatal("second run must return the stored result")
	}

	q := store.New(pool)
	m, _ := q.GetMerchantForUpdate(ctx, "m1")
	if m.BalanceMinor != 7_500 {
		t.Fatalf("balance=%d, want 7500 (debited exactly once)", m.BalanceMinor)
	}
	if n, _ := q.CountEntries(ctx); n != 2 { // opening credit + one debit
		t.Fatalf("ledger entries=%d, want 2", n)
	}
}
