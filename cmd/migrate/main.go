// Command migrate applies goose migrations ("up") and loads demo data ("seed").
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mini4141/payout-saga-orchestrator/db"
	"github.com/mini4141/payout-saga-orchestrator/internal/obs"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

var merchants = []struct {
	ID, Name, Currency string
	Balance            int64
}{
	{"m_acme", "Acme Coffee Roasters", "AUD", 5_000_000},
	{"m_bluebird", "Bluebird Books", "AUD", 3_000_000},
	{"m_cobalt", "Cobalt Cycles", "AUD", 2_500_000},
	{"m_delta", "Delta Dental", "AUD", 8_000_000},
	{"m_ember", "Ember Candles", "AUD", 1_200_000},
}

func main() {
	obs.SetupLogger("migrate")
	cmds := os.Args[1:]
	if len(cmds) == 0 {
		cmds = []string{"up", "seed"}
	}
	dsn := obs.Env("DATABASE_URL", "")
	for _, c := range cmds {
		var err error
		switch c {
		case "up":
			err = up(dsn)
		case "seed":
			err = seed(dsn)
		default:
			err = fmt.Errorf("unknown command %q (want up|seed)", c)
		}
		if err != nil {
			slog.Error("failed", "cmd", c, "err", err)
			os.Exit(1)
		}
		slog.Info("done", "cmd", c)
	}
}

func up(dsn string) error {
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer d.Close()
	goose.SetBaseFS(db.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(d, "migrations")
}

// seed inserts the demo merchants with an opening credit entry each (balance = sum(entries)).
// Re-running it is a no-op for merchants that already have a balance.
func seed(dsn string) error {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	for _, m := range merchants {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		q := store.New(tx)
		if err := q.UpsertMerchant(ctx, store.UpsertMerchantParams{ID: m.ID, Name: m.Name, Currency: m.Currency}); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		cur, err := q.GetMerchantForUpdate(ctx, m.ID)
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if cur.BalanceMinor == 0 {
			if _, err := q.InsertEntry(ctx, store.InsertEntryParams{MerchantID: m.ID, Kind: "credit", AmountMinor: m.Balance}); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			if err := q.AddBalance(ctx, store.AddBalanceParams{ID: m.ID, BalanceMinor: m.Balance}); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
