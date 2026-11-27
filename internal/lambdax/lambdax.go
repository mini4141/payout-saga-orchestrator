// Package lambdax adapts the shared activities to AWS Lambda handlers for the Step Functions saga.
// Each Lambda binary under cmd/lambdas calls Start with its step name.
package lambdax

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
)

// Input is the Step Functions task payload.
type Input struct {
	PayoutID string `json:"payout_id"`
	Outcome  string `json:"outcome,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Handler is a Lambda handler for one saga step.
type Handler func(ctx context.Context, in Input) (any, error)

// Handlers maps a step name to its implementation on top of the shared activities.
func Handlers(a *activities.Activities) map[string]Handler {
	byID := func(fn func(context.Context, string) (activities.Result, error)) Handler {
		return func(ctx context.Context, in Input) (any, error) { return fn(ctx, in.PayoutID) }
	}
	return map[string]Handler{
		"reserve":    byID(a.Reserve),
		"fx":         byID(a.LockFX),
		"compliance": byID(a.Screen),
		"bank":       byID(a.SendBankTransfer),
		"capture":    byID(a.Capture),
		"release":    byID(a.Release),
		"cancelfx":   byID(a.CancelFX),
		"notify": func(ctx context.Context, in Input) (any, error) {
			return a.Finalize(ctx, activities.FinalizeInput{PayoutID: in.PayoutID, Outcome: in.Outcome, Reason: in.Reason})
		},
		"recon": func(ctx context.Context, _ Input) (any, error) { return a.Reconcile(ctx) },
	}
}

var (
	once sync.Once
	acts *activities.Activities
	err  error
)

// Start runs the Lambda for step. Dependencies come from DATABASE_URL and MOCKS_URL and are
// created once per execution environment.
func Start(step string) {
	lambda.Start(func(ctx context.Context, in Input) (any, error) {
		once.Do(func() {
			var pool *pgxpool.Pool
			obs.SetupLogger("lambda-" + step)
			if pool, err = pgxpool.New(context.Background(), obs.Env("DATABASE_URL", "")); err == nil {
				acts = activities.New(pool, obs.Env("MOCKS_URL", ""))
			}
		})
		if err != nil {
			return nil, activities.Transient{Msg: fmt.Sprintf("init: %v", err)}
		}
		h, ok := Handlers(acts)[step]
		if !ok {
			return nil, fmt.Errorf("unknown step %q", step)
		}
		return h(ctx, in)
	})
}
