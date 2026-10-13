package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"loriss-prototype/internal/store"
)

const jobColumns = `id, kind, source, scope, trigger, window_start, window_end,
 cursor, status, owner_token, lease_expires_at, run_started_at, finished_at,
 error_code, version, created_at, updated_at`

type jobs struct{ q queryer }

func (r jobs) Create(ctx context.Context, job store.SyncJob) (store.SyncJob, error) {
	if job.ID == uuid.Nil {
		return store.SyncJob{}, errors.New("store: job ID is required")
	}
	q := `INSERT INTO sync_jobs (` + jobColumns + `) VALUES
 ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,now(),now()) RETURNING ` + jobColumns
	return scanJob(r.q.QueryRow(ctx, q, job.ID, job.Kind, job.Source, job.Scope, job.Trigger,
		job.WindowStart, job.WindowEnd, job.Cursor, job.Status, job.OwnerToken,
		job.LeaseExpiresAt, job.RunStartedAt, job.FinishedAt, job.ErrorCode))
}
func (r jobs) Get(ctx context.Context, id uuid.UUID) (store.SyncJob, error) {
	return scanJob(r.q.QueryRow(ctx, `SELECT `+jobColumns+` FROM sync_jobs WHERE id=$1`, id))
}
func (r jobs) ClaimNext(ctx context.Context, owner string, ttl time.Duration) (store.SyncJob, error) {
	q := `WITH candidate AS (
 SELECT id FROM sync_jobs
 WHERE (status IN ('pending','retryable') OR (status='running' AND lease_expires_at < now()))
 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1
 ) UPDATE sync_jobs j SET status='running', owner_token=$1,
 lease_expires_at=now()+$2::interval, run_started_at=COALESCE(j.run_started_at, now()),
 version=j.version+1, updated_at=now() FROM candidate WHERE j.id=candidate.id RETURNING ` + jobColumns
	return scanJob(r.q.QueryRow(ctx, q, owner, ttl.String()))
}
func (r jobs) Update(ctx context.Context, job store.SyncJob) (store.SyncJob, error) {
	if job.ID == uuid.Nil || job.Version < 1 {
		return store.SyncJob{}, errors.New("store: job ID and version are required")
	}
	q := `UPDATE sync_jobs SET kind=$2, source=$3, scope=$4, trigger=$5,
 window_start=$6, window_end=$7, cursor=$8, status=$9, owner_token=$10,
 lease_expires_at=$11, run_started_at=$12, finished_at=$13, error_code=$14,
 version=version+1, updated_at=now() WHERE id=$1 AND version=$15 RETURNING ` + jobColumns
	result, err := scanJob(r.q.QueryRow(ctx, q, job.ID, job.Kind, job.Source, job.Scope, job.Trigger,
		job.WindowStart, job.WindowEnd, job.Cursor, job.Status, job.OwnerToken,
		job.LeaseExpiresAt, job.RunStartedAt, job.FinishedAt, job.ErrorCode, job.Version))
	if errors.Is(err, store.ErrNotFound) {
		return store.SyncJob{}, store.ErrVersionConflict
	}
	return result, err
}
func (r jobs) AddItem(ctx context.Context, item store.SyncJobItem) (store.SyncJobItem, error) {
	if item.ID == uuid.Nil {
		return store.SyncJobItem{}, errors.New("store: job item ID is required")
	}
	q := `INSERT INTO sync_job_items (id,job_id,mapping_id,source,source_identity,outcome,error_code,created_at,updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$7,now(),now())
 RETURNING id,job_id,mapping_id,source,source_identity,outcome,error_code,created_at,updated_at`
	var result store.SyncJobItem
	err := r.q.QueryRow(ctx, q, item.ID, item.JobID, item.MappingID, item.Source, item.SourceIdentity,
		item.Outcome, item.ErrorCode).Scan(&result.ID, &result.JobID, &result.MappingID, &result.Source,
		&result.SourceIdentity, &result.Outcome, &result.ErrorCode, &result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return store.SyncJobItem{}, mapError(err)
	}
	return result, nil
}

// CreateNotificationJob is a regular job insertion hook. Notification receipt
// uses watches.RecordNotification so its deduplication and job creation share a transaction.
func (r jobs) CreateNotificationJob(ctx context.Context, job store.SyncJob) (store.SyncJob, error) {
	return r.Create(ctx, job)
}

func scanJob(s row) (store.SyncJob, error) {
	var job store.SyncJob
	err := s.Scan(&job.ID, &job.Kind, &job.Source, &job.Scope, &job.Trigger, &job.WindowStart,
		&job.WindowEnd, &job.Cursor, &job.Status, &job.OwnerToken, &job.LeaseExpiresAt,
		&job.RunStartedAt, &job.FinishedAt, &job.ErrorCode, &job.Version, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return store.SyncJob{}, mapError(err)
	}
	return job, nil
}
