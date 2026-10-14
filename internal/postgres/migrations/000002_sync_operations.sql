CREATE TABLE sync_operations (
    id UUID PRIMARY KEY,
    mapping_id UUID NULL REFERENCES event_mappings(id),
    source TEXT NOT NULL CHECK (source IN ('google', 'live25')),
    source_identity TEXT NOT NULL,
    destination TEXT NOT NULL CHECK (destination IN ('google', 'live25')),
    kind TEXT NOT NULL CHECK (kind IN ('create', 'update', 'cancel')),
    idempotency_key TEXT NOT NULL UNIQUE,
    source_revision TEXT NOT NULL DEFAULT '',
    source_fingerprint TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'retryable', 'superseded', 'failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NULL,
    lease_key TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NULL,
    CONSTRAINT sync_operations_direction_ck CHECK (source <> destination)
);

CREATE INDEX sync_operations_ready_idx
    ON sync_operations (next_attempt_at, created_at)
    WHERE status IN ('pending', 'retryable');
