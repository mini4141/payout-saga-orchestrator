// Command worker runs the Temporal worker for the payout saga and starts the reconciliation cron.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/temporalx"
	"github.com/mini4141/payout-saga-orchestrator/workflows"
)

func main() {
	log := obs.SetupLogger("worker")
	shutdown := obs.SetupTracing("worker")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	defer func() { _ = shutdown(context.Background()) }()

	pool, err := pgxpool.New(ctx, obs.Env("DATABASE_URL", ""))
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	tc, err := temporalx.Dial(ctx, obs.Env("TEMPORAL_ADDRESS", "temporal:7233"))
	if err != nil {
		log.Error("temporal", "err", err)
		os.Exit(1)
	}
	defer tc.Close()
	obs.Serve(obs.Env("WORKER_ADDR", ":9090"))

	acts := activities.New(pool, obs.Env("MOCKS_URL", "http://mocks:8090"))
	acts.AttemptFn = func(ctx context.Context) int { return int(activity.GetInfo(ctx).Attempt) }

	w := worker.New(tc, workflows.TaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.PayoutWorkflow)
	w.RegisterWorkflow(workflows.NotifyWorkflow)
	w.RegisterWorkflow(workflows.ReconciliationWorkflow)
	reg := func(name string, fn any) { w.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name}) }
	reg(activities.StepReserve, instrument(activities.StepReserve, acts.Reserve))
	reg(activities.StepFX, instrument(activities.StepFX, acts.LockFX))
	reg(activities.StepCompliance, instrument(activities.StepCompliance, acts.Screen))
	reg(activities.StepBank, instrument(activities.StepBank, acts.SendBankTransfer))
	reg(activities.StepCapture, instrument(activities.StepCapture, acts.Capture))
	reg(activities.StepRelease, instrument(activities.StepRelease, acts.Release))
	reg(activities.StepCancelFX, instrument(activities.StepCancelFX, acts.CancelFX))
	reg(activities.StepFinalize, instrumentFinalize(acts.Finalize))
	reg("Reconcile", acts.Reconcile)

	// Cron reconciliation: idempotent start, ignore "already running".
	_, err = tc.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: workflows.ReconWorkflowID, TaskQueue: workflows.TaskQueue, CronSchedule: workflows.ReconCronSchedule,
	}, workflows.ReconciliationWorkflow)
	var already *serviceerror.WorkflowExecutionAlreadyStarted
	if err != nil && !errors.As(err, &already) {
		log.Warn("start reconciliation cron", "err", err)
	}

	log.Info("worker started", "task_queue", workflows.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		slog.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}
