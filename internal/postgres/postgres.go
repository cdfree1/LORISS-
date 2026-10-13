// Package postgres implements store using PostgreSQL and pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"loriss-prototype/internal/store"
)

// Store uses a caller-owned pool. It does not read configuration or close the
// pool, which keeps connection lifetime under cmd/server's control.
type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) InTx(ctx context.Context, fn func(store.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return mapError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(txStore{q: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	return nil
}

func (s *Store) Mappings() store.MappingRepository       { return mappings{q: s.pool} }
func (s *Store) Operations() store.OperationRepository   { return operations{q: s.pool} }
func (s *Store) Jobs() store.JobRepository               { return jobs{q: s.pool} }
func (s *Store) Watches() store.WatchRepository          { return watches{q: s.pool} }
func (s *Store) Checkpoints() store.CheckpointRepository { return checkpoints{q: s.pool} }
func (s *Store) Issues() store.IssueRepository           { return issues{q: s.pool} }
func (s *Store) Leases() store.LeaseRepository           { return leases{q: s.pool} }

type txStore struct{ q queryer }

func (s txStore) Mappings() store.MappingRepository       { return mappings{q: s.q} }
func (s txStore) Operations() store.OperationRepository   { return operations{q: s.q} }
func (s txStore) Jobs() store.JobRepository               { return jobs{q: s.q} }
func (s txStore) Watches() store.WatchRepository          { return watches{q: s.q} }
func (s txStore) Checkpoints() store.CheckpointRepository { return checkpoints{q: s.q} }
func (s txStore) Issues() store.IssueRepository           { return issues{q: s.q} }
func (s txStore) Leases() store.LeaseRepository           { return leases{q: s.q} }

type row interface{ Scan(...any) error }
type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", store.ErrAlreadyExists, pgErr.ConstraintName)
		case "40001", "40P01":
			return fmt.Errorf("store: transaction retry required: %w", err)
		}
	}
	return err
}
