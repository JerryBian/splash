package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"uuid"
)

// SkipStore is the storage contract for excused days.
type SkipStore interface {
	Create(ctx context.Context, projectID uuid.UUID, in SkipInput) (Skip, error)
	Delete(ctx context.Context, projectID uuid.UUID, date string) error
	List(ctx context.Context, projectID uuid.UUID, from, to string) ([]Skip, error)
	// Map returns every skip as date → reason, the input to BuildStats.
	Map(ctx context.Context, projectID uuid.UUID) (map[string]string, error)
}

// Skips is the SQLite-backed SkipStore.
type Skips struct{ db *DB }

var _ SkipStore = (*Skips)(nil)

// Create excuses a day. A second skip for the same day reports ErrConflict;
// clients toggle by deleting first.
func (s *Skips) Create(ctx context.Context, projectID uuid.UUID, in SkipInput) (Skip, error) {
	p, err := s.db.Projects().Get(ctx, projectID)
	if err != nil {
		return Skip{}, err
	}
	if err := in.Validate(p.StartDate, s.db.Today()); err != nil {
		return Skip{}, err
	}
	n := in.Normalized()
	sk := Skip{
		ID:        uuid.NewV7(),
		ProjectID: projectID,
		Date:      n.Date,
		Reason:    n.Reason,
		CreatedAt: s.db.now().UTC().Truncate(time.Millisecond),
	}
	_, err = s.db.sql.ExecContext(ctx,
		`INSERT INTO skips (id, project_id, skip_date, reason, created_at) VALUES (?, ?, ?, ?, ?)`,
		sk.ID.String(), sk.ProjectID.String(), sk.Date, sk.Reason, toMillis(sk.CreatedAt))
	if err != nil {
		if isUniqueErr(err) {
			return Skip{}, fmt.Errorf("%w: %s is already skipped", ErrConflict, sk.Date)
		}
		return Skip{}, fmt.Errorf("create skip: %w", err)
	}
	return sk, nil
}

// Delete un-excuses a day. Deleting a day that was never skipped reports
// ErrNotFound so a retrying client can tell the difference.
func (s *Skips) Delete(ctx context.Context, projectID uuid.UUID, date string) error {
	res, err := s.db.sql.ExecContext(ctx,
		`DELETE FROM skips WHERE project_id = ? AND skip_date = ?`, projectID.String(), date)
	if err != nil {
		return fmt.Errorf("delete skip: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: skip %s/%s", ErrNotFound, projectID, date)
	}
	return nil
}

// List returns skips in [from, to], oldest first. Empty bounds mean no bound.
func (s *Skips) List(ctx context.Context, projectID uuid.UUID, from, to string) ([]Skip, error) {
	where := []string{"project_id = ?"}
	args := []any{projectID.String()}
	if from != "" {
		where = append(where, "skip_date >= ?")
		args = append(args, from)
	}
	if to != "" {
		where = append(where, "skip_date <= ?")
		args = append(args, to)
	}
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT id, project_id, skip_date, reason, created_at FROM skips WHERE `+strings.Join(where, " AND ")+
			` ORDER BY skip_date`, args...)
	if err != nil {
		return nil, fmt.Errorf("list skips: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Skip{}
	for rows.Next() {
		var sk Skip
		var id, pid string
		var createdAt int64
		if err := rows.Scan(&id, &pid, &sk.Date, &sk.Reason, &createdAt); err != nil {
			return nil, fmt.Errorf("list skips: %w", err)
		}
		sk.ID, _ = uuid.Parse(id)
		sk.ProjectID, _ = uuid.Parse(pid)
		sk.CreatedAt = fromMillis(createdAt)
		out = append(out, sk)
	}
	return out, rows.Err()
}

// Map returns every skip for the project as date → reason.
func (s *Skips) Map(ctx context.Context, projectID uuid.UUID) (map[string]string, error) {
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT skip_date, reason FROM skips WHERE project_id = ?`, projectID.String())
	if err != nil {
		return nil, fmt.Errorf("map skips: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var date, reason string
		if err := rows.Scan(&date, &reason); err != nil {
			return nil, fmt.Errorf("map skips: %w", err)
		}
		out[date] = reason
	}
	return out, rows.Err()
}
