// Command api serves the Connect PayoutService.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"connectrpc.com/grpcreflect"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/mini4141/payout-saga-orchestrator/gen/payout/v1/payoutv1connect"
	"github.com/mini4141/payout-saga-orchestrator/internal/api"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/temporalx"
)

func main() {
	log := obs.SetupLogger("api")
	shutdown := obs.SetupTracing("api")
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

	srv := api.New(pool, tc)
	h := srv.Handler(os.Getenv("API_TOKEN"))
	mux := h.(*http.ServeMux)
	reflector := grpcreflect.NewStaticReflector(payoutv1connect.PayoutServiceName)
	mux.Handle(grpcreflect.NewHandlerV1(reflector))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector))

	addr := obs.Env("API_ADDR", ":8080")
	log.Info("api listening", "addr", addr)
	// h2c lets grpcurl (gRPC over HTTP/2 cleartext) and curl (Connect over HTTP/1.1) use one port.
	if err := http.ListenAndServe(addr, h2c.NewHandler(mux, &http2.Server{})); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}
