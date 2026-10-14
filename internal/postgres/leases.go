package postgres

import (
	"context"
	"time"

	"loriss-prototype/internal/store"
)

type leases struct{ q queryer }

// Acquire atomically creates a lease, extends a lease already owned by this
// worker, or reclaims an expired one. Live leases owned by others are never
// overwritten.
func (r leases) Acquire(ctx context.Context, key, owner string, ttl time.Duration) (store.Lease, error) {
	q := `INSERT INTO event_leases (lease_key,owner_token,expires_at,created_at,updated_at)
 VALUES ($1,$2,now()+$3::interval,now(),now())
 ON CONFLICT (lease_key) DO UPDATE SET owner_token=EXCLUDED.owner_token,
 expires_at=EXCLUDED.expires_at,updated_at=now()
 WHERE event_leases.expires_at <= now() OR event_leases.owner_token=EXCLUDED.owner_token
 RETURNING lease_key,owner_token,expires_at,created_at,updated_at`
	lease, err := scanLease(r.q.QueryRow(ctx, q, key, owner, ttl.String()))
	if err == store.ErrNotFound {
		return store.Lease{}, store.ErrLeaseHeld
	}
	return lease, err
}
func (r leases) Confirm(ctx context.Context, key, owner string) error {
	var got string
	err := r.q.QueryRow(ctx, `SELECT owner_token FROM event_leases WHERE lease_key=$1 AND owner_token=$2 AND expires_at > now()`, key, owner).Scan(&got)
	if err == nil {
		return nil
	}
	if err == store.ErrNotFound {
		return store.ErrLeaseHeld
	}
	return mapError(err)
}
func (r leases) Release(ctx context.Context, key, owner string) error {
	tag, err := r.q.Exec(ctx, `DELETE FROM event_leases WHERE lease_key=$1 AND owner_token=$2`, key, owner)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrLeaseHeld
	}
	return nil
}
func scanLease(s row) (store.Lease, error) {
	var lease store.Lease
	if err := s.Scan(&lease.Key, &lease.OwnerToken, &lease.ExpiresAt, &lease.CreatedAt, &lease.UpdatedAt); err != nil {
		return store.Lease{}, mapError(err)
	}
	return lease, nil
}
