CREATE TABLE event_mappings (
    id UUID PRIMARY KEY,
    google_calendar_id TEXT NULL,
    google_event_id TEXT NULL,
    google_recurrence_key TEXT NOT NULL DEFAULT '',
    live25_event_id TEXT NULL,
    live25_occurrence_key TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL,
    state TEXT NOT NULL,
    google_revision TEXT NOT NULL DEFAULT '',
    live25_revision TEXT NOT NULL DEFAULT '',
    google_fingerprint TEXT NOT NULL DEFAULT '',
    live25_fingerprint TEXT NOT NULL DEFAULT '',
    synced_fingerprint TEXT NOT NULL DEFAULT '',
    event_time_zone TEXT NULL,
    is_all_day BOOLEAN NOT NULL DEFAULT FALSE,
    all_day_start_date DATE NULL,
    all_day_end_date DATE NULL,
    window_start TIMESTAMPTZ NULL,
    window_end TIMESTAMPTZ NULL,
    version BIGINT NOT NULL DEFAULT 1,
    tombstoned_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT event_mappings_google_ref_ck CHECK (
        (google_calendar_id IS NULL) = (google_event_id IS NULL)
        AND (google_event_id IS NOT NULL OR google_recurrence_key = '')
    ),
    CONSTRAINT event_mappings_live25_ref_ck CHECK (
        live25_event_id IS NOT NULL OR live25_occurrence_key = ''
    ),
    CONSTRAINT event_mappings_provider_ref_ck CHECK (
        google_event_id IS NOT NULL OR live25_event_id IS NOT NULL
    ),
    CONSTRAINT event_mappings_window_ck CHECK (
        (window_start IS NULL AND window_end IS NULL)
        OR (window_start IS NOT NULL AND window_end IS NOT NULL AND window_end > window_start)
    ),
    CONSTRAINT event_mappings_schedule_ck CHECK (
        (window_start IS NULL OR event_time_zone IS NOT NULL)
        AND (
            (is_all_day = TRUE AND all_day_start_date IS NOT NULL AND all_day_end_date IS NOT NULL
                AND all_day_end_date > all_day_start_date)
            OR (is_all_day = FALSE AND all_day_start_date IS NULL AND all_day_end_date IS NULL)
        )
    ),
    CONSTRAINT event_mappings_origin_ck CHECK (origin IN ('google', 'live25')),
    CONSTRAINT event_mappings_state_ck CHECK (state IN ('provisioning', 'active', 'tombstoned', 'conflicted', 'failed')),
    CONSTRAINT event_mappings_tombstone_ck CHECK (
        (state = 'tombstoned') = (tombstoned_at IS NOT NULL)
    ),
    CONSTRAINT event_mappings_version_ck CHECK (version > 0)
);

CREATE UNIQUE INDEX event_mappings_google_identity_uq
    ON event_mappings (google_calendar_id, google_event_id)
    WHERE google_event_id IS NOT NULL;

CREATE UNIQUE INDEX event_mappings_live25_identity_uq
    ON event_mappings (live25_event_id, live25_occurrence_key)
    WHERE live25_event_id IS NOT NULL;

CREATE INDEX event_mappings_window_idx
    ON event_mappings (window_start, window_end)
    WHERE state IN ('provisioning', 'active', 'conflicted');
