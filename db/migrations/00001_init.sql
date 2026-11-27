-- +goose Up
CREATE TABLE merchants (
    id            text PRIMARY KEY,
    name          text   NOT NULL,
    currency      text   NOT NULL,
    balance_minor bigint NOT NULL DEFAULT 0 CHECK (balance_minor >= 0)
);

CREATE TABLE payouts (
    id              text PRIMARY KEY,
    merchant_id     text        NOT NULL REFERENCES merchants (id),
    amount_minor    bigint      NOT NULL CHECK (amount_minor > 0),
    currency        text        NOT NULL,
    target_currency text        NOT NULL,
    bank_account    text        NOT NULL,
    status          text        NOT NULL DEFAULT 'PENDING',
    engine          text        NOT NULL DEFAULT 'temporal',
    idempotency_key text        NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payouts_status_idx ON payouts (status);

CREATE TABLE ledger_entries (
    id          bigserial PRIMARY KEY,
    merchant_id text        NOT NULL REFERENCES merchants (id),
    payout_id   text,
    kind        text        NOT NULL CHECK (kind IN ('debit', 'credit')),
    amount_minor bigint     NOT NULL CHECK (amount_minor > 0),
    at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_merchant_idx ON ledger_entries (merchant_id);
-- A payout is captured at most once.
CREATE UNIQUE INDEX ledger_entries_payout_debit_uq ON ledger_entries (payout_id) WHERE kind = 'debit' AND payout_id IS NOT NULL;

CREATE TABLE holds (
    id           bigserial PRIMARY KEY,
    merchant_id  text        NOT NULL REFERENCES merchants (id),
    payout_id    text        NOT NULL UNIQUE,
    amount_minor bigint      NOT NULL CHECK (amount_minor > 0),
    at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE step_results (
    payout_id text        NOT NULL,
    step      text        NOT NULL,
    result    jsonb       NOT NULL,
    at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (payout_id, step)
);

CREATE TABLE step_attempts (
    id          bigserial PRIMARY KEY,
    payout_id   text        NOT NULL,
    step        text        NOT NULL,
    attempt     int         NOT NULL,
    outcome     text        NOT NULL,
    error       text        NOT NULL DEFAULT '',
    started_at  timestamptz NOT NULL,
    duration_ms bigint      NOT NULL
);
CREATE INDEX step_attempts_payout_idx ON step_attempts (payout_id, started_at);

CREATE TABLE outbox (
    id           bigserial PRIMARY KEY,
    aggregate_id text        NOT NULL,
    type         text        NOT NULL,
    payload      jsonb       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    sent_at      timestamptz
);
CREATE INDEX outbox_unsent_idx ON outbox (id) WHERE sent_at IS NULL;

CREATE TABLE recon_reports (
    id         bigserial PRIMARY KEY,
    run_at     timestamptz NOT NULL DEFAULT now(),
    matched    int         NOT NULL,
    missing    int         NOT NULL,
    mismatched int         NOT NULL,
    details    jsonb       NOT NULL DEFAULT '{}'
);

-- +goose Down
DROP TABLE recon_reports, outbox, step_attempts, step_results, holds, ledger_entries, payouts, merchants;
