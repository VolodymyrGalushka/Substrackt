-- 0001_init: core subscription tracking tables.
PRAGMA foreign_keys = ON;

CREATE TABLE vendors (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    category    TEXT NOT NULL DEFAULT '',
    domains     TEXT NOT NULL DEFAULT '[]',
    aliases     TEXT NOT NULL DEFAULT '[]',
    logo_url    TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX vendors_name_nocase ON vendors (name COLLATE NOCASE);

CREATE TABLE payment_methods (
    id               TEXT PRIMARY KEY,
    label            TEXT NOT NULL,
    type             TEXT NOT NULL,
    issuer           TEXT NOT NULL DEFAULT '',
    network          TEXT NOT NULL DEFAULT '',
    last4            TEXT NOT NULL DEFAULT '',
    exp_month        INTEGER NOT NULL DEFAULT 0,
    exp_year         INTEGER NOT NULL DEFAULT 0,
    wallet_address   TEXT NOT NULL DEFAULT '',
    chain            TEXT NOT NULL DEFAULT '',
    billing_currency TEXT NOT NULL DEFAULT '',
    active           INTEGER NOT NULL DEFAULT 1,
    notes            TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE subscriptions (
    id                TEXT PRIMARY KEY,
    vendor_id         TEXT,
    name              TEXT NOT NULL,
    plan              TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,
    amount_minor      INTEGER NOT NULL,
    currency          TEXT NOT NULL,
    scale             INTEGER NOT NULL DEFAULT 2,
    interval_count    INTEGER NOT NULL DEFAULT 1,
    interval_unit     TEXT NOT NULL DEFAULT 'month',
    anchor_date       TEXT NOT NULL,
    next_renewal      TEXT NOT NULL DEFAULT '',
    started_at        TEXT NOT NULL DEFAULT '',
    trial_ends_at     TEXT NOT NULL DEFAULT '',
    cancelled_at      TEXT NOT NULL DEFAULT '',
    auto_renew        INTEGER NOT NULL DEFAULT 1,
    payment_method_id TEXT,
    source_id         TEXT NOT NULL DEFAULT '',
    external_id       TEXT NOT NULL DEFAULT '',
    confidence        REAL NOT NULL DEFAULT 0,
    tags              TEXT NOT NULL DEFAULT '[]',
    notes             TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    FOREIGN KEY (vendor_id) REFERENCES vendors (id) ON DELETE SET NULL,
    FOREIGN KEY (payment_method_id) REFERENCES payment_methods (id) ON DELETE SET NULL
);

CREATE INDEX subscriptions_next_renewal ON subscriptions (next_renewal);
CREATE INDEX subscriptions_status ON subscriptions (status);
CREATE INDEX subscriptions_vendor ON subscriptions (vendor_id);
CREATE INDEX subscriptions_payment_method ON subscriptions (payment_method_id);
