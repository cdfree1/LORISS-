package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"loriss-prototype/internal/store"
)

const watchColumns = `id, calendar_id, channel_id, resource_id, expected_resource_uri,
 verification_token_digest, expires_at, status, created_at, updated_at`

type watches struct{ q queryer }

func (r watches) Create(ctx context.Context, watch store.WebhookWatch) (store.WebhookWatch, error) {
	if watch.ID == uuid.Nil {
		return store.WebhookWatch{}, errors.New("store: watch ID is required")
	}
	q := `INSERT INTO webhook_watches (` + watchColumns + `) VALUES
 ($1,$2,$3,$4,$5,$6,$7,$8,now(),now()) RETURNING ` + watchColumns
	return scanWatch(r.q.QueryRow(ctx, q, watch.ID, watch.CalendarID, watch.ChannelID, watch.ResourceID,
		watch.ExpectedResourceURI, watch.VerificationTokenDigest, watch.ExpiresAt, watch.Status))
}
func (r watches) GetByChannel(ctx context.Context, channel string) (store.WebhookWatch, error) {
	return scanWatch(r.q.QueryRow(ctx, `SELECT `+watchColumns+` FROM webhook_watches WHERE channel_id=$1`, channel))
}
func (r watches) Activate(ctx context.Context, watch store.WebhookWatch) (store.WebhookWatch, error) {
	q := `UPDATE webhook_watches SET resource_id=$2, expires_at=$3, status='active', updated_at=now()
 WHERE id=$1 AND status='pending' RETURNING ` + watchColumns
	return scanWatch(r.q.QueryRow(ctx, q, watch.ID, watch.ResourceID, watch.ExpiresAt))
}
func (r watches) MarkStatus(ctx context.Context, id uuid.UUID, status store.WatchStatus) error {
	tag, err := r.q.Exec(ctx, `UPDATE webhook_watches SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (r watches) RecordNotification(ctx context.Context, n store.WebhookNotification, job store.SyncJob) (bool, store.SyncJob, error) {
	if pool, ok := r.q.(*pgxpool.Pool); ok {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return false, store.SyncJob{}, mapError(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		inserted, result, err := watches{q: tx}.recordNotification(ctx, n, job)
		if err != nil {
			return false, store.SyncJob{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return false, store.SyncJob{}, mapError(err)
		}
		return inserted, result, nil
	}
	return r.recordNotification(ctx, n, job)
}

func (r watches) recordNotification(ctx context.Context, n store.WebhookNotification, job store.SyncJob) (bool, store.SyncJob, error) {
	if n.ID == uuid.Nil || job.ID == uuid.Nil {
		return false, store.SyncJob{}, errors.New("store: notification and job IDs are required")
	}
	// ON CONFLICT avoids placing a transaction into an aborted state for a replay.
	tag, err := r.q.Exec(ctx, `INSERT INTO webhook_notifications
 (id, channel_id, message_number, resource_id, resource_state, received_at)
 VALUES ($1,$2,$3,$4,$5,COALESCE($6,now())) ON CONFLICT (channel_id,message_number) DO NOTHING`,
		n.ID, n.ChannelID, n.MessageNumber, n.ResourceID, n.ResourceState, nullableTime(n.ReceivedAt))
	if err != nil {
		return false, store.SyncJob{}, mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return false, store.SyncJob{}, nil
	}
	// Notification reconciliation is always a pending Google job, regardless
	// of caller defaults. This also ensures the partial coalescing index applies.
	source := store.SourceGoogle
	job.Source = &source
	job.Kind = store.JobGoogleNotification
	job.Status = store.JobPending
	q := `INSERT INTO sync_jobs (` + jobColumns + `) VALUES
 ($1,$2,$3,$4,$5,$6,$7,$8,$9,'',NULL,NULL,NULL,'',1,now(),now())
 ON CONFLICT (kind,source,scope) WHERE kind='google_notification' AND status IN ('pending','running')
 DO UPDATE SET updated_at=now()
 RETURNING ` + jobColumns
	created, err := scanJob(r.q.QueryRow(ctx, q, job.ID, job.Kind, job.Source, job.Scope, job.Trigger,
		job.WindowStart, job.WindowEnd, job.Cursor, job.Status))
	if err != nil {
		return false, store.SyncJob{}, err
	}
	return true, created, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func scanWatch(s row) (store.WebhookWatch, error) {
	var w store.WebhookWatch
	err := s.Scan(&w.ID, &w.CalendarID, &w.ChannelID, &w.ResourceID, &w.ExpectedResourceURI,
		&w.VerificationTokenDigest, &w.ExpiresAt, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return store.WebhookWatch{}, mapError(err)
	}
	return w, nil
}
