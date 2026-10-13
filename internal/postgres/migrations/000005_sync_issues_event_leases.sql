CREATE TABLE sync_issues (
    id UUID PRIMARY KEY,
    mapping_id UUID NULL REFERENCES event_mappings(id),
    source TEXT NULL CHECK (source IN ('google', 'live25')),
    source_identity TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL,
    fingerprint TEXT NOT NULL DEFAULT '',
    diagnostic TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sync_issues_identity_ck CHECK (
        mapping_id IS NOT NULL OR (source IS NOT NULL AND source_identity <> '')
    )
);

CREATE UNIQUE INDEX sync_issues_open_mapped_uq
    ON sync_issues (mapping_id, type, fingerprint)
    WHERE status = 'open' AND mapping_id IS NOT NULL;

CREATE UNIQUE INDEX sync_issues_open_unmapped_uq
    ON sync_issues (source, source_identity, type, fingerprint)
    WHERE status = 'open' AND mapping_id IS NULL;

CREATE TABLE event_leases (
    lease_key TEXT PRIMARY KEY,
    owner_token TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX event_leases_expiry_idx ON event_leases (expires_at);
