// Command relay polls the outbox and publishes events to stdout and a webhook (at-least-once).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

func main() {
	log := obs.SetupLogger("relay")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, obs.Env("DATABASE_URL", ""))
	if err != nil {
		log.Error("db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	hook := obs.Env("RECEIVER_URL", "http://receiver:8095/events")
	published := obs.Counter("outbox_published_total", "Outbox events published.", "type", "result")
	obs.Serve(obs.Env("RELAY_ADDR", ":8096"))
	client := &http.Client{Timeout: 5 * time.Second}

	log.Info("relay started", "webhook", hook)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := poll(ctx, pool, client, hook, published); err != nil {
				log.Warn("poll", "err", err)
			}
		}
	}
}

// poll publishes one batch. The row locks are held until the sent marks commit, so a crash
// between publish and mark re-publishes the event (at-least-once); the receiver dedupes by id.
func poll(ctx context.Context, pool *pgxpool.Pool, client *http.Client, hook string, published *prometheus.CounterVec) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := store.New(tx)
	rows, err := q.ListUnsentOutbox(ctx, 50)
	if err != nil {
		return err
	}
	for _, ev := range rows {
		body, _ := json.Marshal(map[string]any{
			"id": ev.ID, "aggregate_id": ev.AggregateID, "type": ev.Type, "payload": json.RawMessage(ev.Payload),
		})
		// stdout publisher
		fmt.Fprintf(os.Stdout, "%s\n", body)
		// webhook publisher
		resp, err := client.Post(hook, "application/json", bytes.NewReader(body))
		if err != nil {
			published.WithLabelValues(ev.Type, "error").Inc()
			return fmt.Errorf("webhook: %w", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			published.WithLabelValues(ev.Type, "error").Inc()
			return fmt.Errorf("webhook: http %d", resp.StatusCode)
		}
		if err := q.MarkOutboxSent(ctx, ev.ID); err != nil {
			return err
		}
		published.WithLabelValues(ev.Type, "sent").Inc()
		slog.Info("outbox event published", "event_id", ev.ID, "type", ev.Type, "payout_id", ev.AggregateID)
	}
	return tx.Commit(ctx)
}
