package main

import (
	"context"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
)

var (
	stepAttempts = obs.Counter("payout_step_attempts_total", "Activity attempts by step and outcome.", "step", "outcome")
	finalized    = obs.Counter("payout_finalized_total", "Payouts finalized by status.", "status")
)

// instrument counts attempts of a payout-id activity.
func instrument(step string, fn func(context.Context, string) (activities.Result, error)) func(context.Context, string) (activities.Result, error) {
	return func(ctx context.Context, payoutID string) (activities.Result, error) {
		res, err := fn(ctx, payoutID)
		if err != nil {
			stepAttempts.WithLabelValues(step, "error").Inc()
		} else {
			stepAttempts.WithLabelValues(step, "ok").Inc()
		}
		return res, err
	}
}

func instrumentFinalize(fn func(context.Context, activities.FinalizeInput) (activities.Result, error)) func(context.Context, activities.FinalizeInput) (activities.Result, error) {
	return func(ctx context.Context, in activities.FinalizeInput) (activities.Result, error) {
		res, err := fn(ctx, in)
		if err == nil && !res.Replayed {
			finalized.WithLabelValues(in.Outcome).Inc()
		}
		return res, err
	}
}
