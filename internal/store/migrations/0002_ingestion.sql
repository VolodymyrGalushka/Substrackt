-- 0002_ingestion: background jobs, artifacts, review queue, settings and LLM
-- telemetry. M1 tables are untouched.

CREATE TABLE jobs (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    status       TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',
    progress     INTEGER NOT NULL DEFAULT 0,
    message      TEXT NOT NULL DEFAULT '',
    error        TEXT NOT NULL DEFAULT '',
    attempts     INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    created_at   TEXT NOT NULL,
    started_at   TEXT NOT NULL DEFAULT '',
    finished_at  TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL
);

CREATE INDEX jobs_status_created ON jobs (status, created_at);

CREATE TABLE artifacts (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    media_type   TEXT NOT NULL DEFAULT 'text/plain',
    content_hash TEXT NOT NULL,
    content      TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    metadata     TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);

CREATE UNIQUE INDEX artifacts_hash ON artifacts (content_hash);

CREATE TABLE extractions (
    id                    TEXT PRIMARY KEY,
    job_id                TEXT,
    artifact_id           TEXT,
    source                TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL,
    vendor_name           TEXT NOT NULL DEFAULT '',
    name                  TEXT NOT NULL DEFAULT '',
    plan                  TEXT NOT NULL DEFAULT '',
    amount_minor          INTEGER NOT NULL DEFAULT 0,
    currency              TEXT NOT NULL DEFAULT '',
    scale                 INTEGER NOT NULL DEFAULT 2,
    interval_count        INTEGER NOT NULL DEFAULT 1,
    interval_unit         TEXT NOT NULL DEFAULT 'month',
    anchor_date           TEXT NOT NULL DEFAULT '',
    next_renewal          TEXT NOT NULL DEFAULT '',
    desired_status        TEXT NOT NULL DEFAULT 'active',
    payment_hint          TEXT NOT NULL DEFAULT '',
    confidence            REAL NOT NULL DEFAULT 0,
    model                 TEXT NOT NULL DEFAULT '',
    rationale             TEXT NOT NULL DEFAULT '',
    evidence              TEXT NOT NULL DEFAULT '',
    match_subscription_id TEXT,
    match_kind            TEXT NOT NULL DEFAULT 'none',
    raw                   TEXT NOT NULL DEFAULT '{}',
    error                 TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    resolved_at           TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE SET NULL,
    FOREIGN KEY (artifact_id) REFERENCES artifacts (id) ON DELETE SET NULL,
    FOREIGN KEY (match_subscription_id) REFERENCES subscriptions (id) ON DELETE SET NULL
);

CREATE INDEX extractions_status_created ON extractions (status, created_at);
CREATE INDEX extractions_artifact ON extractions (artifact_id);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE llm_cache (
    key        TEXT PRIMARY KEY,
    model      TEXT NOT NULL,
    result     TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE llm_usage (
    id                TEXT PRIMARY KEY,
    model             TEXT NOT NULL,
    base_url          TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    ok                INTEGER NOT NULL DEFAULT 0,
    cached            INTEGER NOT NULL DEFAULT 0,
    error             TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL
);

CREATE INDEX llm_usage_created ON llm_usage (created_at);
