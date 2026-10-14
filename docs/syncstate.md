# Synchronization state contract

This document defines the database-facing contract for code that interacts
with Google Calendar, 25Live, webhook delivery, and background workers. The
caller-facing contract is `internal/syncstate`. It wraps the
provider-neutral `internal/store` package. Provider adapters must not import
`internal/postgres`, use `pgx`, issue SQL, or access store repositories
directly.

```text
Google Calendar adapter ─┐
25Live adapter           ├─> syncstate.Service ─> internal/store.Store ─> PostgreSQL
Webhook HTTP handler ────┤
Job/reconciliation worker┘
```

The store owns LORISS's durable synchronization state. It does not store raw
provider payloads and it does not call either provider. An adapter translates a
provider event into its stable identity, provider revision, and normalized
fingerprint before using the synchronization-state service. The synchronization engine owns the
decision to create, update, cancel, or surface a conflict.

## Entry point and transaction boundary

The application composition root creates the service with a `store.Store` and
injects the scoped caller adapter; ordinary callers do not receive the store:

```go
state := syncstate.New(db)
google := state.GoogleCalendar()
live25 := state.Live25()
work := state.Work()
```

`syncstate.Service` validates required identities, generates LORISS-owned
UUIDs, and wraps every state transition in `Store.InTx`. Do not make an HTTP
call to Google Calendar or 25Live while one of its methods is executing.

The expected external-write sequence is:

1. Call `work.PlanOperation` to create an idempotent `SyncOperation` and make
   any needed mapping state transition.
2. Commit, then call the destination provider.
3. Call `work.UpdateOperation` to persist the observed result and mark the
   operation succeeded, retryable, failed, or superseded.

This makes a provider write recoverable if the process stops between the HTTP
call and the local update.

## Method summary

These are the methods exposed to callers. Mutating calls are transactional.

| Caller need | Sync-state method |
| --- | --- |
| Find/create/update a Google mapping | `google.Mapping`, `CreateMapping`, `UpdateMapping` |
| Find/create/update a 25Live mapping | `live25.Mapping`, `CreateMapping`, `UpdateMapping` |
| Validate a Google notification channel | `google.Watch` |
| Record a Google notification with work | `google.RecordNotification` |
| Create, activate, or stop a Google watch | `google.BeginWatch`, `ActivateWatch`, `StopWatch` |
| Read or advance a Google/25Live cursor | `google.Checkpoint`/`SaveCheckpoint`; `live25.Checkpoint`/`SaveCheckpoint` |
| Inspect mappings in a rolling horizon | `work.Mapping`, `MappingsInWindow` |
| Persist/recover an external write | `work.PlanOperation`, `Operation`, `OperationByIdempotencyKey`, `UpdateOperation` |
| Create, claim, update, and audit sync jobs | `work.Enqueue`, `Job`, `ClaimNext`, `UpdateJob`, `RecordOutcome` |
| Serialize one logical event | `work.AcquireLease`, `ConfirmLease`, `ReleaseLease` |
| Surface or resolve an actionable conflict/error | `work.OpenIssue`, `ResolveIssue` |

## Google Calendar callers

Google-facing code uses `GoogleRef` as the stable identity of a managed event.
`CalendarID` and `EventID` are both required; `RecurrenceKey` distinguishes an
instance when the provider supplies one.

```go
type GoogleRef struct {
	CalendarID    string
	EventID       string
	RecurrenceKey string
}
```

### Observe an event or fetch a managed counterpart

```go
mapping, err := google.Mapping(ctx, googleRef)
```

`store.ErrNotFound` means that no managed mapping exists. It is **not**
permission to associate the Google event with a similar 25Live event. The
synchronization engine may create a new provisioning mapping only after it has
decided that the event is eligible for synchronization.

When an existing mapped Google event is observed, the caller supplies its
Google revision and normalized Google fingerprint to the synchronization
engine. That engine compares them only with `EventMapping.GoogleRevision` and
`EventMapping.GoogleFingerprint`. A provider fingerprint must never be
compared with a 25Live fingerprint.

### Create or update a Google-side identity

For a Google-originated event, create a mapping with `Origin: SourceGoogle`, a
non-nil `Google` reference, and `State: MappingProvisioning`. For a 25Live
event whose Google counterpart was successfully created, update the existing
mapping with the returned Google reference and revision/fingerprint.

```go
created, err := google.CreateMapping(ctx, store.EventMapping{
	Google: &googleRef,
	// Populate the confirmed schedule and Google baseline when available.
})

updated, err := google.UpdateMapping(ctx, mapping)
```

`Update` requires the current `mapping.Version`; on success it returns the
next version. Treat `store.ErrVersionConflict` as stale work: re-read and
replan rather than retrying the same stale update. `store.ErrAlreadyExists`
means another worker established the provider identity first; re-read the
mapping instead of creating a duplicate.

### Receive a Google push notification

The HTTP handler validates the channel, verification token, and applicable
resource metadata before calling the store. It records only notification
metadata, not event content:

```go
watch, err := google.Watch(ctx, notification.ChannelID)
// Verify the token and resource metadata against watch before continuing.
accepted, job, err := google.RecordNotification(ctx, notification, calendarID, "google-webhook")
```

`RecordNotification` atomically deduplicates `(ChannelID, MessageNumber)` and
creates the job. If `accepted` is false, the delivery is a replay and must not
start another reconciliation. The handler returns promptly; a worker fetches
the authoritative change list later. A notification is never treated as an
event payload.

### Manage Google watches and incremental cursors

```go
watch, err := google.BeginWatch(ctx, pendingWatch)
active, err := google.ActivateWatch(ctx, watchWithGoogleResourceID)
err = google.StopWatch(ctx, watchID)

checkpoint, err := google.Checkpoint(ctx, calendarID)
checkpoint, err = google.SaveCheckpoint(ctx, checkpoint)
```

Create a pending watch before issuing Google `watch`, then activate it only
after the returned resource ID is known. This permits validation of Google's
initial `sync` notification. The Google cursor/sync token belongs in a
`SyncCheckpoint` keyed by `SourceGoogle` and the calendar scope. Advance it
only after a complete authoritative listing and durable follow-up operations.
If Google invalidates a token, schedule a full recovery sync rather than using
an incomplete listing as deletion evidence.

## 25Live callers

25Live-facing code uses the event ID plus the provider's occurrence identity:

```go
type Live25Ref struct {
	EventID       string
	OccurrenceKey string
}
```

### Observe an event or fetch a managed counterpart

```go
mapping, err := live25.Mapping(ctx, live25Ref)
```

As with Google, `store.ErrNotFound` identifies an unmapped 25Live event; it
does not establish a relationship from matching title, room, or time. A new
eligible 25Live event starts a provisioning mapping with
`Origin: SourceLive25` and a non-nil `Live25` reference. After Google confirms
the counterpart creation, update the same mapping with its Google reference.

The adapter passes its current revision and normalized fingerprint to the
synchronization engine. The engine compares them only to
`Live25Revision` and `Live25Fingerprint`, then uses `SyncedFingerprint` for
the provider-neutral converged state.

### Poll and checkpoint a complete 25Live snapshot

```go
live25Source := store.SourceLive25
job, err := work.Enqueue(ctx, store.SyncJob{
	Kind:    store.JobLive25Poll,
	Source:  &live25Source,
	Scope:   live25Scope,
	Trigger: "daily-poll",
	Status:  store.JobPending,
})

checkpoint, err := live25.SaveCheckpoint(ctx, store.SyncCheckpoint{
	Scope:  live25Scope,
	Cursor: nextCursor,
	Version: previous.Version,
})
```

The poller processes the rolling six-month window and advances the checkpoint
only after every page was read successfully and its resulting operations are
durable. A failed, partial, or permission-limited poll is never evidence that
an absent 25Live reservation was deleted. It should leave the prior checkpoint
in place and create retryable per-event or job outcomes instead.

## Shared synchronization methods

### Plan and recover provider writes

```go
operation, err := work.PlanOperation(ctx, store.SyncOperation{
	MappingID:         &mapping.ID,
	Source:            store.SourceGoogle,
	SourceIdentity:    googleIdentity,
	Destination:       store.SourceLive25,
	Kind:              store.OperationCreate, // or Update / Cancel
	IdempotencyKey:    key,
	SourceRevision:    observedRevision,
	SourceFingerprint: observedFingerprint,
	Status:            store.OperationPending,
	LeaseKey:          leaseKey,
})

operation, err = work.UpdateOperation(ctx, operation)
```

The idempotency key must represent the logical event, destination, operation
kind, and source revision/fingerprint. `Create` returns the existing operation
for the same key when appropriate; callers can also use
`GetByIdempotencyKey`. Before retrying an operation, re-read the source and
verify that its revision/fingerprint still matches the operation. If it does
not, mark the operation `OperationSuperseded` and plan a new one.

Only an established mapping permits an update or cancellation of a counterpart.
For uncertain creates, query the destination by the known mapping or LORISS
origin marker before attempting another create. If that cannot establish the
result safely, open an issue rather than guessing.

### Claim jobs and record outcomes

```go
job, err := work.ClaimNext(ctx, workerID, leaseTTL)
job, err = work.UpdateJob(ctx, job)

googleSource := store.SourceGoogle
item, err := work.RecordOutcome(ctx, store.SyncJobItem{
	JobID:          job.ID,
	MappingID:      &mapping.ID,
	Source:         &googleSource,
	SourceIdentity: googleIdentity,
	Outcome:        store.JobItemUpdated,
})
```

Workers claim pending/retryable jobs using a unique `workerID` and a bounded
lease TTL. A job that cannot be completed becomes `JobRetryable` or
`JobFailed`, with a safe `ErrorCode`; a bad event must not abort an otherwise
valid batch. Record `no_op`, `created`, `updated`, `removed`, `conflict`,
`retryable_error`, or `permanent_error` for each processed event.

### Serialize one logical event

```go
lease, err := work.AcquireLease(ctx, leaseKey, workerID, leaseTTL)
err = work.ConfirmLease(ctx, leaseKey, workerID)
err = work.ReleaseLease(ctx, leaseKey, workerID)
```

Use a stable logical-event key (normally a mapping ID, otherwise a
provider-qualified source identity) before planning or executing provider work.
`store.ErrLeaseHeld` means another worker owns unexpired work; leave the job
retryable. Confirm ownership immediately before a provider write. A lease
coordinates workers but does not make an HTTP write transactional, so callers
must still use operation idempotency and mapping versions.

### Surface and resolve issues

```go
live25Source := store.SourceLive25
issue, err := work.OpenIssue(ctx, store.SyncIssue{
	MappingID:      &mapping.ID, // nil for an unmapped source event
	Source:         &live25Source,
	SourceIdentity: live25Identity,
	Type:           "concurrent_edit",
	Fingerprint:    observedFingerprint,
	Diagnostic:     safeDiagnostic,
	Status:         store.IssueOpen,
})
err = work.ResolveIssue(ctx, issue.ID)
```

Use an issue for concurrent edits, validation failures, unknown uncertain
write results, and other actionable errors. Diagnostics must be safe for
operational logs; do not store full descriptions, organizer details, OAuth
tokens, or credentials.

## Error and lifecycle rules

Repository methods return stable storage errors rather than driver errors:

| Error | Caller action |
| --- | --- |
| `store.ErrNotFound` | Treat as absent state; do not infer provider deletion or event identity without an authoritative observation. |
| `store.ErrAlreadyExists` | Re-read the identity/idempotency key; another worker may have completed the same transition. |
| `store.ErrVersionConflict` | Re-read, re-evaluate source state, and replan. Never overwrite with stale data. |
| `store.ErrLeaseHeld` | Leave work retryable and wait for or reclaim after the lease expires. |

Lifecycle states have distinct meanings: `MappingProvisioning` is a
one-sided relationship awaiting counterpart confirmation; `MappingActive` is
fully managed; `MappingConflicted` requires resolution; `MappingFailed`
requires attention; and `MappingTombstoned` prevents delayed observations from
recreating a confirmed deletion. Normal synchronization must transition a
confirmed removal to a tombstone, not delete the mapping record.

## Explicit non-contracts

- Callers do not use matching event titles, rooms, or times as identity.
- Callers do not execute SQL, access PostgreSQL tables, or depend on the
  concrete `postgres.Store` type.
- Callers do not put Google Calendar or 25Live SDK types into `store` values.
- Callers do not hold a database transaction during a network call.
- A webhook does not authorize an inline sync; it only produces durable work.
- A missing event from an incomplete source listing does not authorize a
  cancellation or tombstone.
