package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"uuid"
)

// CheckinStore is the storage contract for check-ins.
type CheckinStore interface {
	Create(ctx context.Context, projectID uuid.UUID, in CheckinInput) (Checkin, error)
	Get(ctx context.Context, id uuid.UUID) (Checkin, error)
	List(ctx context.Context, q CheckinQuery) ([]Checkin, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// Aggregates rolls check-ins up by calendar day, the input to BuildStats.
	Aggregates(ctx context.Context, projectID uuid.UUID, from, to string) (map[string]DayAggregate, error)
	// AllAsc returns the full history in instant order, for interval_hours.
	AllAsc(ctx context.Context, projectID uuid.UUID) ([]Checkin, error)
}

// Checkins is the SQLite-backed CheckinStore.
type Checkins struct{ db *DB }

var _ CheckinStore = (*Checkins)(nil)

const checkinColumns = `id, project_id, value, note, mood, local_date, checked_at, tz_offset, backfilled, created_at`

// Create records a check-in. The project must exist; the date defaults to the
// client's today (now + reported offset) and may be in the past (a backfill),
// which is flagged so the UI can mark it. A backfilled check-in is timestamped
// at noon UTC of its credited date so history ordering stays sensible.
func (s *Checkins) Create(ctx context.Context, projectID uuid.UUID, in CheckinInput) (Checkin, error) {
	proj, err := s.db.Projects().Get(ctx, projectID)
	if err != nil {
		return Checkin{}, err
	}
	today := s.db.Today()
	if err := in.Validate(today); err != nil {
		return Checkin{}, err
	}
	n := in.Normalized()

	now := s.db.now().UTC().Truncate(time.Millisecond)
	clientToday := DateKey(now.Add(time.Duration(n.TZOffset) * time.Minute))
	date := n.Date
	if date == "" {
		date = clientToday
	}
	backfilled := date != clientToday

	value := n.Value
	if proj.Mode == ModeCheck {
		value = 1 // one tick completes the period; quantity is meaningless
	}
	checkedAt := now
	if backfilled {
		if t, err := ParseDate(date); err == nil {
			checkedAt = t.Add(12 * time.Hour)
		}
	}

	c := Checkin{
		ID:         uuid.NewV7(),
		ProjectID:  projectID,
		Value:      value,
		Note:       n.Note,
		Mood:       n.Mood,
		LocalDate:  date,
		CheckedAt:  checkedAt,
		TZOffset:   n.TZOffset,
		Backfilled: backfilled,
		CreatedAt:  now,
	}
	_, err = s.db.sql.ExecContext(ctx, `INSERT INTO checkins (`+checkinColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID.String(), c.ProjectID.String(), c.Value, c.Note, c.Mood, c.LocalDate,
		toMillis(c.CheckedAt), c.TZOffset, boolInt(c.Backfilled), toMillis(c.CreatedAt))
	if err != nil {
		return Checkin{}, fmt.Errorf("create checkin: %w", err)
	}
	// Carry the derived prerequisite verdict on the create response too, so
	// the UI can flag a violation immediately.
	one := []Checkin{c}
	if err := s.db.enrichCheckins(ctx, one); err != nil {
		return Checkin{}, err
	}
	return one[0], nil
}

// Get returns one check-in, or ErrNotFound.
func (s *Checkins) Get(ctx context.Context, id uuid.UUID) (Checkin, error) {
	row := s.db.sql.QueryRowContext(ctx, `SELECT `+checkinColumns+` FROM checkins WHERE id = ?`, id.String())
	c, err := scanCheckin(row)
	if err != nil {
		return Checkin{}, fmt.Errorf("get checkin %s: %w", id, errNoRows(err, "checkin "+id.String()))
	}
	return c, nil
}

// List returns one page of history, newest first.
func (s *Checkins) List(ctx context.Context, q CheckinQuery) ([]Checkin, error) {
	q.Normalize()
	where := []string{"project_id = ?"}
	args := []any{q.ProjectID.String()}
	if q.From != "" {
		where = append(where, "local_date >= ?")
		args = append(args, q.From)
	}
	if q.To != "" {
		where = append(where, "local_date <= ?")
		args = append(args, q.To)
	}
	args = append(args, q.Limit)
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT `+checkinColumns+` FROM checkins WHERE `+strings.Join(where, " AND ")+
			` ORDER BY checked_at DESC, id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("list checkins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Checkin{}
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return nil, fmt.Errorf("list checkins: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.db.enrichCheckins(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete removes one check-in (undo).
func (s *Checkins) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.sql.ExecContext(ctx, `DELETE FROM checkins WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("delete checkin %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: checkin %s", ErrNotFound, id)
	}
	return nil
}

// Aggregates sums value and counts per calendar day in [from, to]. Empty
// bounds mean "no bound". Days with no check-ins are absent from the map.
func (s *Checkins) Aggregates(ctx context.Context, projectID uuid.UUID, from, to string) (map[string]DayAggregate, error) {
	where := []string{"project_id = ?"}
	args := []any{projectID.String()}
	if from != "" {
		where = append(where, "local_date >= ?")
		args = append(args, from)
	}
	if to != "" {
		where = append(where, "local_date <= ?")
		args = append(args, to)
	}
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT local_date, COUNT(*), SUM(value) FROM checkins WHERE `+strings.Join(where, " AND ")+
			` GROUP BY local_date`, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate checkins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]DayAggregate{}
	for rows.Next() {
		var date string
		var agg DayAggregate
		if err := rows.Scan(&date, &agg.Count, &agg.Value); err != nil {
			return nil, fmt.Errorf("aggregate checkins: %w", err)
		}
		out[date] = agg
	}
	return out, rows.Err()
}

// AllAsc returns the full history ordered by instant, for interval streaks.
func (s *Checkins) AllAsc(ctx context.Context, projectID uuid.UUID) ([]Checkin, error) {
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT `+checkinColumns+` FROM checkins WHERE project_id = ? ORDER BY checked_at, id`, projectID.String())
	if err != nil {
		return nil, fmt.Errorf("list checkins asc: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Checkin{}
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			return nil, fmt.Errorf("list checkins asc: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCheckin(row rowScanner) (Checkin, error) {
	var c Checkin
	var id, projectID string
	var checkedAt, createdAt int64
	var backfilled int
	if err := row.Scan(&id, &projectID, &c.Value, &c.Note, &c.Mood, &c.LocalDate,
		&checkedAt, &c.TZOffset, &backfilled, &createdAt); err != nil {
		return Checkin{}, err
	}
	c.ID, _ = uuid.Parse(id)
	c.ProjectID, _ = uuid.Parse(projectID)
	c.CheckedAt = fromMillis(checkedAt)
	c.Backfilled = backfilled != 0
	c.CreatedAt = fromMillis(createdAt)
	return c, nil
}
