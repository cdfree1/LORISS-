# AGENTS.md — LORISS

## Project overview

**LORISS** (Lightweight Orchestration for Real-time Intuitive Schedule Synchronization) is a UNC Computer Science scheduling integration service. It keeps events in the department's Google Calendar and 25Live synchronized in both directions, without requiring users to copy events manually between systems.

This document guides AI coding agents and contributors working on the project. Treat the decisions and requirements below as the current project scope. Do not implement discarded technology alternatives or speculative product features.

## Committed scope

1. **25Live → Google Calendar:** Discover applicable 25Live events and create, update, or remove corresponding events on the Computer Science Google Calendar.
2. **Google Calendar → 25Live:** Discover applicable Google Calendar events and create, update, or remove their corresponding 25Live events, subject to 25Live validation and permissions.
3. **Six-month rolling horizon:** Synchronization and reconciliation cover events occurring within the next six months. Handle recurring-event instances within that window consistently.
4. **Google Calendar change monitoring:** Use Google Calendar push-notification webhooks to detect relevant changes, then fetch authoritative event data and reconcile changes (including deletions).
5. **25Live change monitoring:** Poll 25Live daily and reconcile additions, modifications, and removals with the Google Calendar.
6. **Conflict checking:** Check proposed changes against corresponding records and 25Live scheduling constraints. Distinct events must not be silently conflated or used to overwrite one another. Record and surface failures or conflicts for investigation.
7. **Unattended operation:** Run as a background service on Carolina CloudApps. Normal synchronization does not require a frontend or Chrome extension.

The 25Live / Series25 WebServices integration is assumed to have functioning authorized **read and write** access. Do not build a scraping fallback, browser automation workflow, or privileged-user Chrome extension.

## Selected technology stack

| Layer | Decision |
| --- | --- |
| Application language | **Go** |
| Service interface | **HTTP server / REST API** (primarily for webhooks and operational endpoints) |
| Persistence | **PostgreSQL** |
| Hosting | **Carolina CloudApps** |
| Calendar integration | **Google Calendar API** |
| Scheduling integration | **25Live / Series25 WebServices API** |
| Dependency management | **Go modules** |
| Source control | **Git / GitHub** |

Keep the service deployable as a Go application on CloudApps. Prefer standard Go tooling and focused dependencies. Do not introduce Python/FastAPI, TypeScript/Node.js, MySQL, Render, or a new frontend platform without an explicit change to scope.

## Architecture and component responsibilities

Aim for a small, clearly separated service with the following logical components. These are responsibilities, not a mandated repository directory structure:

- **HTTP layer:** Accept Google Calendar webhook notifications, verify their legitimacy, expose health endpoints, and initiate fast background reconciliation rather than performing substantial sync work during webhook handling.
- **Google Calendar adapter:** Read events, obtain changed event details, write/update/delete managed calendar events, manage webhook watches, and handle API errors and pagination.
- **25Live adapter:** Read events and resource/room information, create/update/cancel events through supported Series25 operations, and translate API errors into meaningful results.
- **Synchronization engine:** Compare records, decide the appropriate operation, detect conflicts, prevent ping-pong changes, and coordinate API calls and database transactions.
- **Persistent state:** Store cross-system event mappings, source identifiers, revision/fingerprint information, sync cursors or checkpoints as appropriate, webhook channel metadata, run status, errors, and retry state.
- **Background jobs:** Process webhook-triggered Google Calendar changes, perform daily 25Live polling, run reconciliation/recovery tasks, and renew expiring Google Calendar watches.

Prefer interfaces around external adapters so synchronization behavior can be tested without live API access. Keep business rules separate from transport-specific payloads.

## Synchronization semantics

### Identity and ownership

- Maintain an explicit, persistent mapping between each synced 25Live event (and relevant occurrence identity) and its Google Calendar counterpart.
- Treat two events as **the same event only when supported by an established mapping or another deliberately validated external identifier**. Similar titles, rooms, or times are insufficient.
- Track which system originated an event and the last successfully synchronized revisions/state. Preserve this distinction when events are mirrored into the other system.
- Edits to an already-mapped event should update its counterpart; they should not create duplicates.
- If both sides have changed since the last successful sync, treat this as a **concurrent-edit conflict** rather than arbitrarily overwriting one side. Log enough information for resolution.
- Do not presume either system is globally authoritative for every field. If particular fields require 25Live to remain authoritative (for example, 25Live-controlled room booking state), encode those rules explicitly and test them.

### Creating and updating events

- Translate fields deliberately: title, description, start/end times, timezone, location/room, organizer details where available and authorized, recurrence/instance identifiers, and cancellation status.
- Preserve timezone information and use timezone-aware instants. Account for daylight saving time and all-day event semantics.
- Confirm that required 25Live rooms/resources are valid and available before committing a 25Live write. Never imply that a Google Calendar event has secured a 25Live room before the 25Live API confirms it.
- Detect overlapping **distinct** events, particularly when the same room/resource is involved. A mapped mirror is not a conflict with itself.
- Prefer idempotent create/update paths. Retries and duplicate webhook deliveries must not create duplicate events.

### Deletions and cancellations

- Handle deletions in both directions, subject to the 25Live API's actual cancel/delete semantics and UNC permissions.
- **Only modify or delete counterpart events that LORISS can positively identify as managed mappings.** Never remove unrelated events from the Google Calendar or unrelated 25Live reservations.
- Distinguish an explicitly deleted/cancelled source event from an event temporarily missing because of an API failure, access change, pagination bug, or incomplete poll.
- Use tombstones or equivalent state where needed to prevent deleted events from being recreated by delayed notifications or later polling.

### Loop prevention and eventual consistency

- A LORISS write often triggers a new observation from the destination system. Use stored mappings, revisions, origin markers where supported, and/or normalized event fingerprints to identify already-synchronized changes.
- Make synchronization **idempotent** and safe to retry. Handle API rate limits, transient failures, and partial completion with bounded retries and backoff.
- Do not attempt a distributed transaction across Google Calendar and 25Live. Persist enough progress to recover from a successful write in one system followed by failure in the other.
- Serialize or otherwise coordinate work on the same logical event so webhook and polling workers do not race each other.

## Integration and job behavior

### Google Calendar

- Subscribe to notifications for the configured **Computer Science Google Calendar** using the Google Calendar API's supported watch mechanism.
- Validate notification metadata/channel identity; do not treat a notification as the full event payload. Query Google Calendar to determine what actually changed.
- Persist watch/channel IDs, expirations, and any supported synchronization tokens/checkpoints. Renew subscriptions before expiration.
- Support notification replay, missed notifications, token invalidation, pagination, and full resynchronization when incremental state can no longer be trusted.
- Use the configured calendar identity and appropriate Google credentials; do not hardcode IDs or secrets.

### 25Live / Series25

- Use the working Series25 WebServices API integration for reads and writes.
- Poll at least **daily** for relevant events in the rolling six-month horizon, using pagination and filters where supported.
- Compare the latest valid 25Live snapshot with persisted mappings and Google Calendar data. Add missing managed counterparts, update changed events, and handle confirmed removals.
- Treat missing results cautiously: a failed or incomplete snapshot is not evidence that every absent event was deleted.
- Follow the actual API's resource reservation, status, validation, and cancellation rules rather than assuming Google Calendar semantics apply unchanged.

### Reconciliation

- Reconcile events from **now through six months ahead**, and make the horizon roll forward over time.
- Provide recovery reconciliation independent of normal webhook delivery, so events changed while the service was offline can converge.
- Record outcome per event: no-op, created, updated, removed, conflict, retryable error, or permanent/actionable error.
- Make sync jobs observable and resumable; one bad event must not abort an entire batch.

## Database guidance

Use PostgreSQL for durable synchronization metadata. A reasonable initial data model includes:

- **Event mappings:** 25Live ID, Google Calendar ID and event ID, occurrence/recurrence identity when applicable, originating system, sync state, last synced revision/fingerprint for each side, and timestamps.
- **Sync jobs/checkpoints:** job type, trigger source, window, cursor/continuation state, run start/end, and outcome.
- **Webhook subscriptions:** calendar, channel/resource identifiers, expiry, and renewal status.
- **Sync issues:** event mapping reference, error/conflict type, source, diagnostic context, and retry/resolution status.

This is a logical design, not a mandatory exact schema. Make mapping identifiers unique, enforce referential integrity, and use migrations for schema changes. Never store API credentials in the database unless the explicit credential-management design requires it.

## Security and operational expectations

- Configure API credentials, tokens, calendar IDs, database URLs, and other environment-specific settings via CloudApps-supported secret/config mechanisms. Do not commit secrets or real event data to Git.
- Request the minimum Google and 25Live permissions needed; clearly document access requirements.
- Authenticate/verify inbound webhook requests using the notification provider's supported mechanisms and persisted channel identity. HTTPS is required in deployment.
- Limit log exposure of organizer details and sensitive event content. Log identifiers, operation types, statuses, and safe diagnostics by default.
- Provide health/readiness visibility and structured logs for failed writes, missed/restarted jobs, webhook renewal, polling, and reconciliation outcomes.
- Be conservative with destructive operations. Require a known managed mapping and a confirmed source-side deletion/cancellation before propagating removal.

## Working conventions for coding agents

1. **Inspect the repository first.** Follow existing package boundaries, migration tooling, naming patterns, CI workflows, and configuration conventions. The architecture above does not imply that specific directories or commands already exist.
2. **Stay within the committed scope.** Do not implement ConnectCarolina, a schedule-drafting UI, optimization algorithms, a Chrome extension, or alternative technology stacks.
3. **Make targeted changes.** Keep HTTP handling, API adapters, synchronization decisions, and database access separable. Avoid unnecessary infrastructure or broad refactors.
4. **Test synchronization behavior.** Add table-driven Go tests for event identity, create/update/delete decisions, conflicting edits, conflict overlap, deduplication, timezones/DST, retries, recurrence, and incomplete source snapshots.
5. **Mock external APIs in unit tests.** Integration tests may use explicit test credentials and isolated calendars/25Live resources, but must never modify production events by default.
6. **Verify Go changes.** Run `gofmt` on edited Go files and `go test ./...` when the repository is available. If the project provides additional lint/build/test commands, follow them. Report checks not run and why.
7. **Document real interface assumptions.** If an endpoint, field, permission, or behavior is not established by the repository or integration documentation, mark it as needing verification rather than inventing its API contract.
8. **Plan migrations and rollout safety.** New synchronization functionality should support dry-run/read-only verification when practical, especially before enabling deletions or writing to a production 25Live calendar/resource.

## Definition of done for the synchronization phase

The phase is complete when, for the configured Computer Science Google Calendar and supported 25Live scope:

- A new/updated/deleted eligible event in **Google Calendar** is reconciled appropriately to **25Live**.
- A new/updated/deleted eligible event in **25Live** is reconciled appropriately to **Google Calendar** after the next poll or recovery run.
- The active event horizon covers the **next six months** and advances automatically.
- Google Calendar webhook subscriptions work and are renewed; missing/out-of-order notifications and daily 25Live polling do not create duplicates.
- 25Live validation errors and distinct-event scheduling conflicts are surfaced rather than silently overwriting reservations.
- Concurrent edits, network errors, incomplete polls, and partial cross-system writes can be safely retried or flagged for manual resolution.
- The service runs unattended on **Carolina CloudApps**, with PostgreSQL persistence, appropriate secret handling, tests, and operational logging.

## Open implementation details to verify in the codebase

The project brief does **not** specify the exact 25Live endpoints, event fields, reservation-approval rules, Google Calendar ID, authentication arrangements, CloudApps scheduler, or existing repository layout. Use established integration code and UNC-provided API documentation as the source of truth for those details. When something remains unspecified, choose the least destructive behavior, preserve the event for review, and record the assumption clearly.
