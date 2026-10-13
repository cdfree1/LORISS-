CREATE TABLE sync_jobs (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('google_notification', 'live25_poll', 'recovery', 'watch_renewal')),
    source TEXT NULL CHECK (source IN ('google', 'live25')),
    scope TEXT NOT NULL DEFAULT '',
    trigger TEXT NOT NULL DEFAULT '',
    window_start TIMESTAMPTZ NULL,
    window_end TIMESTAMPTZ NULL,
    cursor TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'retryable', 'failed', 'cancelled')),
    owner_token TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ NULL,
    run_started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    error_code TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sync_jobs_window_ck CHECK (
        (window_start IS NULL AND window_end IS NULL)
        OR (window_start IS NOT NULL AND window_end IS NOT NULL AND window_end > window_start)
    )
);

CREATE INDEX sync_jobs_claim_idx ON sync_jobs (created_at)
    WHERE status IN ('pending', 'retryable');

-- A burst of Google messages for the same calendar is coalesced into one
-- durable pending/running reconciliation job.
CREATE UNIQUE INDEX sync_jobs_google_notification_pending_uq
    ON sync_jobs (kind, source, scope)
    WHERE kind = 'google_notification' AND status IN ('pending', 'running');

CREATE TABLE sync_job_items (
    id UUID PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES sync_jobs(id) ON DELETE CASCADE,
    mapping_id UUID NULL REFERENCES event_mappings(id),
    source TEXT NULL CHECK (source IN ('google', 'live25')),
    source_identity TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL CHECK (outcome IN ('no_op', 'created', 'updated', 'removed', 'conflict', 'retryable_error', 'permanent_error')),
    error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sync_job_items_job_idx ON sync_job_items (job_id);
