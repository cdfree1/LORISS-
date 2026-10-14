package postgres

import (
	"context"
	"errors"

	"loriss-prototype/internal/store"
)

type checkpoints struct{ q queryer }

func (r checkpoints) Get(ctx context.Context, source store.Source, scope string) (store.SyncCheckpoint, error) {
	return scanCheckpoint(r.q.QueryRow(ctx, `SELECT source,scope,cursor,version,updated_at FROM sync_checkpoints WHERE source=$1 AND scope=$2`, source, scope))
}
func (r checkpoints) Create(ctx context.Context, cp store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	return scanCheckpoint(r.q.QueryRow(ctx, `INSERT INTO sync_checkpoints (source,scope,cursor,version,updated_at)
 VALUES ($1,$2,$3,1,now()) RETURNING source,scope,cursor,version,updated_at`, cp.Source, cp.Scope, cp.Cursor))
}
func (r checkpoints) Update(ctx context.Context, cp store.SyncCheckpoint) (store.SyncCheckpoint, error) {
	if cp.Version < 1 {
		return store.SyncCheckpoint{}, errors.New("store: checkpoint version is required")
	}
	result, err := scanCheckpoint(r.q.QueryRow(ctx, `UPDATE sync_checkpoints
 SET cursor=$3,version=version+1,updated_at=now() WHERE source=$1 AND scope=$2 AND version=$4
 RETURNING source,scope,cursor,version,updated_at`, cp.Source, cp.Scope, cp.Cursor, cp.Version))
	if errors.Is(err, store.ErrNotFound) {
		return store.SyncCheckpoint{}, store.ErrVersionConflict
	}
	return result, err
}
func scanCheckpoint(s row) (store.SyncCheckpoint, error) {
	var cp store.SyncCheckpoint
	if err := s.Scan(&cp.Source, &cp.Scope, &cp.Cursor, &cp.Version, &cp.UpdatedAt); err != nil {
		return store.SyncCheckpoint{}, mapError(err)
	}
	return cp, nil
}
