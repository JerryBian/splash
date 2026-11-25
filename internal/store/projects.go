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
	target, unit, weekdays, interval_value, start_date, archived, created_at, updated_at, version`

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

	prereqs := parsePrerequisites(n.Prerequisites)
	if err := s.checkPrereqGraph(ctx, uuid.Nil(), prereqs); err != nil {
		return Project{}, err
	}

	now := s.db.now().UTC().Truncate(1e6)
	p := Project{
		ID:            uuid.NewV7(),
		Name:          n.Name,
		Description:   n.Description,
		Icon:          n.Icon,
		Color:         n.Color,
		CategoryID:    categoryID,
		Nature:        n.Nature,
		Schedule:      n.Schedule,
		Mode:          n.Mode,
		Target:        n.Target,
		Unit:          n.Unit,
		Weekdays:      n.Weekdays,
		Interval:      n.Interval,
		StartDate:     n.StartDate,
		Prerequisites: prereqs,
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
	}
	_, err := s.db.sql.ExecContext(ctx, `INSERT INTO projects (`+projectColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID.String(), p.Name, p.Description, p.Icon, p.Color, categoryArg(p.CategoryID),
		p.Nature, string(p.Schedule), p.Mode, p.Target, p.Unit, weekdaysCSV(p.Weekdays),
		p.Interval, p.StartDate, boolInt(p.Archived), toMillis(p.CreatedAt), toMillis(p.UpdatedAt), p.Version)
	if err != nil {
		return Project{}, fmt.Errorf("create project: %w", err)
	}
	if err := s.setPrereqs(ctx, p.ID, prereqs); err != nil {
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
	edges, err := s.prereqMap(ctx, []uuid.UUID{p.ID})
	if err != nil {
		return Project{}, err
	}
	p.Prerequisites = edges[p.ID]
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
	edges, err := s.prereqMap(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Prerequisites = edges[out[i].ID]
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
	if err := s.checkPrereqGraph(ctx, id, next.Prerequisites); err != nil {
		return Project{}, err
	}
	next.UpdatedAt = s.db.now().UTC().Truncate(1e6)
	next.Version = current.Version + 1

	res, err := s.db.sql.ExecContext(ctx, `UPDATE projects SET
		name = ?, description = ?, icon = ?, color = ?, category_id = ?, nature = ?, schedule = ?, mode = ?,
		target = ?, unit = ?, weekdays = ?, interval_value = ?, start_date = ?, archived = ?,
		updated_at = ?, version = ?
		WHERE id = ? AND version = ?`,
		next.Name, next.Description, next.Icon, next.Color, categoryArg(next.CategoryID),
		next.Nature, string(next.Schedule), next.Mode, next.Target, next.Unit, weekdaysCSV(next.Weekdays),
		next.Interval, next.StartDate, boolInt(next.Archived), toMillis(next.UpdatedAt), next.Version,
		id.String(), current.Version)
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
	if err := s.setPrereqs(ctx, id, next.Prerequisites); err != nil {
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
	var archived int
	var weekdays string
	var createdAt, updatedAt int64
	if err := row.Scan(&id, &p.Name, &p.Description, &p.Icon, &p.Color, &categoryID,
		&p.Nature, &p.Schedule, &p.Mode, &p.Target, &p.Unit, &weekdays, &p.Interval, &p.StartDate,
		&archived, &createdAt, &updatedAt, &p.Version); err != nil {
		return Project{}, err
	}
	p.ID, _ = uuid.Parse(id)
	p.CategoryID = scanCategoryID(categoryID)
	p.Archived = archived != 0
	p.Weekdays = parseWeekdaysCSV(weekdays)
	p.CreatedAt = fromMillis(createdAt)
	p.UpdatedAt = fromMillis(updatedAt)
	return p, nil
}

// prereqMap loads the prerequisite edges for a set of projects in one query.
func (s *Projects) prereqMap(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	out := make(map[uuid.UUID][]uuid.UUID, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id.String()
		out[id] = []uuid.UUID{}
	}
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT project_id, requires_id FROM prerequisites WHERE project_id IN (`+placeholders+`) ORDER BY requires_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("load prerequisites: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var pid, rid string
		if err := rows.Scan(&pid, &rid); err != nil {
			return nil, fmt.Errorf("load prerequisites: %w", err)
		}
		p, _ := uuid.Parse(pid)
		r, _ := uuid.Parse(rid)
		out[p] = append(out[p], r)
	}
	return out, rows.Err()
}

// setPrereqs replaces the edge list for one project.
func (s *Projects) setPrereqs(ctx context.Context, id uuid.UUID, prereqs []uuid.UUID) error {
	if _, err := s.db.sql.ExecContext(ctx, `DELETE FROM prerequisites WHERE project_id = ?`, id.String()); err != nil {
		return fmt.Errorf("replace prerequisites: %w", err)
	}
	for _, r := range prereqs {
		if _, err := s.db.sql.ExecContext(ctx,
			`INSERT INTO prerequisites (project_id, requires_id) VALUES (?, ?)`, id.String(), r.String()); err != nil {
			return fmt.Errorf("replace prerequisites: %w", err)
		}
	}
	return nil
}

// checkPrereqGraph validates that every prerequisite exists and that wiring
// them for `id` (uuid.Nil() for a not-yet-created project) introduces no cycle.
// The graph is small, so a full load + DFS is cheaper than recursive SQL.
func (s *Projects) checkPrereqGraph(ctx context.Context, id uuid.UUID, prereqs []uuid.UUID) error {
	if len(prereqs) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(prereqs)), ",")
	args := make([]any, len(prereqs))
	for i, r := range prereqs {
		args[i] = r.String()
	}
	var found int
	if err := s.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE id IN (`+placeholders+`)`, args...).Scan(&found); err != nil {
		return fmt.Errorf("validate prerequisites: %w", err)
	}
	if found != len(prereqs) {
		return ValidationErrors{{Field: "prerequisites", Message: "references a project that does not exist"}}
	}

	rows, err := s.db.sql.QueryContext(ctx, `SELECT project_id, requires_id FROM prerequisites`)
	if err != nil {
		return fmt.Errorf("validate prerequisites: %w", err)
	}
	defer func() { _ = rows.Close() }()
	adj := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var pid, rid string
		if err := rows.Scan(&pid, &rid); err != nil {
			return fmt.Errorf("validate prerequisites: %w", err)
		}
		p, _ := uuid.Parse(pid)
		r, _ := uuid.Parse(rid)
		adj[p] = append(adj[p], r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate prerequisites: %w", err)
	}
	if id != uuid.Nil() {
		adj[id] = prereqs
	}
	// DFS from each new prerequisite; reaching `id` means a cycle.
	target := id
	seen := map[uuid.UUID]bool{}
	var visit func(uuid.UUID) bool
	visit = func(n uuid.UUID) bool {
		if target != uuid.Nil() && n == target {
			return true
		}
		if seen[n] {
			return false
		}
		seen[n] = true
		for _, next := range adj[n] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	for _, r := range prereqs {
		if visit(r) {
			return ValidationErrors{{Field: "prerequisites", Message: "would create a dependency cycle"}}
		}
	}
	return nil
}
