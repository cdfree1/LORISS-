package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"loriss-prototype/internal/store"
)

const mappingColumns = `id, google_calendar_id, google_event_id, google_recurrence_key,
 live25_event_id, live25_occurrence_key, origin, state, google_revision,
 live25_revision, google_fingerprint, live25_fingerprint, synced_fingerprint,
 event_time_zone, is_all_day, all_day_start_date, all_day_end_date, window_start,
 window_end, version, tombstoned_at, created_at, updated_at`

type mappings struct{ q queryer }

func mappingArgs(m store.EventMapping) []any {
	var calendarID, googleID, googleKey any
	if m.Google != nil {
		calendarID, googleID, googleKey = m.Google.CalendarID, m.Google.EventID, m.Google.RecurrenceKey
	} else {
		googleKey = ""
	}
	var liveID, liveKey any
	if m.Live25 != nil {
		liveID, liveKey = m.Live25.EventID, m.Live25.OccurrenceKey
	} else {
		liveKey = ""
	}
	return []any{m.ID, calendarID, googleID, googleKey, liveID, liveKey, m.Origin, m.State,
		m.GoogleRevision, m.Live25Revision, m.GoogleFingerprint, m.Live25Fingerprint,
		m.SyncedFingerprint, m.EventTimeZone, m.AllDay, m.AllDayStartDate, m.AllDayEndDate,
		m.WindowStart, m.WindowEnd, m.TombstonedAt}
}

func (r mappings) Create(ctx context.Context, m store.EventMapping) (store.EventMapping, error) {
	if m.ID == uuid.Nil {
		return store.EventMapping{}, errors.New("store: mapping ID is required")
	}
	args := mappingArgs(m)
	q := `INSERT INTO event_mappings (` + mappingColumns + `) VALUES
($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,1,$20,now(),now())
RETURNING ` + mappingColumns
	return scanMapping(r.q.QueryRow(ctx, q, args...))
}

func (r mappings) Get(ctx context.Context, id uuid.UUID) (store.EventMapping, error) {
	return scanMapping(r.q.QueryRow(ctx, `SELECT `+mappingColumns+` FROM event_mappings WHERE id = $1`, id))
}

func (r mappings) GetByGoogle(ctx context.Context, ref store.GoogleRef) (store.EventMapping, error) {
	return scanMapping(r.q.QueryRow(ctx, `SELECT `+mappingColumns+` FROM event_mappings WHERE google_calendar_id=$1 AND google_event_id=$2`, ref.CalendarID, ref.EventID))
}

func (r mappings) GetByLive25(ctx context.Context, ref store.Live25Ref) (store.EventMapping, error) {
	return scanMapping(r.q.QueryRow(ctx, `SELECT `+mappingColumns+` FROM event_mappings WHERE live25_event_id=$1 AND live25_occurrence_key=$2`, ref.EventID, ref.OccurrenceKey))
}

func (r mappings) Update(ctx context.Context, m store.EventMapping) (store.EventMapping, error) {
	if m.ID == uuid.Nil || m.Version < 1 {
		return store.EventMapping{}, errors.New("store: mapping ID and version are required")
	}
	args := mappingArgs(m)
	args = append(args, m.Version)
	q := `UPDATE event_mappings SET
 google_calendar_id=$2, google_event_id=$3, google_recurrence_key=$4,
 live25_event_id=$5, live25_occurrence_key=$6, origin=$7, state=$8,
 google_revision=$9, live25_revision=$10, google_fingerprint=$11,
 live25_fingerprint=$12, synced_fingerprint=$13, event_time_zone=$14,
 is_all_day=$15, all_day_start_date=$16, all_day_end_date=$17,
 window_start=$18, window_end=$19, tombstoned_at=$20,
 version=version+1, updated_at=now()
 WHERE id=$1 AND version=$21 RETURNING ` + mappingColumns
	mapping, err := scanMapping(r.q.QueryRow(ctx, q, args...))
	if errors.Is(err, store.ErrNotFound) {
		return store.EventMapping{}, store.ErrVersionConflict
	}
	return mapping, err
}

func (r mappings) ListInWindow(ctx context.Context, start, end time.Time) ([]store.EventMapping, error) {
	rows, err := r.q.Query(ctx, `SELECT `+mappingColumns+` FROM event_mappings
 WHERE state IN ('provisioning','active','conflicted')
   AND window_start < $2 AND window_end > $1 ORDER BY window_start, id`, start, end)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	result := make([]store.EventMapping, 0)
	for rows.Next() {
		m, err := scanMapping(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, mapError(rows.Err())
}

func scanMapping(s row) (store.EventMapping, error) {
	var m store.EventMapping
	var calendarID, googleID, liveID *string
	var googleKey, liveKey string
	err := s.Scan(&m.ID, &calendarID, &googleID, &googleKey, &liveID, &liveKey,
		&m.Origin, &m.State, &m.GoogleRevision, &m.Live25Revision, &m.GoogleFingerprint,
		&m.Live25Fingerprint, &m.SyncedFingerprint, &m.EventTimeZone, &m.AllDay,
		&m.AllDayStartDate, &m.AllDayEndDate, &m.WindowStart, &m.WindowEnd, &m.Version,
		&m.TombstonedAt, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return store.EventMapping{}, mapError(err)
	}
	if googleID != nil {
		m.Google = &store.GoogleRef{CalendarID: *calendarID, EventID: *googleID, RecurrenceKey: googleKey}
	}
	if liveID != nil {
		m.Live25 = &store.Live25Ref{EventID: *liveID, OccurrenceKey: liveKey}
	}
	return m, nil
}
