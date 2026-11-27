// Package activities holds the payout saga steps. The same code runs under Temporal
// (worker) and AWS Step Functions (Lambdas). Each step is idempotent: it is keyed by
// (payout_id, step) in step_results, and a replay returns the stored result.
package activities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mini4141/payout-saga-orchestrator/internal/ledger"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

// Step names double as Temporal activity names and Lambda step identifiers.
const (
	StepReserve    = "Reserve"
	StepFX         = "LockFX"
	StepCompliance = "Screen"
	StepBank       = "SendBankTransfer"
	StepCapture    = "Capture"
	StepFinalize   = "Finalize"
	StepRelease    = "Release"
	StepCancelFX   = "CancelFX"
)

// Outcomes accepted by Finalize.
const (
	OutcomeSucceeded   = "SUCCEEDED"
	OutcomeCompensated = "COMPENSATED"
	OutcomeNeedsReview = "NEEDS_REVIEW"
)

// Result is what every step returns (and what step_results stores).
type Result struct {
	Step     string         `json:"step"`
	Replayed bool           `json:"replayed"`
	Attempt  int            `json:"attempt"`
	Data     map[string]any `json:"data,omitempty"`
}

// Activities bundles the dependencies shared by all steps.
type Activities struct {
	Pool   *pgxpool.Pool
	Q      *store.Queries
	Mocks  *Mocks
	Logger *slog.Logger
	// AttemptFn reports the retry attempt (1-based) for the current invocation. Temporal sets it
	// from the activity info; when nil, it is the number of recorded attempts of the step plus one
	// (which is how Step Functions retries are counted).
	AttemptFn func(ctx context.Context) int
}

// New builds Activities on a pool and a mocks base URL.
func New(pool *pgxpool.Pool, mocksURL string) *Activities {
	return &Activities{Pool: pool, Q: store.New(pool), Mocks: NewMocks(mocksURL), Logger: slog.Default()}
}

type stepFn func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error)

// run executes one idempotent step. When inTx is true the step body and the
// step_results insert commit atomically (ledger and outbox work); otherwise the body is an
// external call and the stored result is written after it returns.
func (a *Activities) run(ctx context.Context, payoutID, step string, inTx bool, fn stepFn) (Result, error) {
	attempt := 1
	if a.AttemptFn != nil {
		attempt = a.AttemptFn(ctx)
	} else if n, err := a.Q.CountAttempts(ctx, store.CountAttemptsParams{PayoutID: payoutID, Step: step}); err == nil {
		attempt = int(n) + 1
	}
	started := time.Now()
	attemptID, _ := a.Q.StartAttempt(ctx, store.StartAttemptParams{PayoutID: payoutID, Step: step, Attempt: int32(attempt)})
	res, err := a.execute(ctx, payoutID, step, attempt, inTx, fn)
	err = classify(err)

	outcome, msg := "ok", ""
	switch {
	case err != nil:
		outcome, msg = "error", err.Error()
	case res.Replayed:
		outcome = "replayed"
	}
	// Use a fresh context: the step context may already be cancelled.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = a.Q.FinishAttempt(fctx, store.FinishAttemptParams{ID: attemptID, Outcome: outcome, Error: msg, DurationMs: time.Since(started).Milliseconds()})
	a.Logger.Info("step", "payout_id", payoutID, "step", step, "attempt", attempt, "outcome", outcome, "error", msg, "ms", time.Since(started).Milliseconds())
	return res, err
}

func (a *Activities) execute(ctx context.Context, payoutID, step string, attempt int, inTx bool, fn stepFn) (Result, error) {
	var tx pgx.Tx
	q := a.Q
	if inTx {
		var err error
		if tx, err = a.Pool.Begin(ctx); err != nil {
			return Result{}, err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		q = store.New(tx)
	}
	if stored, err := q.GetStepResult(ctx, store.GetStepResultParams{PayoutID: payoutID, Step: step}); err == nil {
		var r Result
		if err := json.Unmarshal(stored.Result, &r); err != nil {
			return Result{}, err
		}
		r.Replayed, r.Attempt = true, attempt
		return r, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	p, err := q.GetPayout(ctx, payoutID)
	if err != nil {
		return Result{}, fmt.Errorf("load payout %s: %w", payoutID, err)
	}
	data, err := fn(ctx, q, p)
	if err != nil {
		return Result{}, err
	}
	r := Result{Step: step, Attempt: attempt, Data: data}
	raw, _ := json.Marshal(r)
	if err := q.InsertStepResult(ctx, store.InsertStepResultParams{PayoutID: payoutID, Step: step, Result: raw}); err != nil {
		return Result{}, err
	}
	if inTx {
		if err := tx.Commit(ctx); err != nil {
			return Result{}, err
		}
	}
	return r, nil
}

// stored returns the data map of an earlier step.
func stored(ctx context.Context, q *store.Queries, payoutID, step string) (map[string]any, error) {
	row, err := q.GetStepResult(ctx, store.GetStepResultParams{PayoutID: payoutID, Step: step})
	if err != nil {
		return nil, fmt.Errorf("step %s has no stored result: %w", step, err)
	}
	var r Result
	if err := json.Unmarshal(row.Result, &r); err != nil {
		return nil, err
	}
	return r.Data, nil
}

// Reserve (step 1) places the ledger hold and moves the payout to PROCESSING.
func (a *Activities) Reserve(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepReserve, true, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		if err := ledger.Reserve(ctx, q, p.MerchantID, p.ID, p.AmountMinor); err != nil {
			return nil, err
		}
		if err := q.UpdatePayoutStatus(ctx, store.UpdatePayoutStatusParams{ID: p.ID, Status: "PROCESSING"}); err != nil {
			return nil, err
		}
		return map[string]any{"held_minor": p.AmountMinor}, nil
	})
}

// LockFX (step 2) obtains an FX quote.
func (a *Activities) LockFX(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepFX, false, func(ctx context.Context, _ *store.Queries, p store.Payout) (map[string]any, error) {
		return a.Mocks.Quote(ctx, p)
	})
}

// Screen (step 3) runs compliance screening.
func (a *Activities) Screen(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepCompliance, false, func(ctx context.Context, _ *store.Queries, p store.Payout) (map[string]any, error) {
		return a.Mocks.Screen(ctx, p)
	})
}

// SendBankTransfer (step 4) sends the transfer; the payout id is the idempotency key.
func (a *Activities) SendBankTransfer(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepBank, false, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		fx, err := stored(ctx, q, p.ID, StepFX)
		if err != nil {
			return nil, err
		}
		quote, _ := fx["quote_id"].(string)
		return a.Mocks.Transfer(ctx, p, quote)
	})
}

// Capture (step 5) converts the hold into a debit.
func (a *Activities) Capture(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepCapture, true, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		if err := ledger.Capture(ctx, q, p.MerchantID, p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"captured_minor": p.AmountMinor}, nil
	})
}

// Release (compensation of step 1) deletes the hold.
func (a *Activities) Release(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepRelease, true, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		if err := ledger.Release(ctx, q, p.MerchantID, p.ID); err != nil {
			return nil, err
		}
		return map[string]any{"released_minor": p.AmountMinor}, nil
	})
}

// CancelFX (compensation of step 2) cancels the FX lock.
func (a *Activities) CancelFX(ctx context.Context, payoutID string) (Result, error) {
	return a.run(ctx, payoutID, StepCancelFX, false, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		fx, err := stored(ctx, q, p.ID, StepFX)
		if err != nil {
			return nil, err
		}
		quote, _ := fx["quote_id"].(string)
		return a.Mocks.CancelQuote(ctx, p, quote)
	})
}

// FinalizeInput carries the outcome for step 6.
type FinalizeInput struct {
	PayoutID string `json:"payout_id"`
	Outcome  string `json:"outcome"`
	Reason   string `json:"reason,omitempty"`
}

// Finalize (step 6) sets the final status and writes the outbox event in the same transaction.
func (a *Activities) Finalize(ctx context.Context, in FinalizeInput) (Result, error) {
	return a.run(ctx, in.PayoutID, StepFinalize, true, func(ctx context.Context, q *store.Queries, p store.Payout) (map[string]any, error) {
		if err := q.UpdatePayoutStatus(ctx, store.UpdatePayoutStatusParams{ID: p.ID, Status: in.Outcome}); err != nil {
			return nil, err
		}
		typ := "payout.succeeded"
		if in.Outcome != OutcomeSucceeded {
			typ = "payout.failed"
		}
		payload, _ := json.Marshal(map[string]any{
			"payout_id": p.ID, "merchant_id": p.MerchantID, "amount_minor": p.AmountMinor,
			"currency": p.Currency, "status": in.Outcome, "reason": in.Reason,
		})
		if err := q.InsertOutbox(ctx, store.InsertOutboxParams{AggregateID: p.ID, Type: typ, Payload: payload}); err != nil {
			return nil, err
		}
		return map[string]any{"status": in.Outcome, "event": typ}, nil
	})
}

// Transient marks an infrastructure failure that is safe to retry. Its type name is matched by
// the Step Functions Retry blocks; Temporal retries it by default.
type Transient struct{ Msg string }

func (e Transient) Error() string { return e.Msg }

// classify keeps domain errors as they are and wraps everything else as Transient.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var (
		br BankRejected
		cr ComplianceRejected
		is ledger.InsufficientFunds
	)
	if errors.As(err, &br) || errors.As(err, &cr) || errors.As(err, &is) {
		return err
	}
	return Transient{Msg: err.Error()}
}
