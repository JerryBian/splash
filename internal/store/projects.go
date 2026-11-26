package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"uuid"
)

// ErrVersionConflict reports that an If-Match precondition failed: the project
// changed since the client read it. The API maps it to 412, distinct from the
// 409 that a name collision or a concurrent write produces.
var ErrVersionConflict = errors.New("store: version conflict")

// ProjectStore is the storage contract for projects.
type ProjectStore interface {
	Create(ctx context.Context, in ProjectInput) (Project, error)
	Get(ctx context.Context, id uuid.UUID) (Project, error)
	List(ctx context.Context, q ProjectQuery) ([]Project, error)
	// Update applies a patch. expectedVersion, when non-nil, is folded into
	// the UPDATE's WHERE clause so the database — not a check-then-write gap —
	// enforces optimistic concurrency.
	Update(ctx context.Context, id uuid.UUID, patch ProjectPatch, expectedVersion *int) (Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Count(ctx context.Context) (int, error)
}

// Projects is the SQLite-backed ProjectStore.
type Projects struct{ db *DB }

var _ ProjectStore = (*Projects)(nil)

const projectColumns = `id, name, description, icon, color, category_id, nature, schedule, mode,
	target, unit, weekdays, interval_value, note_prompt, start_date, archived, pinned,
	created_at, updated_at, version`

// Create validates and stores a new project.
func (s *Projects) Create(ctx context.Context, in ProjectInput) (Project, error) {
	today := s.db.Today()
	if err := in.Validate(today); err != nil {
		return Project{}, err
	}
	n := in.Normalized()

	categoryID := uuid.Nil()
	if n.CategoryID != "" {
		id, err := uuid.Parse(n.CategoryID)
		if err != nil {
			return Project{}, ValidationErrors{{Field: "categoryId", Message: "must be a UUID or empty"}}
		}
		if _, err := s.db.Categories().Get(ctx, id); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Project{}, ValidationErrors{{Field: "categoryId", Message: "does not exist"}}
			}
			return Project{}, err
		}
		categoryID = id
	}

	prereqs := parseLinks(n.Prerequisites)
	posts := parseLinks(n.Postrequisites)
	if err := s.checkLinkGraph(ctx, uuid.Nil(), prereqs, posts); err != nil {
		return Project{}, err
	}

	now := s.db.now().UTC().Truncate(1e6)
	p := Project{
		ID:             uuid.NewV7(),
		Name:           n.Name,
		Description:    n.Description,
		Icon:           n.Icon,
		Color:          n.Color,
		CategoryID:     categoryID,
		Nature:         n.Nature,
		Schedule:       n.Schedule,
		Mode:           n.Mode,
		Target:         n.Target,
		Unit:           n.Unit,
		Weekdays:       n.Weekdays,
		Interval:       n.Interval,
		NotePrompt:     n.NotePrompt,
		StartDate:      n.StartDate,
		Pinned:         n.Pinned,
		Prerequisites:  prereqs,
		Postrequisites: posts,
		CreatedAt:      now,
		UpdatedAt:      now,
		Version:        1,
	}
	_, err := s.db.sql.ExecContext(ctx, `INSERT INTO projects (`+projectColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID.String(), p.Name, p.Description, p.Icon, p.Color, categoryArg(p.CategoryID),
		p.Nature, string(p.Schedule), p.Mode, p.Target, p.Unit, weekdaysCSV(p.Weekdays),
		p.Interval, p.NotePrompt, p.StartDate, boolInt(p.Archived), boolInt(p.Pinned),
		toMillis(p.CreatedAt), toMillis(p.UpdatedAt), p.Version)
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	if err := s.setLinks(ctx, p.ID, prereqs, posts); err != nil {
		return Project{}, err
	}
	return p, nil
}

// Get returns one project, or ErrNotFound. Access control is the API layer's
// job: every project is readable by the signed-in administrator.
func (s *Projects) Get(ctx context.Context, id uuid.UUID) (Project, error) {
	if id == uuid.Nil() {
		return Project{}, fmt.Errorf("%w: nil identifier", ErrNotFound)
	}
	row := s.db.sql.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = ?`, id.String())
	p, err := scanProject(row)
	if err != nil {
		return Project{}, fmt.Errorf("get project %s: %w", id, errNoRows(err, "project "+id.String()))
	}
	requires, implies, err := s.linkMaps(ctx, []uuid.UUID{p.ID})
	if err != nil {
		return Project{}, err
	}
	p.Prerequisites = requires[p.ID]
	p.Postrequisites = implies[p.ID]
	return p, nil
}

// List returns matching projects, oldest first (UUIDv7 breaks ties).
func (s *Projects) List(ctx context.Context, q ProjectQuery) ([]Project, error) {
	where := []string{"1=1"}
	args := []any{}
	if !q.IncludeArchived {
		where = append(where, "archived = 0")
	}
	if q.CategoryID != "" {
		where = append(where, "category_id = ?")
		args = append(args, q.CategoryID)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		where = append(where, `(name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')`)
		needle := "%" + likeEscape(search) + "%"
		args = append(args, needle, needle)
	}
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE `+strings.Join(where, " AND ")+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("list projects: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(out))
	for i, p := range out {
		ids[i] = p.ID
	}
	requires, implies, err := s.linkMaps(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Prerequisites = requires[out[i].ID]
		out[i].Postrequisites = implies[out[i].ID]
	}
	return out, nil
}

// Update applies a patch under optimistic concurrency. The version comparison
// lives in the UPDATE's WHERE clause, so two concurrent patches cannot lose one
// another's write even though the read and the write are separate statements.
func (s *Projects) Update(ctx context.Context, id uuid.UUID, patch ProjectPatch, expectedVersion *int) (Project, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Project{}, err
	}
	if expectedVersion != nil && *expectedVersion != current.Version {
		return Project{}, fmt.Errorf("%w: project %s is at v%d, client expected v%d",
			ErrVersionConflict, id, current.Version, *expectedVersion)
	}
	if err := patch.Validate(current, s.db.Today()); err != nil {
		return Project{}, err
	}

	next := current
	patch.ApplyTo(&next)
	if next.CategoryID != uuid.Nil() && next.CategoryID != current.CategoryID {
		if _, err := s.db.Categories().Get(ctx, next.CategoryID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Project{}, ValidationErrors{{Field: "categoryId", Message: "does not exist"}}
			}
			return Project{}, err
		}
	}
	if err := s.checkLinkGraph(ctx, id, next.Prerequisites, next.Postrequisites); err != nil {
		return Project{}, err
	}
	next.UpdatedAt = s.db.now().UTC().Truncate(1e6)
	next.Version = current.Version + 1

	res, err := s.db.sql.ExecContext(ctx, `UPDATE projects SET
		name = ?, description = ?, icon = ?, color = ?, category_id = ?, nature = ?, schedule = ?, mode = ?,
		target = ?, unit = ?, weekdays = ?, interval_value = ?, note_prompt = ?,
		start_date = ?, archived = ?, pinned = ?, updated_at = ?, version = ?
		WHERE id = ? AND version = ?`,
		next.Name, next.Description, next.Icon, next.Color, categoryArg(next.CategoryID),
		next.Nature, string(next.Schedule), next.Mode, next.Target, next.Unit, weekdaysCSV(next.Weekdays),
		next.Interval, next.NotePrompt, next.StartDate, boolInt(next.Archived), boolInt(next.Pinned),
		toMillis(next.UpdatedAt), next.Version, id.String(), current.Version)
	if err != nil {
		return Project{}, fmt.Errorf("update project %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// The row vanished or moved between the read and the write.
		if _, err := s.Get(ctx, id); err != nil {
			return Project{}, err
		}
		return Project{}, fmt.Errorf("%w: project %s changed during update", ErrVersionConflict, id)
	}
	if err := s.setLinks(ctx, id, next.Prerequisites, next.Postrequisites); err != nil {
		return Project{}, err
	}
	return next, nil
}

// Delete removes a project and, by cascade, its check-ins and skips.
func (s *Projects) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.sql.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("delete project %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: project %s", ErrNotFound, id)
	}
	return nil
}

// Count returns how many projects exist, archived included.
func (s *Projects) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&n)
	return n, err
}

func scanProject(row rowScanner) (Project, error) {
	var p Project
	var id string
	var categoryID sql.NullString
	var archived, pinned int
	var weekdays string
	var createdAt, updatedAt int64
	if err := row.Scan(&id, &p.Name, &p.Description, &p.Icon, &p.Color, &categoryID,
		&p.Nature, &p.Schedule, &p.Mode, &p.Target, &p.Unit, &weekdays, &p.Interval,
		&p.NotePrompt, &p.StartDate,
		&archived, &pinned, &createdAt, &updatedAt, &p.Version); err != nil {
		return Project{}, err
	}
	p.ID, _ = uuid.Parse(id)
	p.CategoryID = scanCategoryID(categoryID)
	p.Archived = archived != 0
	p.Pinned = pinned != 0
	p.Weekdays = parseWeekdaysCSV(weekdays)
	p.CreatedAt = fromMillis(createdAt)
	p.UpdatedAt = fromMillis(updatedAt)
	return p, nil
}
