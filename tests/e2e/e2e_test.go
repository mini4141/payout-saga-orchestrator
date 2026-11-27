// Package e2e is the smoke test: create a payout through Connect and poll it to SUCCEEDED.
// It needs the running stack (scripts/dev-up.sh) and E2E_API_URL, e.g. http://localhost:18080.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"

	payoutv1 "github.com/mini4141/payout-saga-orchestrator/gen/payout/v1"
	"github.com/mini4141/payout-saga-orchestrator/gen/payout/v1/payoutv1connect"
)

func TestCreatePayoutReachesSucceeded(t *testing.T) {
	base := os.Getenv("E2E_API_URL")
	if base == "" {
		t.Skip("E2E_API_URL not set")
	}
	token := os.Getenv("API_TOKEN")
	if token == "" {
		token = "dev-token"
	}
	c := payoutv1connect.NewPayoutServiceClient(http.DefaultClient, base)
	ctx := context.Background()
	withAuth := func(r *connect.Request[payoutv1.CreatePayoutRequest]) *connect.Request[payoutv1.CreatePayoutRequest] {
		r.Header().Set("Authorization", "Bearer "+token)
		return r
	}
	key := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	req := &payoutv1.CreatePayoutRequest{MerchantId: "m_delta", AmountMinor: 4200, Currency: "AUD", TargetCurrency: "USD", BankAccount: "062-000-9999", IdempotencyKey: key}

	created, err := c.CreatePayout(ctx, withAuth(connect.NewRequest(req)))
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.CreatePayout(ctx, withAuth(connect.NewRequest(req)))
	if err != nil {
		t.Fatal(err)
	}
	if again.Msg.PayoutId != created.Msg.PayoutId || again.Msg.Created {
		t.Fatalf("repeating the idempotency key must return the same payout: %v vs %v", created.Msg, again.Msg)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		g := connect.NewRequest(&payoutv1.GetPayoutRequest{PayoutId: created.Msg.PayoutId})
		g.Header().Set("Authorization", "Bearer "+token)
		got, err := c.GetPayout(ctx, g)
		if err != nil {
			t.Fatal(err)
		}
		if got.Msg.Payout.Status == "SUCCEEDED" {
			if len(got.Msg.Attempts) < 6 {
				t.Fatalf("want 6 step attempts, got %d", len(got.Msg.Attempts))
			}
			return
		}
		if got.Msg.Payout.Status == "COMPENSATED" {
			t.Fatalf("payout compensated unexpectedly: %v", got.Msg.Attempts)
		}
		time.Sleep(time.Second)
	}
	t.Fatal("payout did not reach SUCCEEDED in time")
}
