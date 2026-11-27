// Package workflows holds the Temporal workflows: the payout saga, its notify child
// workflow and the cron reconciliation workflow.
package workflows

import (
	"errors"
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
)

// Names shared by the API, ops UI and worker.
const (
	TaskQueue          = "payout"
	CancelSignal       = "cancel"
	StatusQuery        = "status"
	ReconWorkflowID    = "reconciliation-cron"
	ReconCronSchedule  = "*/5 * * * *"
	reconcileActivity  = "Reconcile"
	cancelledReason    = "cancelled by merchant"
	reviewReasonFormat = "bank outcome unknown after retries"
)

// WorkflowID returns the workflow ID for a payout.
func WorkflowID(payoutID string) string { return "payout-" + payoutID }

// Status is the answer to the "status" query.
type Status struct {
	PayoutID      string         `json:"payout_id"`
	Step          string         `json:"step"`
	State         string         `json:"state"`
	Attempts      map[string]int `json:"attempts"`
	Compensations []string       `json:"compensations"`
}

func stepOptions(maxAttempts int32, nonRetryable ...string) workflow.ActivityOptions {
	return workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Second,
			BackoffCoefficient:     2,
			MaximumInterval:        15 * time.Second,
			MaximumAttempts:        maxAttempts, // 0 = unlimited
			NonRetryableErrorTypes: nonRetryable,
		},
	}
}

var definiteFailures = map[string]bool{
	"BankRejected": true, "ComplianceRejected": true, "InsufficientFunds": true, "Cancelled": true,
}

func isDefinite(err error) bool {
	var app *temporal.ApplicationError
	return errors.As(err, &app) && definiteFailures[app.Type()]
}

// PayoutWorkflow runs the six-step payout saga for an existing payout row.
func PayoutWorkflow(ctx workflow.Context, payoutID string) (string, error) {
	st := &Status{PayoutID: payoutID, Step: "starting", State: "RUNNING", Attempts: map[string]int{}, Compensations: []string{}}
	if err := workflow.SetQueryHandler(ctx, StatusQuery, func() (Status, error) { return *st, nil }); err != nil {
		return "", err
	}
	cancelCh := workflow.GetSignalChannel(ctx, CancelSignal)

	call := func(name string, opts workflow.ActivityOptions, arg any) error {
		st.Step = name
		var res activities.Result
		err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, opts), name, arg).Get(ctx, &res)
		if err == nil {
			st.Attempts[name] = res.Attempt
		}
		return err
	}

	var stack []string // compensations, pushed in step order and run in reverse
	bankStarted := false
	saga := func() error {
		if err := call(activities.StepReserve, stepOptions(3, "InsufficientFunds"), payoutID); err != nil {
			return err
		}
		stack = append(stack, activities.StepRelease)
		if err := call(activities.StepFX, stepOptions(5), payoutID); err != nil {
			return err
		}
		stack = append(stack, activities.StepCancelFX)
		if err := call(activities.StepCompliance, stepOptions(5, "ComplianceRejected"), payoutID); err != nil {
			return err
		}
		if cancelCh.ReceiveAsync(nil) { // CancelPayout is honoured only before the bank step
			return temporal.NewNonRetryableApplicationError(cancelledReason, "Cancelled", nil)
		}
		bankStarted = true
		if err := call(activities.StepBank, stepOptions(5, "BankRejected"), payoutID); err != nil {
			return err
		}
		return call(activities.StepCapture, stepOptions(0), payoutID)
	}

	outcome, reason := activities.OutcomeSucceeded, ""
	if err := saga(); err != nil {
		reason = err.Error()
		var app *temporal.ApplicationError
		if errors.As(err, &app) {
			reason = app.Message()
		}
		if bankStarted && !isDefinite(err) {
			// The transfer may have gone out: keep the hold and hand over to humans.
			outcome, reason = activities.OutcomeNeedsReview, reviewReasonFormat
		} else {
			outcome = activities.OutcomeCompensated
			dctx, cancel := workflow.NewDisconnectedContext(ctx)
			defer cancel()
			for i := len(stack) - 1; i >= 0; i-- {
				name := stack[i]
				st.Step = "compensate:" + name
				if cerr := workflow.ExecuteActivity(workflow.WithActivityOptions(dctx, stepOptions(0)), name, payoutID).Get(dctx, nil); cerr != nil {
					return "", cerr
				}
				st.Compensations = append(st.Compensations, name)
			}
		}
	}

	st.Step = "notify"
	child := workflow.WithChildOptions(ctx, workflow.ChildWorkflowOptions{WorkflowID: "notify-" + payoutID})
	in := activities.FinalizeInput{PayoutID: payoutID, Outcome: outcome, Reason: reason}
	if err := workflow.ExecuteChildWorkflow(child, NotifyWorkflow, in).Get(ctx, nil); err != nil {
		return "", err
	}
	st.Step, st.State = "done", outcome
	return outcome, nil
}

// NotifyWorkflow is step 6: it writes the final status and the outbox event in one transaction.
func NotifyWorkflow(ctx workflow.Context, in activities.FinalizeInput) (activities.Result, error) {
	var res activities.Result
	err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, stepOptions(0)), activities.StepFinalize, in).Get(ctx, &res)
	return res, err
}

// ReconciliationWorkflow compares SUCCEEDED payouts with the bank's settled list.
// It is started with a cron schedule, and also once on demand from the ops UI.
func ReconciliationWorkflow(ctx workflow.Context) (activities.ReconReport, error) {
	var rep activities.ReconReport
	opts := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	}
	err := workflow.ExecuteActivity(workflow.WithActivityOptions(ctx, opts), reconcileActivity).Get(ctx, &rep)
	return rep, err
}
