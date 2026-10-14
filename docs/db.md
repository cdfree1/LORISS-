# Database architecture

LORISS uses PostgreSQL for synchronization state. The database layer owns persistence, transactions, idempotency, and coordination; it does not import or store Google Calendar or 25Live client types.

## Package boundary

```text
cmd/server/                  configuration, pool creation, dependency injection
cmd/migrate/                 explicit migration command
internal/store/              provider-neutral types and repository interfaces
internal/postgres/           PostgreSQL implementation of internal/store
internal/syncstate/          caller-facing transactional facade over internal/store
internal/postgres/migrations/ SQL migrations
internal/sync/               depends on internal/store only
internal/calendar/, live25/  translate provider payloads outside the database layer
```

Use `pgx/v5` and `pgxpool`. The PostgreSQL implementation receives a configured pool; it does not read environment variables or manage credentials. Migrations run as a deployment step, not automatically in every server process.

## Store contract

`internal/store` defines plain Go types and interfaces. It must not import the sync engine, provider adapters, or provider SDKs.

Provider adapters, webhook handlers, and workers access those repositories
through `internal/syncstate`, which validates caller inputs, assigns
LORISS-owned IDs, and owns transactional state transitions. `store.Store`
remains the narrow dependency injected into that facade; callers do not issue
SQL or depend on the concrete PostgreSQL implementation. See
[`syncstate.md`](syncstate.md) for its public methods and usage rules.

```go
type Source string // google | live25

type Store interface {
	InTx(context.Context, func(Tx) error) error
	Ping(context.Context) error

	Mappings() MappingRepository
	Operations() OperationRepository
	Jobs() JobRepository
	Watches() WatchRepository
	Checkpoints() CheckpointRepository
	Issues() IssueRepository
	Leases() LeaseRepository
}
```

`Tx` exposes the same repositories under one transaction. Repository methods return stable storage errors (`ErrNotFound`, `ErrVersionConflict`, `ErrAlreadyExists`, `ErrLeaseHeld`) rather than driver-specific errors.

### Core values

```go
type GoogleRef struct {
	CalendarID    string
	EventID       string
	RecurrenceKey string
}

type Live25Ref struct {
	EventID       string
	OccurrenceKey string
}

type EventMapping struct {
	ID                uuid.UUID
	Google            *GoogleRef
	Live25            *Live25Ref
	Origin            Source
	State             MappingState
	GoogleRevision    string
	Live25Revision    string
	GoogleFingerprint string
	Live25Fingerprint string
	SyncedFingerprint string
	EventTimeZone     *string
	AllDay            bool
	AllDayStartDate   *time.Time // date-only, inclusive
	AllDayEndDate     *time.Time // date-only, exclusive
	WindowStart       *time.Time
	WindowEnd         *time.Time
	Version           int64
	TombstonedAt      *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
```

Fingerprints are hashes of normalized event state. `GoogleRevision`, `Live25Revision`, and their corresponding fingerprints are the last successfully synchronized baselines, not merely the last observation. A fetched provider event is compared only to that provider's stored baseline. Provider fingerprints are never compared directly with one another: different providers can represent the same schedule differently. `SyncedFingerprint` is a hash of LORISS's provider-neutral canonical projection (the synchronization-owned title, description, timing, timezone, all-day status, location/resource, recurrence/occurrence, cancellation, and other explicitly mapped fields) and is recorded on successful convergence.

## Tables

| Table | Purpose |
| --- | --- |
| `event_mappings` | Managed Google Calendar ↔ 25Live event and occurrence identity, synchronization state, revisions, fingerprints, and tombstones. |
| `sync_operations` | Idempotent planned writes, attempts, retries, and recovery from partial cross-system writes. |
| `sync_jobs` | Webhook, polling, recovery, and watch-renewal runs. |
| `sync_job_items` | Per-event outcome for a sync job. |
| `webhook_watches` | Google Calendar watch channel/resource identity, expiration, and token state. |
| `webhook_notifications` | De-duplication of received Google notification messages. |
| `sync_checkpoints` | Source-scoped cursors and sync tokens. |
| `sync_issues` | Unresolved conflicts and actionable failures. |
| `event_leases` | Expiring distributed locks for a source event or mapping. |

All persisted schedule instants are `timestamptz` (UTC), but UTC instants alone are not sufficient to preserve calendar semantics. Every confirmed schedule also records its IANA event timezone. All-day events retain their local start and exclusive end dates; their UTC bounds are derived values used only for horizon queries. The campus timezone, `America/New_York`, is a fallback for interpreting a 25Live value that lacks a timezone, never a substitute for the event timezone stored with a mapping. State columns are `text` with `CHECK` constraints instead of PostgreSQL enums.

### `event_mappings`

Required columns:

| Field | Type | Description and reason |
| --- | --- | --- |
| `id` | `UUID` | LORISS-owned immutable mapping identifier. It is the primary key and lets jobs, operations, and issues refer to a mapping without depending on either provider's identifier format. Generated by the application. |
| `google_calendar_id` | `TEXT NULL` | Google Calendar containing the managed event. Google event IDs are only meaningful inside a calendar, so this is part of Google-side identity. It is null while provisioning a Google counterpart from a 25Live-originated event. |
| `google_event_id` | `TEXT NULL` | Google Calendar event ID for the mapped event or recurrence instance. It is used to fetch, update, and delete only the managed Google event. It is null until Google confirms creation. |
| `google_recurrence_key` | `TEXT NOT NULL DEFAULT ''` | Stable recurrence-instance discriminator, normally the normalized original occurrence start when Google supplies one. It distinguishes instances when a provider operation is series-oriented or an event-ID behavior needs additional validation. Empty means a non-recurring event or a series-level mapping. |
| `live25_event_id` | `TEXT NULL` | 25Live event/reservation identifier for the mapped event. It is used to read, update, and cancel only the known managed counterpart. It is null while provisioning a 25Live counterpart from a Google-originated event. |
| `live25_occurrence_key` | `TEXT NOT NULL DEFAULT ''` | Stable 25Live occurrence discriminator. It distinguishes reservations/occurrences belonging to the same 25Live event when the integration supports recurrence. Empty means no separate occurrence identity is available. |
| `origin` | `TEXT` | Source that first created the managed relationship: `google` or `live25`. It supports provenance, loop prevention, and recovery; it does not make that provider globally authoritative for every field. |
| `state` | `TEXT` | Current lifecycle state, constrained to supported values such as `provisioning`, `active`, `tombstoned`, `conflicted`, or `failed`. It prevents the sync engine from treating incomplete, cancelled, or blocked relationships as ordinary active mappings. |
| `google_revision` | `TEXT NOT NULL DEFAULT ''` | Google provider version at the last successful synchronization, such as an ETag or updated revision. |
| `live25_revision` | `TEXT NOT NULL DEFAULT ''` | 25Live provider version at the last successful synchronization. |
| `google_fingerprint` | `TEXT NOT NULL DEFAULT ''` | Hash of the normalized, synchronization-relevant Google fields at the last successful synchronization. |
| `live25_fingerprint` | `TEXT NOT NULL DEFAULT ''` | Hash of the normalized 25Live fields at the last successful synchronization. |
| `synced_fingerprint` | `TEXT NOT NULL DEFAULT ''` | Hash of the last converged provider-neutral canonical projection. It is never compared to a provider-specific fingerprint. |
| `event_time_zone` | `TEXT NULL` | IANA timezone used to interpret the confirmed local schedule and recurrence. It is required when a confirmed schedule exists, including all-day events. |
| `is_all_day` | `BOOLEAN NOT NULL DEFAULT false` | Whether the event uses date-only, exclusive-end semantics rather than timed instants. |
| `all_day_start_date` | `DATE NULL` | Inclusive local date for an all-day event. It is null for timed or unscheduled mappings. |
| `all_day_end_date` | `DATE NULL` | Exclusive local end date for an all-day event. It is null for timed or unscheduled mappings. |
| `window_start` | `TIMESTAMPTZ NULL` | Start instant of the last confirmed mapped occurrence. It supports indexed selection of active mappings in LORISS's rolling six-month horizon. Null is allowed while an event is incomplete or lacks a confirmed schedule. |
| `window_end` | `TIMESTAMPTZ NULL` | End instant paired with `window_start`. It supports overlap and horizon queries and makes schedule duration explicit. Null is allowed for the same provisioning cases as `window_start`. |
| `version` | `BIGINT NOT NULL DEFAULT 1` | LORISS optimistic-concurrency counter. Updates use `WHERE id = $1 AND version = $2`, then increment it, preventing workers from silently overwriting each other's mapping state. |
| `tombstoned_at` | `TIMESTAMPTZ NULL` | Time at which a confirmed deletion/cancellation became a tombstone. Retaining it prevents delayed webhooks or stale polling results from recreating a deleted managed event. Null means the mapping is not tombstoned. |
| `created_at` | `TIMESTAMPTZ` | Time the mapping record was first created. It supports auditability, provisioning diagnostics, and retention decisions. |
| `updated_at` | `TIMESTAMPTZ` | Time of the most recent local mapping mutation. It supports operational diagnosis and stale-work detection; it is not a provider revision. |

Mappings may initially have one provider reference absent while a counterpart is being created. A mapping has exactly one granularity: a series-level relationship or a specific occurrence relationship. For occurrence mappings, `google_event_id` must be the provider's specific instance ID, not the recurring-series ID. `google_recurrence_key` is retained as an additional validation key and is not an alternate identity that allows duplicate mappings for one Google event.

Enforce identity with partial unique indexes:

```sql
CREATE UNIQUE INDEX event_mappings_google_identity_uq
    ON event_mappings (google_calendar_id, google_event_id)
    WHERE google_event_id IS NOT NULL;

CREATE UNIQUE INDEX event_mappings_live25_identity_uq
    ON event_mappings (live25_event_id, live25_occurrence_key)
    WHERE live25_event_id IS NOT NULL;

CREATE INDEX event_mappings_window_idx
    ON event_mappings (window_start, window_end)
    WHERE state IN ('provisioning', 'active', 'conflicted');
```

Require the following `CHECK` constraints in addition to the indexes:

- A Google reference is all-or-nothing: `google_calendar_id IS NULL` if and only if `google_event_id IS NULL`; when absent, `google_recurrence_key = ''`.
- A missing 25Live reference has `live25_occurrence_key = ''`.
- At least one provider event ID is present.
- `window_start` and `window_end` are both null or both non-null, and a non-null range has `window_end > window_start`.
- A confirmed schedule has an `event_time_zone`. An all-day schedule has both dates and `all_day_end_date > all_day_start_date`; a timed schedule has no all-day dates.
- `origin` and `state` are constrained to the documented supported values.

Do not delete mappings during normal synchronization; transition them to `tombstoned` and retain them according to an explicit retention policy.

### `sync_operations`

Each operation records a source identity, mapping ID when available, direction, operation kind (`create`, `update`, `cancel`), idempotency key, source revision/fingerprint, status, attempt count, retry time, lease key, and safe error code. The source remains authoritative: a retry re-reads it and proceeds only if its revision/fingerprint still matches the operation. If it has changed, the operation is superseded and replanned from the new source state.

`idempotency_key` is unique. A create or update is retried through the same operation record, including after lease reacquisition. Its key is derived from mapping/logical event identity, destination, operation kind, and source revision or fingerprint; an obsolete operation is never reused for a later source revision. If the destination supports an idempotency key or an opaque LORISS origin marker, include the operation ID in that write. After an uncertain create result, reconciliation queries the destination by its known mapping or marker before issuing another create; if it cannot identify the result safely, it records an issue instead of guessing.

### Jobs and checkpoints

`sync_jobs` is the durable database work queue. It stores kind, trigger metadata, rolling-horizon bounds, cursor/checkpoint metadata, worker ownership, status, and timestamps. Inserting a notification and its deduplicated pending reconciliation job occurs in the same database transaction; there is no separate, non-transactional enqueue between those steps. `sync_job_items` records the per-event result: `no_op`, `created`, `updated`, `removed`, `conflict`, `retryable_error`, or `permanent_error`.

`sync_checkpoints` is keyed by `(source, scope)` and has an optimistic version. One planner at a time holds an ordinary expiring lease for a source scope. It may record progress in its job row, but advances the durable source cursor or sync token only after the complete listing succeeds and its derived operations are stored. If the worker stops partway through a listing, the next job repeats from the previous checkpoint; unique operations make that safe. A failed or incomplete 25Live listing cannot be used as evidence that missing events were deleted.

### Watches and notification delivery

`webhook_watches` stores calendar ID, channel ID, resource ID, expected resource URI, expiration, status, and a digest of a random webhook verification token. `channel_id` is unique. `resource_id` is not globally unique: replacement watches for the same calendar legitimately overlap and identify the same watched resource. The current Google sync token remains in the source checkpoint, not the watch row.

Before calling Google `watch`, insert a `pending` watch row with the locally generated unique channel ID, expected calendar/resource URI, and verification-token digest. This permits the initial `sync` notification, which Google may deliver before the watch response, to be validated by channel and token. Persist the returned resource ID and mark the row active in a follow-up transaction; subsequently require a matching resource ID as well. Mark and clean up pending rows if the watch request fails or never completes.

`webhook_notifications` has a unique `(channel_id, message_number)` constraint. It records the resource state and received time, but not event content. Webhook handlers verify the channel, token, and applicable resource identity; insert the notification and create or coalesce a pending database job in one transaction; then return quickly. They do not synchronize inline.

### Issues and leases

`sync_issues` records mapping/source/type/fingerprint, a source identity for unmapped events, safe diagnostic context, first/last seen timestamps, and resolution state. Use separate partial unique indexes for mapped and unmapped issues so PostgreSQL null semantics do not create duplicate unresolved issues.

`event_leases` is keyed by a stable logical event key. It stores an owner token and expiration. Acquisition is an atomic conditional upsert, and expired leases are reclaimable. A worker confirms it still owns the lease before starting provider work; mapping and operation completion use optimistic versions so a stale worker cannot silently replace newer local state. Workers must not hold a database transaction open while calling Google or 25Live. Since a lease cannot fence an HTTP provider call, an uncertain result is resolved by reading the destination before retrying or compensating.

## Transaction and consistency model

PostgreSQL is the system of record for LORISS state. Every local state transition uses `Store.InTx` and is committed atomically: mapping/version changes, operation creation or completion, job claims/statuses, notifications, issue upserts, leases, and checkpoint advancement. Database constraints, foreign keys, unique indexes, and conditional updates are part of that transaction rather than application-only conventions. Workers claim durable jobs with row locking (for example, `FOR UPDATE SKIP LOCKED`) and an expiring lease, so a job has one active owner at a time and abandoned claims can be reclaimed safely.

Google Calendar and 25Live HTTP calls cannot be included in a PostgreSQL transaction. LORISS therefore uses a small persisted saga: commit an idempotent operation first; make the external call outside a transaction; then commit the observed result with the operation ID and mapping/checkpoint version checks. A crash at any boundary leaves a durable operation that reconciliation can retry or flag for review. The design does not claim cross-provider exactly-once delivery or distributed ACID; it provides ACID local state and idempotent, recoverable external effects.

## Synchronization invariants

- A counterpart is modified or cancelled only when a managed mapping identifies it.
- Similar titles, locations, and times never establish identity.
- A mapping update uses `version` for optimistic concurrency.
- A source write has an idempotency key and committed operation record before external work begins.
- Provider-specific fingerprints are compared only to their own last-converged baselines; cross-provider convergence uses the canonical fingerprint.
- A successful external write followed by a local failure remains recoverable through `sync_operations` and provider origin markers where available.
- A source deletion becomes a tombstone only after a complete, authoritative observation.
- Event work is serialized with an expiring lease; webhook and polling workers may not independently write the same logical event.
- Notification receipt and its durable reconciliation job are committed atomically.
- A checkpoint advances only after the complete source listing and its follow-up operations are durable.
- The database keeps identifiers, revisions, fingerprints, and safe diagnostics by default. Event descriptions and organizer details are not stored.

## Migrations

```text
000001_event_mappings.sql
000002_sync_operations.sql
000003_sync_jobs.sql
000004_webhook_watches_notifications_checkpoints.sql
000005_sync_issues_event_leases.sql
000006_indexes_constraints_retention.sql
```

The migration runner takes a PostgreSQL advisory lock before applying migrations and records the applied versions. Production deployments use forward-only migrations; optional down scripts are for local development and are never an automatic deployment rollback mechanism. UUIDs are generated by the application, avoiding an extension dependency.

## Tests

Run repository behavior tests against PostgreSQL, covering:

- Google and 25Live identity uniqueness, including occurrences.
- Reference tuple and schedule `CHECK` constraints, including all-day exclusive end dates and DST/timezone preservation.
- Provider-specific versus canonical fingerprint comparisons.
- Idempotent operation creation and retry.
- Optimistic-lock conflicts.
- Complete versus incomplete source checkpoints.
- Recovery after an interrupted listing from the prior checkpoint.
- Webhook notification replay.
- Pending-watch `sync` delivery before the watch response and overlapping watch renewal for one resource.
- Atomic notification/job insertion and rollback.
- Lease expiration, stale-worker version rejection, and reacquisition.
- Tombstone retention.
- Concurrent mapping creation.
- Applying all migrations to an empty database and migration-lock contention.
