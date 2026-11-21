package store

import (
	"context"
	"fmt"
	"uuid"
)

// CategoryStore is the storage contract for categories.
type CategoryStore interface {
	Create(ctx context.Context, in CategoryInput) (Category, error)
	Get(ctx context.Context, id uuid.UUID) (Category, error)
	List(ctx context.Context) ([]Category, error)
	Update(ctx context.Context, id uuid.UUID, in CategoryInput) (Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Count(ctx context.Context) (int, error)
}

// Categories is the SQLite-backed CategoryStore.
type Categories struct{ db *DB }

var _ CategoryStore = (*Categories)(nil)

// Create validates and stores a new category. A duplicate name (any case)
// reports ErrConflict.
func (s *Categories) Create(ctx context.Context, in CategoryInput) (Category, error) {
	if err := in.Validate(); err != nil {
		return Category{}, err
	}
	n := in.Normalized()
	c := Category{
		ID:        uuid.NewV7(),
		Name:      n.Name,
		Icon:      n.Icon,
		Color:     n.Color,
		CreatedAt: s.db.now().UTC().Truncate(1e6),
	}
	_, err := s.db.sql.ExecContext(ctx,
		`INSERT INTO categories (id, name, icon, color, created_at) VALUES (?, ?, ?, ?, ?)`,
		c.ID.String(), c.Name, c.Icon, c.Color, toMillis(c.CreatedAt))
	if err != nil {
		if isUniqueErr(err) {
			return Category{}, fmt.Errorf("%w: category name %q", ErrConflict, c.Name)
		}
		return Category{}, fmt.Errorf("create category: %w", err)
	}
	return c, nil
}

// Get returns one category, or ErrNotFound.
func (s *Categories) Get(ctx context.Context, id uuid.UUID) (Category, error) {
	if id == uuid.Nil() {
		return Category{}, fmt.Errorf("%w: nil identifier", ErrNotFound)
	}
	row := s.db.sql.QueryRowContext(ctx,
		`SELECT id, name, icon, color, created_at FROM categories WHERE id = ?`, id.String())
	c, err := scanCategory(row)
	if err != nil {
		return Category{}, fmt.Errorf("get category %s: %w", id, errNoRows(err, "category "+id.String()))
	}
	return c, nil
}

// List returns every category, oldest first.
func (s *Categories) List(ctx context.Context) ([]Category, error) {
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT id, name, icon, color, created_at FROM categories ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("list categories: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Update replaces a category's editable fields.
func (s *Categories) Update(ctx context.Context, id uuid.UUID, in CategoryInput) (Category, error) {
	if err := in.Validate(); err != nil {
		return Category{}, err
	}
	current, err := s.Get(ctx, id)
	if err != nil {
		return Category{}, err
	}
	n := in.Normalized()
	current.Name, current.Icon, current.Color = n.Name, n.Icon, n.Color
	res, err := s.db.sql.ExecContext(ctx,
		`UPDATE categories SET name = ?, icon = ?, color = ? WHERE id = ?`,
		current.Name, current.Icon, current.Color, id.String())
	if err != nil {
		if isUniqueErr(err) {
			return Category{}, fmt.Errorf("%w: category name %q", ErrConflict, current.Name)
		}
		return Category{}, fmt.Errorf("update category %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Category{}, fmt.Errorf("%w: category %s", ErrNotFound, id)
	}
	return current, nil
}

// Delete removes a category. Projects in it become uncategorized (the foreign
// key's ON DELETE SET NULL), never deleted.
func (s *Categories) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.sql.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id.String())
	if err != nil {
		return fmt.Errorf("delete category %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: category %s", ErrNotFound, id)
	}
	return nil
}

// Count returns how many categories exist.
func (s *Categories) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories`).Scan(&n)
	return n, err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCategory(row rowScanner) (Category, error) {
	var c Category
	var id string
	var createdAt int64
	if err := row.Scan(&id, &c.Name, &c.Icon, &c.Color, &createdAt); err != nil {
		return Category{}, err
	}
	c.ID, _ = uuid.Parse(id)
	c.CreatedAt = fromMillis(createdAt)
	return c, nil
}
