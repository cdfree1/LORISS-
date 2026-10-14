-- Tombstoned mappings are retained by policy; no automatic delete trigger is
-- installed here. Operators may run a separately reviewed retention process.
CREATE INDEX event_mappings_tombstoned_at_idx
    ON event_mappings (tombstoned_at)
    WHERE state = 'tombstoned';

CREATE INDEX sync_issues_open_idx ON sync_issues (last_seen_at)
    WHERE status = 'open';
