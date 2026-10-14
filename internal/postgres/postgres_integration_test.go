package postgres_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"loriss-prototype/internal/postgres"
	"loriss-prototype/internal/store"
)

// integrationDB is isolated in a generated schema, so a TEST_DATABASE_URL may
// point at a shared development database without these tests touching its data.
type integrationDB struct {
	store *postgres.Store
	pool  *pgxpool.Pool
}

func newIntegrationDB(t *testing.T) integrationDB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	schema := "loriss_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		admin.Close()
		t.Fatalf("parse test database URL: %v", err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
		t.Fatalf("create schema-scoped pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate empty schema: %v", err)
	}
	return integrationDB{store: postgres.New(pool), pool: pool}
}

func TestMigrateIsForwardOnlyAndRepeatable(t *testing.T) {
	db := newIntegrationDB(t)
	ctx := context.Background()
	var count int
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("applied migrations = %d, want 6", count)
	}
	if err := postgres.Migrate(ctx, db.pool); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := db.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("repeated migration changed ledger to %d rows, want 6", count)
	}
}

func TestMappingsEnforceIdentityAndVersion(t *testing.T) {
	db := newIntegrationDB(t)
	ctx := context.Background()
	zone := "America/New_York"
	start := time.Date(2026, time.March, 8, 13, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	mapping := store.EventMapping{
		ID:            uuid.New(),
		Google:        &store.GoogleRef{CalendarID: "cs@example.edu", EventID: "google-1"},
		Origin:        store.SourceGoogle,
		State:         store.MappingActive,
		EventTimeZone: &zone,
		WindowStart:   &start,
		WindowEnd:     &end,
	}
	created, err := db.store.Mappings().Create(ctx, mapping)
	if err != nil {
		t.Fatalf("create mapping: %v", err)
	}
	if created.Version != 1 {
		t.Fatalf("created version = %d, want 1", created.Version)
	}

	duplicate := mapping
	duplicate.ID = uuid.New()
	if _, err := db.store.Mappings().Create(ctx, duplicate); !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("duplicate Google identity error = %v, want ErrAlreadyExists", err)
	}

	created.GoogleRevision = "etag-2"
	updated, err := db.store.Mappings().Update(ctx, created)
	if err != nil {
		t.Fatalf("update mapping: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("updated version = %d, want 2", updated.Version)
	}
	if _, err := db.store.Mappings().Update(ctx, created); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale mapping update error = %v, want ErrVersionConflict", err)
	}
}

func TestMappingsRejectInvalidAllDayRanges(t *testing.T) {
	db := newIntegrationDB(t)
	ctx := context.Background()
	zone := "America/New_York"
	date := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	_, err := db.store.Mappings().Create(ctx, store.EventMapping{
		ID:              uuid.New(),
		Live25:          &store.Live25Ref{EventID: "live25-1"},
		Origin:          store.SourceLive25,
		State:           store.MappingActive,
		EventTimeZone:   &zone,
		AllDay:          true,
		AllDayStartDate: &date,
		AllDayEndDate:   &date, // Exclusive end must be later than start.
		WindowStart:     &start,
		WindowEnd:       &end,
	})
	if err == nil {
		t.Fatal("invalid all-day date range was accepted")
	}
}

func TestOperationsAreIdempotentAndCheckpointsAreVersioned(t *testing.T) {
	db := newIntegrationDB(t)
	ctx := context.Background()
	op := store.SyncOperation{
		ID:             uuid.New(),
		Source:         store.SourceGoogle,
		SourceIdentity: "google-event-1",
		Destination:    store.SourceLive25,
		Kind:           store.OperationCreate,
		IdempotencyKey: "google-event-1:live25:create:etag-1",
		Status:         store.OperationPending,
	}
	first, err := db.store.Operations().Create(ctx, op)
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	op.ID = uuid.New()
	second, err := db.store.Operations().Create(ctx, op)
	if err != nil {
		t.Fatalf("replay operation: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("replayed operation ID = %s, want %s", second.ID, first.ID)
	}

	checkpoint, err := db.store.Checkpoints().Create(ctx, store.SyncCheckpoint{
		Source: store.SourceGoogle,
		Scope:  "cs@example.edu",
		Cursor: "token-1",
	})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}
	checkpoint.Cursor = "token-2"
	updated, err := db.store.Checkpoints().Update(ctx, checkpoint)
	if err != nil {
		t.Fatalf("update checkpoint: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("checkpoint version = %d, want 2", updated.Version)
	}
	if _, err := db.store.Checkpoints().Update(ctx, checkpoint); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale checkpoint update error = %v, want ErrVersionConflict", err)
	}
}

func TestWebhookReplayAndLeaseReacquisition(t *testing.T) {
	db := newIntegrationDB(t)
	ctx := context.Background()
	watch, err := db.store.Watches().Create(ctx, store.WebhookWatch{
		ID:                      uuid.New(),
		CalendarID:              "cs@example.edu",
		ChannelID:               "channel-1",
		ExpectedResourceURI:     "https://www.googleapis.com/calendar/v3/calendars/cs@example.edu/events",
		VerificationTokenDigest: "digest",
		Status:                  store.WatchPending,
	})
	if err != nil {
		t.Fatalf("create watch: %v", err)
	}

	notification := store.WebhookNotification{
		ID:            uuid.New(),
		ChannelID:     watch.ChannelID,
		MessageNumber: "1",
		ResourceState: "sync",
	}
	inserted, job, err := db.store.Watches().RecordNotification(ctx, notification, store.SyncJob{
		ID:    uuid.New(),
		Scope: watch.CalendarID,
	})
	if err != nil || !inserted {
		t.Fatalf("record notification: inserted=%v err=%v", inserted, err)
	}
	if job.Kind != store.JobGoogleNotification || job.Status != store.JobPending {
		t.Fatalf("notification job = %+v, want pending google notification job", job)
	}
	notification.ID = uuid.New()
	inserted, _, err = db.store.Watches().RecordNotification(ctx, notification, store.SyncJob{ID: uuid.New(), Scope: watch.CalendarID})
	if err != nil || inserted {
		t.Fatalf("replay notification: inserted=%v err=%v, want false and nil", inserted, err)
	}

	if _, err := db.store.Leases().Acquire(ctx, "google:google-event-1", "worker-a", time.Hour); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	if _, err := db.store.Leases().Acquire(ctx, "google:google-event-1", "worker-b", time.Hour); !errors.Is(err, store.ErrLeaseHeld) {
		t.Fatalf("second worker acquire error = %v, want ErrLeaseHeld", err)
	}
	if _, err := db.store.Leases().Acquire(ctx, "live25:expired-event", "worker-a", -time.Second); err != nil {
		t.Fatalf("acquire expired lease: %v", err)
	}
	if lease, err := db.store.Leases().Acquire(ctx, "live25:expired-event", "worker-b", time.Hour); err != nil || lease.OwnerToken != "worker-b" {
		t.Fatalf("reacquire expired lease = %+v, %v; want worker-b and nil", lease, err)
	}
}
