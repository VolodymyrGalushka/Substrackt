-- 0003_sources: configured ingestion channels and per-mailbox IMAP cursors.

CREATE TABLE sources (
    id               TEXT PRIMARY KEY,
    kind             TEXT NOT NULL,
    name             TEXT NOT NULL,
    config           TEXT NOT NULL DEFAULT '{}',
    secret_ref       TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    schedule_minutes INTEGER NOT NULL DEFAULT 15,
    last_sync_at     TEXT NOT NULL DEFAULT '',
    last_error       TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX sources_enabled ON sources (enabled, last_sync_at);

CREATE TABLE mailbox_cursors (
    source_id    TEXT NOT NULL,
    mailbox      TEXT NOT NULL,
    uid_validity INTEGER NOT NULL DEFAULT 0,
    last_uid     INTEGER NOT NULL DEFAULT 0,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (source_id, mailbox),
    FOREIGN KEY (source_id) REFERENCES sources (id) ON DELETE CASCADE
);
