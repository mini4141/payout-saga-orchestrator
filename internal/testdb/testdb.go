// Package testdb creates a throw-away, fully migrated Postgres database for integration tests.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mini4141/payout-saga-orchestrator/db"
	"github.com/mini4141/payout-saga-orchestrator/internal/store"
)

// New returns a pool on a fresh database created next to TEST_DATABASE_URL, and drops it on cleanup.
// The test is skipped when TEST_DATABASE_URL is not set.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; start Postgres (docker compose up -d postgres) and export it")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, _ := url.Parse(base)
	u.Path = "/" + name
	dsn := u.String()

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(db.Migrations)
	_ = goose.SetDialect("postgres")
	if err := goose.Up(sqlDB, "migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		admin.Close()
	})
	return pool
}

// Merchant inserts a merchant with an opening credit entry so that balance = sum(entries).
func Merchant(t *testing.T, pool *pgxpool.Pool, id string, balance int64) {
	t.Helper()
	ctx := context.Background()
	q := store.New(pool)
	if err := q.UpsertMerchant(ctx, store.UpsertMerchantParams{ID: id, Name: id, Currency: "AUD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.InsertEntry(ctx, store.InsertEntryParams{MerchantID: id, Kind: "credit", AmountMinor: balance}); err != nil {
		t.Fatal(err)
	}
	if err := q.AddBalance(ctx, store.AddBalanceParams{ID: id, BalanceMinor: balance}); err != nil {
		t.Fatal(err)
	}
}

// Payout inserts a PENDING payout.
func Payout(t *testing.T, pool *pgxpool.Pool, id, merchant string, amount int64) store.Payout {
	t.Helper()
	p, err := store.New(pool).InsertPayout(context.Background(), store.InsertPayoutParams{
		ID: id, MerchantID: merchant, AmountMinor: amount, Currency: "AUD", TargetCurrency: "USD",
		BankAccount: "062-000-1234", Engine: "temporal", IdempotencyKey: "key-" + id,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Redact hides the password of a DSN for logging.
func Redact(dsn string) string {
	if i := strings.Index(dsn, "@"); i > 0 {
		return "…" + dsn[i:]
	}
	return dsn
}
