package workflows_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/workflows"
)

type calls struct {
	mu    sync.Mutex
	names []string
	final activities.FinalizeInput
}

func (c *calls) record(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.names = append(c.names, name)
}

func setup(t *testing.T, bankErr error) (*testsuite.TestWorkflowEnvironment, *calls) {
	t.Helper()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(workflows.NotifyWorkflow)
	c := &calls{}
	for _, name := range []string{
		activities.StepReserve, activities.StepFX, activities.StepCompliance, activities.StepBank,
		activities.StepCapture, activities.StepRelease, activities.StepCancelFX,
	} {
		name := name
		env.RegisterActivityWithOptions(func(context.Context, string) (activities.Result, error) {
			c.record(name)
			if name == activities.StepBank && bankErr != nil {
				return activities.Result{}, bankErr
			}
			return activities.Result{Step: name, Attempt: 1}, nil
		}, activity.RegisterOptions{Name: name})
	}
	env.RegisterActivityWithOptions(func(_ context.Context, in activities.FinalizeInput) (activities.Result, error) {
		c.record(activities.StepFinalize)
		c.final = in
		return activities.Result{Step: activities.StepFinalize, Attempt: 1}, nil
	}, activity.RegisterOptions{Name: activities.StepFinalize})
	return env, c
}

func TestPayoutWorkflowHappyPath(t *testing.T) {
	env, c := setup(t, nil)
	env.ExecuteWorkflow(workflows.PayoutWorkflow, "po_1")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out string
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, "SUCCEEDED", out)
	require.Equal(t, []string{"Reserve", "LockFX", "Screen", "SendBankTransfer", "Capture", "Finalize"}, c.names)
	require.Equal(t, "SUCCEEDED", c.final.Outcome)

	v, err := env.QueryWorkflow(workflows.StatusQuery)
	require.NoError(t, err)
	var st workflows.Status
	require.NoError(t, v.Get(&st))
	require.Equal(t, "done", st.Step)
}

// A BankRejected failure is not retried; the compensations run in reverse order of the steps.
func TestPayoutWorkflowCompensatesWhenBankRejects(t *testing.T) {
	rejected := temporal.NewNonRetryableApplicationError("bank rejected", "BankRejected", nil)
	env, c := setup(t, rejected)
	env.ExecuteWorkflow(workflows.PayoutWorkflow, "po_1")
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var out string
	require.NoError(t, env.GetWorkflowResult(&out))
	require.Equal(t, "COMPENSATED", out)
	require.Equal(t, []string{"Reserve", "LockFX", "Screen", "SendBankTransfer", "CancelFX", "Release", "Finalize"}, c.names)
	require.Equal(t, "COMPENSATED", c.final.Outcome)
	require.Contains(t, c.final.Reason, "bank rejected")
}
