package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"loriss-prototype/internal/store"
)

const issueColumns = `id,mapping_id,source,source_identity,type,fingerprint,diagnostic,status,
 first_seen_at,last_seen_at,resolved_at,created_at,updated_at`

type issues struct{ q queryer }

func (r issues) UpsertOpen(ctx context.Context, issue store.SyncIssue) (store.SyncIssue, error) {
	if issue.ID == uuid.Nil {
		return store.SyncIssue{}, errors.New("store: issue ID is required")
	}
	if issue.MappingID != nil {
		q := `INSERT INTO sync_issues (` + issueColumns + `) VALUES
 ($1,$2,$3,$4,$5,$6,$7,'open',now(),now(),NULL,now(),now())
 ON CONFLICT (mapping_id,type,fingerprint) WHERE status='open' AND mapping_id IS NOT NULL
 DO UPDATE SET diagnostic=EXCLUDED.diagnostic,last_seen_at=now(),updated_at=now()
 RETURNING ` + issueColumns
		return scanIssue(r.q.QueryRow(ctx, q, issue.ID, issue.MappingID, issue.Source, issue.SourceIdentity,
			issue.Type, issue.Fingerprint, issue.Diagnostic))
	}
	q := `INSERT INTO sync_issues (` + issueColumns + `) VALUES
 ($1,NULL,$2,$3,$4,$5,$6,'open',now(),now(),NULL,now(),now())
 ON CONFLICT (source,source_identity,type,fingerprint) WHERE status='open' AND mapping_id IS NULL
 DO UPDATE SET diagnostic=EXCLUDED.diagnostic,last_seen_at=now(),updated_at=now()
 RETURNING ` + issueColumns
	return scanIssue(r.q.QueryRow(ctx, q, issue.ID, issue.Source, issue.SourceIdentity, issue.Type,
		issue.Fingerprint, issue.Diagnostic))
}
func (r issues) Resolve(ctx context.Context, id uuid.UUID) error {
	tag, err := r.q.Exec(ctx, `UPDATE sync_issues SET status='resolved',resolved_at=now(),updated_at=now()
 WHERE id=$1 AND status='open'`, id)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}
func scanIssue(s row) (store.SyncIssue, error) {
	var issue store.SyncIssue
	err := s.Scan(&issue.ID, &issue.MappingID, &issue.Source, &issue.SourceIdentity, &issue.Type,
		&issue.Fingerprint, &issue.Diagnostic, &issue.Status, &issue.FirstSeenAt, &issue.LastSeenAt,
		&issue.ResolvedAt, &issue.CreatedAt, &issue.UpdatedAt)
	if err != nil {
		return store.SyncIssue{}, mapError(err)
	}
	return issue, nil
}
