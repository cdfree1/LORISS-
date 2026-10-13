CREATE TABLE webhook_watches (
    id UUID PRIMARY KEY,
    calendar_id TEXT NOT NULL,
    channel_id TEXT NOT NULL UNIQUE,
    resource_id TEXT NOT NULL DEFAULT '',
    expected_resource_uri TEXT NOT NULL,
    verification_token_digest TEXT NOT NULL,
    expires_at TIMESTAMPTZ NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'active', 'failed', 'stopped')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX webhook_watches_calendar_status_idx ON webhook_watches (calendar_id, status);

CREATE TABLE webhook_notifications (
    id UUID PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES webhook_watches(channel_id),
    message_number TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    resource_state TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (channel_id, message_number)
);

CREATE TABLE sync_checkpoints (
    source TEXT NOT NULL CHECK (source IN ('google', 'live25')),
    scope TEXT NOT NULL,
    cursor TEXT NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source, scope)
);
