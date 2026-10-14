package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"loriss-prototype/internal/store"
)

const operationColumns = `id, mapping_id, source, source_identity, destination, kind,
 idempotency_key, source_revision, source_fingerprint, status, attempt_count,
 next_attempt_at, lease_key, error_code, version, created_at, updated_at, completed_at`

type operations struct{ q queryer }

func (r operations) Create(ctx context.Context, op store.SyncOperation) (store.SyncOperation, error) {
	if op.ID == uuid.Nil {
		return store.SyncOperation{}, errors.New("store: operation ID is required")
	}
	q := `INSERT INTO sync_operations (` + operationColumns + `) VALUES
 ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,1,now(),now(),$15)
 RETURNING ` + operationColumns
	created, err := scanOperation(r.q.QueryRow(ctx, q, op.ID, op.MappingID, op.Source, op.SourceIdentity,
		op.Destination, op.Kind, op.IdempotencyKey, op.SourceRevision, op.SourceFingerprint,
		op.Status, op.AttemptCount, op.NextAttemptAt, op.LeaseKey, op.ErrorCode, op.CompletedAt))
	if errors.Is(err, store.ErrAlreadyExists) {
		// Replaying the exact plan returns its original durable operation rather
		// than creating another write attempt.
		existing, getErr := r.GetByIdempotencyKey(ctx, op.IdempotencyKey)
		if getErr == nil {
			return existing, nil
		}
	}
	return created, err
}

func (r operations) Get(ctx context.Context, id uuid.UUID) (store.SyncOperation, error) {
	return scanOperation(r.q.QueryRow(ctx, `SELECT `+operationColumns+` FROM sync_operations WHERE id=$1`, id))
}
func (r operations) GetByIdempotencyKey(ctx context.Context, key string) (store.SyncOperation, error) {
	return scanOperation(r.q.QueryRow(ctx, `SELECT `+operationColumns+` FROM sync_operations WHERE idempotency_key=$1`, key))
}
func (r operations) Update(ctx context.Context, op store.SyncOperation) (store.SyncOperation, error) {
	if op.ID == uuid.Nil || op.Version < 1 {
		return store.SyncOperation{}, errors.New("store: operation ID and version are required")
	}
	q := `UPDATE sync_operations SET mapping_id=$2, source=$3, source_identity=$4,
 destination=$5, kind=$6, source_revision=$7, source_fingerprint=$8, status=$9,
 attempt_count=$10, next_attempt_at=$11, lease_key=$12, error_code=$13,
 completed_at=$14, version=version+1, updated_at=now() WHERE id=$1 AND version=$15 RETURNING ` + operationColumns
	result, err := scanOperation(r.q.QueryRow(ctx, q, op.ID, op.MappingID, op.Source, op.SourceIdentity,
		op.Destination, op.Kind, op.SourceRevision, op.SourceFingerprint, op.Status,
		op.AttemptCount, op.NextAttemptAt, op.LeaseKey, op.ErrorCode, op.CompletedAt, op.Version))
	if errors.Is(err, store.ErrNotFound) {
		return store.SyncOperation{}, store.ErrVersionConflict
	}
	return result, err
}

func scanOperation(s row) (store.SyncOperation, error) {
	var op store.SyncOperation
	err := s.Scan(&op.ID, &op.MappingID, &op.Source, &op.SourceIdentity, &op.Destination,
		&op.Kind, &op.IdempotencyKey, &op.SourceRevision, &op.SourceFingerprint, &op.Status,
		&op.AttemptCount, &op.NextAttemptAt, &op.LeaseKey, &op.ErrorCode, &op.Version, &op.CreatedAt,
		&op.UpdatedAt, &op.CompletedAt)
	if err != nil {
		return store.SyncOperation{}, mapError(err)
	}
	return op, nil
}
