// Package syncstate provides the database-facing API used by provider
// adapters, webhook handlers, and background workers. It keeps callers from
// depending on individual store repositories or PostgreSQL implementation
// details.
package syncstate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"loriss-prototype/internal/store"
)

var ErrInvalidInput = errors.New("syncstate: invalid input")

// Service is the caller-facing entry point for durable synchronization state.
// It is safe to share between concurrent HTTP handlers and workers when the
// supplied store implementation is safe to share.
type Service struct {
	store store.Store
	newID func() uuid.UUID
}

// New constructs the synchronization-state layer around a provider-neutral store.
func New(db store.Store) *Service {
	if db == nil {
		panic("syncstate: store is required")
	}
	return &Service{store: db, newID: uuid.New}
}

// GoogleCalendar returns methods used by the Google Calendar adapter and
// Google webhook handler.
func (s *Service) GoogleCalendar() *GoogleCalendar { return &GoogleCalendar{service: s} }

// Live25 returns methods used by the 25Live adapter and daily poller.
func (s *Service) Live25() *Live25 { return &Live25{service: s} }

// Work returns methods shared by synchronization planners and workers.
func (s *Service) Work() *Work { return &Work{service: s} }

// Ping checks whether the underlying store is reachable.
func (s *Service) Ping(ctx context.Context) error { return s.store.Ping(ctx) }

// GoogleCalendar is the persistence adapter for Google Calendar callers.
type GoogleCalendar struct{ service *Service }

// Mapping returns the managed mapping for a Google event or event instance.
func (g *GoogleCalendar) Mapping(ctx context.Context, ref store.GoogleRef) (store.EventMapping, error) {
	if err := validateGoogleRef(ref); err != nil {
		return store.EventMapping{}, err
	}
	return g.service.store.Mappings().GetByGoogle(ctx, ref)
}

// Watch returns the locally tracked watch used to validate an inbound Google
// notification before it is recorded.
func (g *GoogleCalendar) Watch(ctx context.Context, channelID string) (store.WebhookWatch, error) {
	if channelID == "" {
		return store.WebhookWatch{}, invalid("watch channel ID is required")
	}
	return g.service.store.Watches().GetByChannel(ctx, channelID)
}

// CreateMapping creates a provisioning mapping for a newly eligible Google
// event. It assigns the LORISS ID and fixes the origin to Google.
func (g *GoogleCalendar) CreateMapping(ctx context.Context, mapping store.EventMapping) (store.EventMapping, error) {
	if mapping.Google == nil {
		return store.EventMapping{}, invalid("Google mapping requires a Google reference")
	}
	if err := validateGoogleRef(*mapping.Google); err != nil {
		return store.EventMapping{}, err
	}
	mapping.ID = g.service.newID()
	mapping.Origin = store.SourceGoogle
	if mapping.State == "" {
		mapping.State = store.MappingProvisioning
	}

	var created store.EventMapping
	err := g.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Mappings().Create(ctx, mapping)
		return err
	})
	return created, err
}

// UpdateMapping records a state transition for an existing Google-managed
// mapping. The supplied mapping must carry the current optimistic version.
func (g *GoogleCalendar) UpdateMapping(ctx context.Context, mapping store.EventMapping) (store.EventMapping, error) {
	if mapping.Google == nil {
		return store.EventMapping{}, invalid("Google mapping requires a Google reference")
	}
	if err := validateGoogleRef(*mapping.Google); err != nil {
		return store.EventMapping{}, err
	}
	return updateMapping(ctx, g.service.store, mapping)
}

// BeginWatch stores a locally generated pending Google watch before the caller
// makes the Google watch request. The returned watch ID is used by ActivateWatch.
func (g *GoogleCalendar) BeginWatch(ctx context.Context, watch store.WebhookWatch) (store.WebhookWatch, error) {
	if watch.CalendarID == "" || watch.ChannelID == "" || watch.ExpectedResourceURI == "" || watch.VerificationTokenDigest == "" {
		return store.WebhookWatch{}, invalid("pending watch requires calendar, channel, expected resource URI, and token digest")
	}
	watch.ID = g.service.newID()
	watch.Status = store.WatchPending

	var created store.WebhookWatch
	err := g.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Watches().Create(ctx, watch)
		return err
	})
	return created, err
}

// ActivateWatch saves the resource ID and expiration returned by Google for a
// watch that was previously created with BeginWatch.
func (g *GoogleCalendar) ActivateWatch(ctx context.Context, watch store.WebhookWatch) (store.WebhookWatch, error) {
	if watch.ID == uuid.Nil || watch.ResourceID == "" {
		return store.WebhookWatch{}, invalid("active watch requires ID and resource ID")
	}
	var active store.WebhookWatch
	err := g.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		active, err = tx.Watches().Activate(ctx, watch)
		return err
	})
	return active, err
}

// StopWatch stops a locally tracked Google watch. The caller is responsible
// for stopping the remote watch first when Google requires it.
func (g *GoogleCalendar) StopWatch(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return invalid("watch ID is required")
	}
	return g.service.store.InTx(ctx, func(tx store.Tx) error {
		return tx.Watches().MarkStatus(ctx, id, store.WatchStopped)
	})
}

// RecordNotification durably de-duplicates a validated Google notification
// and creates or coalesces the associated reconciliation job.
func (g *GoogleCalendar) RecordNotification(ctx context.Context, notification store.WebhookNotification, calendarID, trigger string) (bool, store.SyncJob, error) {
	if notification.ChannelID == "" || notification.MessageNumber == "" || calendarID == "" {
		return false, store.SyncJob{}, invalid("notification requires channel, message number, and calendar ID")
	}
	notification.ID = g.service.newID()
	job := store.SyncJob{
		ID:      g.service.newID(),
		Kind:    store.JobGoogleNotification,
		Scope:   calendarID,
		Trigger: trigger,
		Status:  store.JobPending,
	}

	var accepted bool
	var result store.SyncJob
	err := g.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		accepted, result, err = tx.Watches().RecordNotification(ctx, notification, job)
		return err
	})
	return accepted, result, err
}

// Checkpoint returns the Google incremental-sync checkpoint for a calendar.
func (g *GoogleCalendar) Checkpoint(ctx context.Context, calendarID string) (store.SyncCheckpoint, error) {
	if calendarID == "" {
		return store.SyncCheckpoint{}, invalid("calendar ID is required")
	}
	return g.service.store.Checkpoints().Get(ctx, store.SourceGoogle, calendarID)
}

// SaveCheckpoint creates or advances the Google checkpoint. Call it only after
// the complete change listing and its planned operations are durable.
func (g *GoogleCalendar) SaveCheckpoint(ctx context.Context, checkpoint store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	checkpoint.Source = store.SourceGoogle
	return saveCheckpoint(ctx, g.service.store, checkpoint)
}

// Live25 is the persistence adapter for 25Live callers.
type Live25 struct{ service *Service }

// Mapping returns the managed mapping for a 25Live event or occurrence.
func (l *Live25) Mapping(ctx context.Context, ref store.Live25Ref) (store.EventMapping, error) {
	if err := validateLive25Ref(ref); err != nil {
		return store.EventMapping{}, err
	}
	return l.service.store.Mappings().GetByLive25(ctx, ref)
}

// CreateMapping creates a provisioning mapping for a newly eligible 25Live
// event. It assigns the LORISS ID and fixes the origin to 25Live.
func (l *Live25) CreateMapping(ctx context.Context, mapping store.EventMapping) (store.EventMapping, error) {
	if mapping.Live25 == nil {
		return store.EventMapping{}, invalid("25Live mapping requires a 25Live reference")
	}
	if err := validateLive25Ref(*mapping.Live25); err != nil {
		return store.EventMapping{}, err
	}
	mapping.ID = l.service.newID()
	mapping.Origin = store.SourceLive25
	if mapping.State == "" {
		mapping.State = store.MappingProvisioning
	}

	var created store.EventMapping
	err := l.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Mappings().Create(ctx, mapping)
		return err
	})
	return created, err
}

// UpdateMapping records a state transition for an existing 25Live-managed
// mapping. The supplied mapping must carry the current optimistic version.
func (l *Live25) UpdateMapping(ctx context.Context, mapping store.EventMapping) (store.EventMapping, error) {
	if mapping.Live25 == nil {
		return store.EventMapping{}, invalid("25Live mapping requires a 25Live reference")
	}
	if err := validateLive25Ref(*mapping.Live25); err != nil {
		return store.EventMapping{}, err
	}
	return updateMapping(ctx, l.service.store, mapping)
}

// Checkpoint returns the 25Live polling checkpoint for a scope.
func (l *Live25) Checkpoint(ctx context.Context, scope string) (store.SyncCheckpoint, error) {
	if scope == "" {
		return store.SyncCheckpoint{}, invalid("25Live scope is required")
	}
	return l.service.store.Checkpoints().Get(ctx, store.SourceLive25, scope)
}

// SaveCheckpoint creates or advances a 25Live polling checkpoint. Call it only
// after a complete source listing and its planned operations are durable.
func (l *Live25) SaveCheckpoint(ctx context.Context, checkpoint store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	checkpoint.Source = store.SourceLive25
	return saveCheckpoint(ctx, l.service.store, checkpoint)
}

// Work exposes persistence operations shared by synchronization planners and
// workers. It does not perform provider HTTP calls.
type Work struct{ service *Service }

// Mapping returns a managed mapping by its LORISS-owned identifier.
func (w *Work) Mapping(ctx context.Context, id uuid.UUID) (store.EventMapping, error) {
	if id == uuid.Nil {
		return store.EventMapping{}, invalid("mapping ID is required")
	}
	return w.service.store.Mappings().Get(ctx, id)
}

// MappingsInWindow returns managed mappings whose confirmed schedules overlap
// the supplied rolling-horizon interval.
func (w *Work) MappingsInWindow(ctx context.Context, start, end time.Time) ([]store.EventMapping, error) {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil, invalid("mapping window requires a non-empty start/end interval")
	}
	return w.service.store.Mappings().ListInWindow(ctx, start, end)
}

// PlanOperation creates (or returns) the idempotent record for one external
// write. The caller must create it before calling the destination provider.
func (w *Work) PlanOperation(ctx context.Context, operation store.SyncOperation) (store.SyncOperation, error) {
	if operation.Source == "" || operation.Destination == "" || operation.Source == operation.Destination || operation.SourceIdentity == "" || operation.IdempotencyKey == "" || operation.LeaseKey == "" {
		return store.SyncOperation{}, invalid("operation requires distinct source and destination, source identity, idempotency key, and lease key")
	}
	operation.ID = w.service.newID()
	if operation.Status == "" {
		operation.Status = store.OperationPending
	}
	var created store.SyncOperation
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Operations().Create(ctx, operation)
		return err
	})
	return created, err
}

// UpdateOperation persists an operation result after the caller observes the
// provider response. The supplied operation must carry its current version.
func (w *Work) UpdateOperation(ctx context.Context, operation store.SyncOperation) (store.SyncOperation, error) {
	if operation.ID == uuid.Nil || operation.Version < 1 {
		return store.SyncOperation{}, invalid("operation ID and version are required")
	}
	var updated store.SyncOperation
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		updated, err = tx.Operations().Update(ctx, operation)
		return err
	})
	return updated, err
}

// Operation returns an external-write record by LORISS-owned identifier.
func (w *Work) Operation(ctx context.Context, id uuid.UUID) (store.SyncOperation, error) {
	if id == uuid.Nil {
		return store.SyncOperation{}, invalid("operation ID is required")
	}
	return w.service.store.Operations().Get(ctx, id)
}

// OperationByIdempotencyKey returns the durable operation for a planned write.
func (w *Work) OperationByIdempotencyKey(ctx context.Context, key string) (store.SyncOperation, error) {
	if key == "" {
		return store.SyncOperation{}, invalid("idempotency key is required")
	}
	return w.service.store.Operations().GetByIdempotencyKey(ctx, key)
}

// Enqueue creates a durable synchronization job.
func (w *Work) Enqueue(ctx context.Context, job store.SyncJob) (store.SyncJob, error) {
	if job.Kind == "" || job.Scope == "" {
		return store.SyncJob{}, invalid("job kind and scope are required")
	}
	job.ID = w.service.newID()
	if job.Status == "" {
		job.Status = store.JobPending
	}
	var created store.SyncJob
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Jobs().Create(ctx, job)
		return err
	})
	return created, err
}

// ClaimNext atomically claims pending work for a worker.
func (w *Work) ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration) (store.SyncJob, error) {
	if workerID == "" || leaseTTL <= 0 {
		return store.SyncJob{}, invalid("worker ID and positive lease TTL are required")
	}
	return w.service.store.Jobs().ClaimNext(ctx, workerID, leaseTTL)
}

// Job returns a durable synchronization job by its identifier.
func (w *Work) Job(ctx context.Context, id uuid.UUID) (store.SyncJob, error) {
	if id == uuid.Nil {
		return store.SyncJob{}, invalid("job ID is required")
	}
	return w.service.store.Jobs().Get(ctx, id)
}

// UpdateJob persists a job status transition using optimistic concurrency.
func (w *Work) UpdateJob(ctx context.Context, job store.SyncJob) (store.SyncJob, error) {
	if job.ID == uuid.Nil || job.Version < 1 {
		return store.SyncJob{}, invalid("job ID and version are required")
	}
	var updated store.SyncJob
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		updated, err = tx.Jobs().Update(ctx, job)
		return err
	})
	return updated, err
}

// RecordOutcome appends the result for one event handled by a job.
func (w *Work) RecordOutcome(ctx context.Context, item store.SyncJobItem) (store.SyncJobItem, error) {
	if item.JobID == uuid.Nil || item.Outcome == "" {
		return store.SyncJobItem{}, invalid("job item requires job ID and outcome")
	}
	item.ID = w.service.newID()
	var created store.SyncJobItem
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.Jobs().AddItem(ctx, item)
		return err
	})
	return created, err
}

// AcquireLease serializes work for one logical event.
func (w *Work) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (store.Lease, error) {
	if key == "" || owner == "" || ttl <= 0 {
		return store.Lease{}, invalid("lease key, owner, and positive TTL are required")
	}
	var lease store.Lease
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		lease, err = tx.Leases().Acquire(ctx, key, owner, ttl)
		return err
	})
	return lease, err
}

// ConfirmLease verifies ownership immediately before a provider write.
func (w *Work) ConfirmLease(ctx context.Context, key, owner string) error {
	if key == "" || owner == "" {
		return invalid("lease key and owner are required")
	}
	return w.service.store.Leases().Confirm(ctx, key, owner)
}

// ReleaseLease releases a logical-event lease once local work is complete.
func (w *Work) ReleaseLease(ctx context.Context, key, owner string) error {
	if key == "" || owner == "" {
		return invalid("lease key and owner are required")
	}
	return w.service.store.InTx(ctx, func(tx store.Tx) error {
		return tx.Leases().Release(ctx, key, owner)
	})
}

// OpenIssue creates or refreshes an actionable issue.
func (w *Work) OpenIssue(ctx context.Context, issue store.SyncIssue) (store.SyncIssue, error) {
	if issue.Type == "" || issue.Fingerprint == "" {
		return store.SyncIssue{}, invalid("issue type and fingerprint are required")
	}
	if issue.MappingID == nil && (issue.Source == nil || issue.SourceIdentity == "") {
		return store.SyncIssue{}, invalid("unmapped issue requires source and source identity")
	}
	issue.ID = w.service.newID()
	var opened store.SyncIssue
	err := w.service.store.InTx(ctx, func(tx store.Tx) error {
		var err error
		opened, err = tx.Issues().UpsertOpen(ctx, issue)
		return err
	})
	return opened, err
}

// ResolveIssue marks an open issue as resolved.
func (w *Work) ResolveIssue(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return invalid("issue ID is required")
	}
	return w.service.store.InTx(ctx, func(tx store.Tx) error {
		return tx.Issues().Resolve(ctx, id)
	})
}

func updateMapping(ctx context.Context, db store.Store, mapping store.EventMapping) (store.EventMapping, error) {
	if mapping.ID == uuid.Nil || mapping.Version < 1 {
		return store.EventMapping{}, invalid("mapping ID and version are required")
	}
	var updated store.EventMapping
	err := db.InTx(ctx, func(tx store.Tx) error {
		var err error
		updated, err = tx.Mappings().Update(ctx, mapping)
		return err
	})
	return updated, err
}

func saveCheckpoint(ctx context.Context, db store.Store, checkpoint store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	if checkpoint.Scope == "" {
		return store.SyncCheckpoint{}, invalid("checkpoint scope is required")
	}
	var saved store.SyncCheckpoint
	err := db.InTx(ctx, func(tx store.Tx) error {
		var err error
		if checkpoint.Version == 0 {
			saved, err = tx.Checkpoints().Create(ctx, checkpoint)
		} else {
			saved, err = tx.Checkpoints().Update(ctx, checkpoint)
		}
		return err
	})
	return saved, err
}

func validateGoogleRef(ref store.GoogleRef) error {
	if ref.CalendarID == "" || ref.EventID == "" {
		return invalid("Google reference requires calendar ID and event ID")
	}
	return nil
}

func validateLive25Ref(ref store.Live25Ref) error {
	if ref.EventID == "" {
		return invalid("25Live reference requires event ID")
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}
