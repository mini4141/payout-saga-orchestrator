// Command receiver is the webhook receiver for outbox events. It dedupes by event ID.
package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
)

type event struct {
	ID          int64           `json:"id"`
	AggregateID string          `json:"aggregate_id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

func main() {
	obs.SetupLogger("receiver")
	var (
		mu         sync.Mutex
		seen       = map[int64]bool{}
		delivered  []event
		duplicates int
	)
	received := obs.Counter("receiver_events_total", "Webhook events received.", "result")

	mux := obs.Mux()
	mux.HandleFunc("POST /events", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var ev event
		if err := json.Unmarshal(b, &ev); err != nil || ev.ID == 0 {
			http.Error(w, "bad event", http.StatusBadRequest)
			return
		}
		mu.Lock()
		dup := seen[ev.ID]
		if dup {
			duplicates++
		} else {
			seen[ev.ID] = true
			delivered = append(delivered, ev)
		}
		mu.Unlock()
		if dup {
			received.WithLabelValues("duplicate").Inc()
			slog.Info("duplicate event ignored", "event_id", ev.ID, "type", ev.Type, "payout_id", ev.AggregateID)
		} else {
			received.WithLabelValues("delivered").Inc()
			slog.Info("event delivered", "event_id", ev.ID, "type", ev.Type, "payout_id", ev.AggregateID)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// GET /delivered lists unique events; used by the drills to prove one event per payout.
	mux.HandleFunc("GET /delivered", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": delivered, "duplicates_ignored": duplicates})
	})

	addr := obs.Env("RECEIVER_ADDR", ":8095")
	slog.Info("receiver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("receiver stopped", "err", err)
		os.Exit(1)
	}
}
