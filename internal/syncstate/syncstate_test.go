package syncstate

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"loriss-prototype/internal/store"
)

func TestGoogleCalendarCreateMappingAssignsOriginAndIDInTransaction(t *testing.T) {
	db := newFakeStore()
	generatedID := uuid.MustParse("71b76107-610e-469f-ad80-1aee05c16f03")
	service := New(db)
	service.newID = func() uuid.UUID { return generatedID }

	mapping, err := service.GoogleCalendar().CreateMapping(context.Background(), store.EventMapping{
		Google: &store.GoogleRef{CalendarID: "cs@example.edu", EventID: "google-event"},
	})
	if err != nil {
		t.Fatalf("CreateMapping() error = %v", err)
	}
	if db.transactions != 1 {
		t.Fatalf("transactions = %d, want 1", db.transactions)
	}
	if mapping.ID != generatedID || mapping.Origin != store.SourceGoogle || mapping.State != store.MappingProvisioning {
		t.Fatalf("created mapping = %+v, want generated Google provisioning mapping", mapping)
	}
	if db.mappings.created != mapping {
		t.Fatalf("stored mapping = %+v, want %+v", db.mappings.created, mapping)
	}
}

func TestGoogleCalendarRecordNotificationBuildsDurableJob(t *testing.T) {
	db := newFakeStore()
	ids := []uuid.UUID{uuid.MustParse("3d9d3b47-24d6-48f9-84c8-45e4458cedb1"), uuid.MustParse("39aa9d46-676d-4fec-8526-d5abe56ee5ae")}
	service := New(db)
	service.newID = func() uuid.UUID {
		id := ids[0]
		ids = ids[1:]
		return id
	}
	db.watches.accepted = true

	accepted, job, err := service.GoogleCalendar().RecordNotification(context.Background(), store.WebhookNotification{
		ChannelID: "channel", MessageNumber: "4", ResourceState: "exists",
	}, "cs@example.edu", "google-webhook")
	if err != nil {
		t.Fatalf("RecordNotification() error = %v", err)
	}
	if !accepted || job.ID != idsSentinel() {
		t.Fatalf("RecordNotification() = accepted %v, job %+v; want accepted job returned by repository", accepted, job)
	}
	if db.transactions != 1 {
		t.Fatalf("transactions = %d, want 1", db.transactions)
	}
	if db.watches.notification.ID != uuid.MustParse("3d9d3b47-24d6-48f9-84c8-45e4458cedb1") {
		t.Fatalf("notification ID = %s, want generated ID", db.watches.notification.ID)
	}
	if db.watches.job.ID != uuid.MustParse("39aa9d46-676d-4fec-8526-d5abe56ee5ae") || db.watches.job.Kind != store.JobGoogleNotification || db.watches.job.Status != store.JobPending || db.watches.job.Scope != "cs@example.edu" {
		t.Fatalf("notification job = %+v, want pending Google job", db.watches.job)
	}
}

func TestWorkPlanOperationValidatesAndPersistsTransactionally(t *testing.T) {
	db := newFakeStore()
	operationID := uuid.MustParse("2288f9ca-4b7e-41be-8a2a-0190bf89eab7")
	service := New(db)
	service.newID = func() uuid.UUID { return operationID }

	_, err := service.Work().PlanOperation(context.Background(), store.SyncOperation{
		Source: store.SourceGoogle, Destination: store.SourceGoogle,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid PlanOperation() error = %v, want ErrInvalidInput", err)
	}
	if db.transactions != 0 {
		t.Fatalf("invalid operation started %d transactions, want 0", db.transactions)
	}

	operation, err := service.Work().PlanOperation(context.Background(), store.SyncOperation{
		Source:         store.SourceGoogle,
		SourceIdentity: "google-event",
		Destination:    store.SourceLive25,
		Kind:           store.OperationCreate,
		IdempotencyKey: "google-event:live25:create:v1",
		LeaseKey:       "mapping:1",
	})
	if err != nil {
		t.Fatalf("PlanOperation() error = %v", err)
	}
	if db.transactions != 1 || operation.ID != operationID || operation.Status != store.OperationPending {
		t.Fatalf("planned operation = %+v, transactions = %d", operation, db.transactions)
	}
}

func TestSaveCheckpointCreatesThenUpdates(t *testing.T) {
	db := newFakeStore()
	service := New(db)

	created, err := service.Live25().SaveCheckpoint(context.Background(), store.SyncCheckpoint{Scope: "main", Cursor: "one"})
	if err != nil {
		t.Fatalf("initial SaveCheckpoint() error = %v", err)
	}
	if db.checkpoints.creates != 1 || created.Source != store.SourceLive25 || created.Version != 1 {
		t.Fatalf("created checkpoint = %+v, creates = %d", created, db.checkpoints.creates)
	}

	created.Cursor = "two"
	updated, err := service.Live25().SaveCheckpoint(context.Background(), created)
	if err != nil {
		t.Fatalf("update SaveCheckpoint() error = %v", err)
	}
	if db.checkpoints.updates != 1 || updated.Version != 2 || updated.Cursor != "two" {
		t.Fatalf("updated checkpoint = %+v, updates = %d", updated, db.checkpoints.updates)
	}
}

func idsSentinel() uuid.UUID { return uuid.MustParse("39aa9d46-676d-4fec-8526-d5abe56ee5ae") }

type fakeStore struct {
	transactions int
	mappings     fakeMappings
	operations   fakeOperations
	jobs         fakeJobs
	watches      fakeWatches
	checkpoints  fakeCheckpoints
	issues       fakeIssues
	leases       fakeLeases
}

func newFakeStore() *fakeStore { return &fakeStore{} }

func (s *fakeStore) InTx(_ context.Context, fn func(store.Tx) error) error {
	s.transactions++
	return fn(s)
}
func (s *fakeStore) Ping(context.Context) error              { return nil }
func (s *fakeStore) Mappings() store.MappingRepository       { return &s.mappings }
func (s *fakeStore) Operations() store.OperationRepository   { return &s.operations }
func (s *fakeStore) Jobs() store.JobRepository               { return &s.jobs }
func (s *fakeStore) Watches() store.WatchRepository          { return &s.watches }
func (s *fakeStore) Checkpoints() store.CheckpointRepository { return &s.checkpoints }
func (s *fakeStore) Issues() store.IssueRepository           { return &s.issues }
func (s *fakeStore) Leases() store.LeaseRepository           { return &s.leases }

type fakeMappings struct {
	store.MappingRepository
	created store.EventMapping
}

func (r *fakeMappings) Create(_ context.Context, mapping store.EventMapping) (store.EventMapping, error) {
	r.created = mapping
	return mapping, nil
}

type fakeOperations struct {
	store.OperationRepository
}

func (r *fakeOperations) Create(_ context.Context, operation store.SyncOperation) (store.SyncOperation, error) {
	return operation, nil
}

type fakeJobs struct{ store.JobRepository }

type fakeWatches struct {
	store.WatchRepository
	accepted     bool
	notification store.WebhookNotification
	job          store.SyncJob
}

func (r *fakeWatches) RecordNotification(_ context.Context, notification store.WebhookNotification, job store.SyncJob) (bool, store.SyncJob, error) {
	r.notification = notification
	r.job = job
	return r.accepted, store.SyncJob{ID: idsSentinel()}, nil
}

type fakeCheckpoints struct {
	store.CheckpointRepository
	creates int
	updates int
}

func (r *fakeCheckpoints) Create(_ context.Context, checkpoint store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	r.creates++
	checkpoint.Version = 1
	return checkpoint, nil
}

func (r *fakeCheckpoints) Update(_ context.Context, checkpoint store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	r.updates++
	checkpoint.Version++
	return checkpoint, nil
}

type fakeIssues struct{ store.IssueRepository }
type fakeLeases struct{ store.LeaseRepository }

var _ store.Store = (*fakeStore)(nil)
var _ store.Tx = (*fakeStore)(nil)
