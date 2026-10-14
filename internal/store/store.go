// Package store defines provider-neutral persistence contracts for LORISS.
//
// It deliberately contains no Google Calendar or 25Live types.  Adapters turn
// provider payloads into these values before handing them to the sync engine.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound        = errors.New("store: not found")
	ErrVersionConflict = errors.New("store: version conflict")
	ErrAlreadyExists   = errors.New("store: already exists")
	ErrLeaseHeld       = errors.New("store: lease held")
)

type Source string

const (
	SourceGoogle Source = "google"
	SourceLive25 Source = "live25"
)

type MappingState string

const (
	MappingProvisioning MappingState = "provisioning"
	MappingActive       MappingState = "active"
	MappingTombstoned   MappingState = "tombstoned"
	MappingConflicted   MappingState = "conflicted"
	MappingFailed       MappingState = "failed"
)

type GoogleRef struct {
	CalendarID    string
	EventID       string
	RecurrenceKey string
}

type Live25Ref struct {
	EventID       string
	OccurrenceKey string
}

// EventMapping is the durable identity and last-converged baseline for one
// provider event (or occurrence) and its managed counterpart.
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
	AllDayStartDate   *time.Time
	AllDayEndDate     *time.Time
	WindowStart       *time.Time
	WindowEnd         *time.Time
	Version           int64
	TombstonedAt      *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type OperationKind string

const (
	OperationCreate OperationKind = "create"
	OperationUpdate OperationKind = "update"
	OperationCancel OperationKind = "cancel"
)

type OperationStatus string

const (
	OperationPending    OperationStatus = "pending"
	OperationRunning    OperationStatus = "running"
	OperationSucceeded  OperationStatus = "succeeded"
	OperationRetryable  OperationStatus = "retryable"
	OperationSuperseded OperationStatus = "superseded"
	OperationFailed     OperationStatus = "failed"
)

// SyncOperation is a persisted, idempotent external write. Source revision
// and fingerprint are revalidated before a retry is sent to a provider.
type SyncOperation struct {
	ID                uuid.UUID
	MappingID         *uuid.UUID
	Source            Source
	SourceIdentity    string
	Destination       Source
	Kind              OperationKind
	IdempotencyKey    string
	SourceRevision    string
	SourceFingerprint string
	Status            OperationStatus
	AttemptCount      int
	NextAttemptAt     *time.Time
	LeaseKey          string
	ErrorCode         string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
}

type JobKind string

const (
	JobGoogleNotification JobKind = "google_notification"
	JobLive25Poll         JobKind = "live25_poll"
	JobRecovery           JobKind = "recovery"
	JobWatchRenewal       JobKind = "watch_renewal"
)

type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobRetryable JobStatus = "retryable"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

type SyncJob struct {
	ID             uuid.UUID
	Kind           JobKind
	Source         *Source
	Scope          string
	Trigger        string
	WindowStart    *time.Time
	WindowEnd      *time.Time
	Cursor         string
	Status         JobStatus
	OwnerToken     string
	LeaseExpiresAt *time.Time
	RunStartedAt   *time.Time
	FinishedAt     *time.Time
	ErrorCode      string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type JobItemOutcome string

const (
	JobItemNoOp           JobItemOutcome = "no_op"
	JobItemCreated        JobItemOutcome = "created"
	JobItemUpdated        JobItemOutcome = "updated"
	JobItemRemoved        JobItemOutcome = "removed"
	JobItemConflict       JobItemOutcome = "conflict"
	JobItemRetryableError JobItemOutcome = "retryable_error"
	JobItemPermanentError JobItemOutcome = "permanent_error"
)

type SyncJobItem struct {
	ID             uuid.UUID
	JobID          uuid.UUID
	MappingID      *uuid.UUID
	Source         *Source
	SourceIdentity string
	Outcome        JobItemOutcome
	ErrorCode      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type WatchStatus string

const (
	WatchPending WatchStatus = "pending"
	WatchActive  WatchStatus = "active"
	WatchFailed  WatchStatus = "failed"
	WatchStopped WatchStatus = "stopped"
)

type WebhookWatch struct {
	ID                      uuid.UUID
	CalendarID              string
	ChannelID               string
	ResourceID              string
	ExpectedResourceURI     string
	VerificationTokenDigest string
	ExpiresAt               *time.Time
	Status                  WatchStatus
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type WebhookNotification struct {
	ID            uuid.UUID
	ChannelID     string
	MessageNumber string
	ResourceID    string
	ResourceState string
	ReceivedAt    time.Time
}

type SyncCheckpoint struct {
	Source    Source
	Scope     string
	Cursor    string
	Version   int64
	UpdatedAt time.Time
}

type IssueStatus string

const (
	IssueOpen     IssueStatus = "open"
	IssueResolved IssueStatus = "resolved"
)

type SyncIssue struct {
	ID             uuid.UUID
	MappingID      *uuid.UUID
	Source         *Source
	SourceIdentity string
	Type           string
	Fingerprint    string
	Diagnostic     string
	Status         IssueStatus
	FirstSeenAt    time.Time
	LastSeenAt     time.Time
	ResolvedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Lease struct {
	Key        string
	OwnerToken string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

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

type Tx interface {
	Mappings() MappingRepository
	Operations() OperationRepository
	Jobs() JobRepository
	Watches() WatchRepository
	Checkpoints() CheckpointRepository
	Issues() IssueRepository
	Leases() LeaseRepository
}

type MappingRepository interface {
	Create(context.Context, EventMapping) (EventMapping, error)
	Get(context.Context, uuid.UUID) (EventMapping, error)
	GetByGoogle(context.Context, GoogleRef) (EventMapping, error)
	GetByLive25(context.Context, Live25Ref) (EventMapping, error)
	Update(context.Context, EventMapping) (EventMapping, error)
	ListInWindow(context.Context, time.Time, time.Time) ([]EventMapping, error)
}

type OperationRepository interface {
	Create(context.Context, SyncOperation) (SyncOperation, error)
	Get(context.Context, uuid.UUID) (SyncOperation, error)
	GetByIdempotencyKey(context.Context, string) (SyncOperation, error)
	Update(context.Context, SyncOperation) (SyncOperation, error)
}

type JobRepository interface {
	Create(context.Context, SyncJob) (SyncJob, error)
	Get(context.Context, uuid.UUID) (SyncJob, error)
	ClaimNext(context.Context, string, time.Duration) (SyncJob, error)
	Update(context.Context, SyncJob) (SyncJob, error)
	AddItem(context.Context, SyncJobItem) (SyncJobItem, error)
	CreateNotificationJob(context.Context, SyncJob) (SyncJob, error)
}

type WatchRepository interface {
	Create(context.Context, WebhookWatch) (WebhookWatch, error)
	GetByChannel(context.Context, string) (WebhookWatch, error)
	Activate(context.Context, WebhookWatch) (WebhookWatch, error)
	MarkStatus(context.Context, uuid.UUID, WatchStatus) error
	RecordNotification(context.Context, WebhookNotification, SyncJob) (bool, SyncJob, error)
}

type CheckpointRepository interface {
	Get(context.Context, Source, string) (SyncCheckpoint, error)
	Create(context.Context, SyncCheckpoint) (SyncCheckpoint, error)
	Update(context.Context, SyncCheckpoint) (SyncCheckpoint, error)
}

type IssueRepository interface {
	UpsertOpen(context.Context, SyncIssue) (SyncIssue, error)
	Resolve(context.Context, uuid.UUID) error
}

type LeaseRepository interface {
	Acquire(context.Context, string, string, time.Duration) (Lease, error)
	Confirm(context.Context, string, string) error
	Release(context.Context, string, string) error
}
