// Package ledger implements the hold / capture / release money movements.
//
// available = balance - holds. Reserve creates a hold, Capture converts it into a
// debit entry and lowers the balance, Release deletes it. Every function is
// idempotent per payout, and must run inside a transaction (q bound to a pgx.Tx).
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

// InsufficientFunds is returned when available balance cannot cover the hold.
// The type name is used as the non-retryable error type by both orchestrators.
type InsufficientFunds struct{ Msg string }

func (e InsufficientFunds) Error() string { return e.Msg }

// Reserve places a hold for the payout. A replay for the same payout is a no-op.
func Reserve(ctx context.Context, q *store.Queries, merchantID, payoutID string, amount int64) error {
	m, err := q.GetMerchantForUpdate(ctx, merchantID) // serialises concurrent reserves per merchant
	if err != nil {
		return fmt.Errorf("lock merchant: %w", err)
	}
	rows, err := q.InsertHold(ctx, store.InsertHoldParams{MerchantID: merchantID, PayoutID: payoutID, AmountMinor: amount})
	if err != nil {
		return fmt.Errorf("insert hold: %w", err)
	}
	if rows == 0 {
		return nil // already held by an earlier attempt
	}
	held, err := q.SumHolds(ctx, merchantID)
	if err != nil {
		return fmt.Errorf("sum holds: %w", err)
	}
	if m.BalanceMinor-held < 0 {
		return InsufficientFunds{Msg: fmt.Sprintf("merchant %s: available %d < requested %d", merchantID, m.BalanceMinor-(held-amount), amount)}
	}
	return nil
}

// Capture converts the payout's hold into a debit entry and lowers the balance.
func Capture(ctx context.Context, q *store.Queries, merchantID, payoutID string) error {
	if _, err := q.GetMerchantForUpdate(ctx, merchantID); err != nil {
		return fmt.Errorf("lock merchant: %w", err)
	}
	h, err := q.DeleteHold(ctx, payoutID)
	if errors.Is(err, pgx.ErrNoRows) {
		done, derr := q.HasDebit(ctx, &payoutID)
		if derr != nil {
			return derr
		}
		if done {
			return nil // already captured
		}
		return fmt.Errorf("capture %s: no hold", payoutID)
	}
	if err != nil {
		return fmt.Errorf("delete hold: %w", err)
	}
	rows, err := q.InsertEntry(ctx, store.InsertEntryParams{
		MerchantID: merchantID, PayoutID: &payoutID, Kind: "debit", AmountMinor: h.AmountMinor,
	})
	if err != nil {
		return fmt.Errorf("insert debit: %w", err)
	}
	if rows == 1 {
		return q.AddBalance(ctx, store.AddBalanceParams{ID: merchantID, BalanceMinor: -h.AmountMinor})
	}
	return nil
}

// Release deletes the payout's hold, restoring available funds.
func Release(ctx context.Context, q *store.Queries, merchantID, payoutID string) error {
	if _, err := q.GetMerchantForUpdate(ctx, merchantID); err != nil {
		return fmt.Errorf("lock merchant: %w", err)
	}
	if _, err := q.DeleteHold(ctx, payoutID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("delete hold: %w", err)
	}
	return nil
}

// Invariant is one merchant's consistency check result.
type Invariant struct {
	MerchantID string
	Balance    int64
	EntriesSum int64
	Held       int64
	OK         bool
}

// CheckInvariants verifies balance = sum(entries) and balance >= holds for every merchant.
func CheckInvariants(ctx context.Context, q *store.Queries) ([]Invariant, error) {
	rows, err := q.InvariantRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Invariant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Invariant{
			MerchantID: r.MerchantID, Balance: r.BalanceMinor, EntriesSum: r.EntriesSum, Held: r.Held,
			OK: r.BalanceMinor == r.EntriesSum && r.BalanceMinor >= r.Held,
		})
	}
	return out, nil
}
