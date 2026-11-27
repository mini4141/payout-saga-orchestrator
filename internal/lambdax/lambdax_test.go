package lambdax_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mini4141/payout-saga-orchestrator/internal/activities"
	"github.com/mini4141/payout-saga-orchestrator/internal/lambdax"
	"github.com/mini4141/payout-saga-orchestrator/internal/mock"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
	"github.com/mini4141/payout-saga-orchestrator/internal/testdb"
)

// Runs every saga Lambda handler in the order Step Functions would, on the shared activities.
func TestHandlersHappyPath(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 10_000)
	testdb.Payout(t, pool, "p1", "m1", 2_500)
	srv := httptest.NewServer(mock.New(time.Second).Handler())
	defer srv.Close()
	h := lambdax.Handlers(activities.New(pool, srv.URL))
	ctx := context.Background()

	for _, step := range []string{"reserve", "fx", "compliance", "bank", "capture"} {
		if _, err := h[step](ctx, lambdax.Input{PayoutID: "p1"}); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
	if _, err := h["notify"](ctx, lambdax.Input{PayoutID: "p1", Outcome: "SUCCEEDED"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h["recon"](ctx, lambdax.Input{}); err != nil {
		t.Fatal(err)
	}
	p, _ := store.New(pool).GetPayout(ctx, "p1")
	if p.Status != "SUCCEEDED" {
		t.Fatalf("status=%s, want SUCCEEDED", p.Status)
	}
}

// The compensation handlers and the bank rejection error type that Step Functions' Catch matches on.
func TestBankRejectedThenCompensate(t *testing.T) {
	pool := testdb.New(t)
	testdb.Merchant(t, pool, "m1", 10_000)
	testdb.Payout(t, pool, "p1", "m1", 2_500)
	m := mock.New(time.Second)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	post := func(path, body string) {
		resp, err := srv.Client().Post(srv.URL+path, "application/json", stringsReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	post("/admin/bank/config", `{"reject_next":1}`)
	h := lambdax.Handlers(activities.New(pool, srv.URL))
	ctx := context.Background()

	for _, step := range []string{"reserve", "fx", "compliance"} {
		if _, err := h[step](ctx, lambdax.Input{PayoutID: "p1"}); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
	_, err := h["bank"](ctx, lambdax.Input{PayoutID: "p1"})
	var rej activities.BankRejected
	if !errors.As(err, &rej) {
		t.Fatalf("want BankRejected, got %v", err)
	}
	if name := reflect.TypeOf(err).Name(); name != "BankRejected" { // Lambda reports this as errorType
		t.Fatalf("error type name %q, want BankRejected", name)
	}
	for _, step := range []string{"cancelfx", "release"} {
		if _, err := h[step](ctx, lambdax.Input{PayoutID: "p1"}); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
	if n, _ := store.New(pool).GetHold(ctx, "p1"); n.ID != 0 {
		t.Fatal("hold should be released")
	}
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
