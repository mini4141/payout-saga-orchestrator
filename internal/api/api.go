// Package api implements the Connect PayoutService.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	payoutv1 "github.com/mini4141/payout-saga-orchestrator/gen/payout/v1"
	"github.com/mini4141/payout-saga-orchestrator/gen/payout/v1/payoutv1connect"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
	"github.com/mini4141/payout-saga-orchestrator/workflows"
)

// Server implements payoutv1connect.PayoutServiceHandler.
type Server struct {
	Q        *store.Queries
	Temporal client.Client
}

// New returns a Server on a pool and a Temporal client.
func New(pool *pgxpool.Pool, tc client.Client) *Server {
	return &Server{Q: store.New(pool), Temporal: tc}
}

var requests = obs.Counter("api_requests_total", "Connect requests.", "procedure", "code")

// Handler returns the HTTP handler: Connect service (HTTP/1.1, h2c gRPC), /healthz and /metrics.
// When token is non-empty the Connect procedures require "Authorization: Bearer <token>".
func (s *Server) Handler(token string) http.Handler {
	ics := connect.WithInterceptors(authInterceptor(token), metricsInterceptor())
	path, h := payoutv1connect.NewPayoutServiceHandler(s, ics)
	mux := obs.Mux()
	mux.Handle(path, h)
	return mux
}

func authInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if token != "" && req.Header().Get("Authorization") != "Bearer "+token {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing or invalid bearer token"))
			}
			return next(ctx, req)
		}
	}
}

func metricsInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			start := time.Now()
			resp, err := next(ctx, req)
			code := "ok"
			if err != nil {
				code = connect.CodeOf(err).String()
			}
			requests.With(prometheus.Labels{"procedure": req.Spec().Procedure, "code": code}).Inc()
			slog.Info("rpc", "procedure", req.Spec().Procedure, "code", code, "ms", time.Since(start).Milliseconds())
			return resp, err
		}
	}
}

func toProto(p store.Payout) *payoutv1.Payout {
	return &payoutv1.Payout{
		Id: p.ID, MerchantId: p.MerchantID, AmountMinor: p.AmountMinor, Currency: p.Currency,
		TargetCurrency: p.TargetCurrency, BankAccount: p.BankAccount, Status: p.Status,
		IdempotencyKey: p.IdempotencyKey, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), Engine: p.Engine,
	}
}

// CreatePayout inserts the payout (idempotent on the key) and starts workflow payout-<id>.
func (s *Server) CreatePayout(ctx context.Context, req *connect.Request[payoutv1.CreatePayoutRequest]) (*connect.Response[payoutv1.CreatePayoutResponse], error) {
	m := req.Msg
	if m.MerchantId == "" || m.AmountMinor <= 0 || m.IdempotencyKey == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("merchant_id, amount_minor > 0 and idempotency_key are required"))
	}
	engine := strings.ToLower(m.Engine)
	if engine == "" {
		engine = "temporal"
	}
	if engine != "temporal" && engine != "sfn" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(`engine must be "temporal" or "sfn"`))
	}
	currency, target, account := orDefault(m.Currency, "AUD"), orDefault(m.TargetCurrency, "USD"), orDefault(m.BankAccount, "000-111-222")

	p, err := s.Q.InsertPayout(ctx, store.InsertPayoutParams{
		ID: "po_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], MerchantID: m.MerchantId, AmountMinor: m.AmountMinor,
		Currency: currency, TargetCurrency: target, BankAccount: account, Engine: engine, IdempotencyKey: m.IdempotencyKey,
	})
	created := true
	if errors.Is(err, pgx.ErrNoRows) { // key seen before: return the same payout
		created = false
		p, err = s.Q.GetPayoutByKey(ctx, m.IdempotencyKey)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if p.Engine == "temporal" {
		_, err := s.Temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: workflows.WorkflowID(p.ID), TaskQueue: workflows.TaskQueue}, workflows.PayoutWorkflow, p.ID)
		var already *serviceerror.WorkflowExecutionAlreadyStarted
		if err != nil && !errors.As(err, &already) {
			return nil, connect.NewError(connect.CodeUnavailable, err)
		}
	}
	slog.Info("payout created", "payout_id", p.ID, "created", created, "engine", p.Engine)
	return connect.NewResponse(&payoutv1.CreatePayoutResponse{PayoutId: p.ID, Status: p.Status, Created: created}), nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// GetPayout returns the payout, its step attempts and, while running, the workflow's current step.
func (s *Server) GetPayout(ctx context.Context, req *connect.Request[payoutv1.GetPayoutRequest]) (*connect.Response[payoutv1.GetPayoutResponse], error) {
	p, err := s.Q.GetPayout(ctx, req.Msg.PayoutId)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("payout not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	atts, err := s.Q.ListAttempts(ctx, p.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &payoutv1.GetPayoutResponse{Payout: toProto(p)}
	for _, a := range atts {
		resp.Attempts = append(resp.Attempts, &payoutv1.StepAttempt{
			Step: a.Step, Attempt: a.Attempt, Outcome: a.Outcome, Error: a.Error,
			StartedAt: a.StartedAt.UTC().Format(time.RFC3339Nano), DurationMs: a.DurationMs,
		})
	}
	if p.Engine == "temporal" && (p.Status == "PENDING" || p.Status == "PROCESSING") {
		if st, err := s.queryStatus(ctx, p.ID); err == nil {
			resp.CurrentStep = st.Step
		}
	}
	return connect.NewResponse(resp), nil
}

func (s *Server) queryStatus(ctx context.Context, payoutID string) (workflows.Status, error) {
	var st workflows.Status
	v, err := s.Temporal.QueryWorkflow(ctx, workflows.WorkflowID(payoutID), "", workflows.StatusQuery)
	if err != nil {
		return st, err
	}
	err = v.Get(&st)
	return st, err
}

// ListPayouts returns the most recent payouts.
func (s *Server) ListPayouts(ctx context.Context, req *connect.Request[payoutv1.ListPayoutsRequest]) (*connect.Response[payoutv1.ListPayoutsResponse], error) {
	limit := req.Msg.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.Q.ListPayouts(ctx, limit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &payoutv1.ListPayoutsResponse{}
	for _, p := range rows {
		resp.Payouts = append(resp.Payouts, toProto(p))
	}
	return connect.NewResponse(resp), nil
}

// CancelPayout signals the workflow. It is accepted only before the bank transfer step.
func (s *Server) CancelPayout(ctx context.Context, req *connect.Request[payoutv1.CancelPayoutRequest]) (*connect.Response[payoutv1.CancelPayoutResponse], error) {
	p, err := s.Q.GetPayout(ctx, req.Msg.PayoutId)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("payout not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if p.Engine != "temporal" {
		return connect.NewResponse(&payoutv1.CancelPayoutResponse{Detail: "cancel is only supported for the temporal engine"}), nil
	}
	st, err := s.queryStatus(ctx, p.ID)
	if err != nil {
		return connect.NewResponse(&payoutv1.CancelPayoutResponse{Detail: "payout is not running: " + p.Status}), nil
	}
	switch st.Step {
	case "starting", "Reserve", "LockFX", "Screen":
		if err := s.Temporal.SignalWorkflow(ctx, workflows.WorkflowID(p.ID), "", workflows.CancelSignal, nil); err != nil {
			return nil, connect.NewError(connect.CodeUnavailable, err)
		}
		return connect.NewResponse(&payoutv1.CancelPayoutResponse{Accepted: true, Detail: "cancel signalled at step " + st.Step}), nil
	default:
		return connect.NewResponse(&payoutv1.CancelPayoutResponse{Detail: "too late: workflow is at step " + st.Step}), nil
	}
}
