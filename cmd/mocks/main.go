// Command mocks serves the FX, compliance and bank mock services with failure injection.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/mini4141/payout-saga-orchestrator/internal/mock"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
)

func main() {
	obs.SetupLogger("mocks")
	addr := obs.Env("MOCKS_ADDR", ":8090")
	srv := mock.New(12 * time.Second) // longer than the 10 s bank StartToCloseTimeout
	slog.Info("mocks listening", "addr", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		slog.Error("mocks stopped", "err", err)
		os.Exit(1)
	}
}
