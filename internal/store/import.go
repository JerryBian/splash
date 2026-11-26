package store

import (
	"context"
	"fmt"
	"uuid"
)

// Importer restores a backup produced by [Exporter]. *DB implements it.
type Importer interface {
	Import(ctx context.Context, data ExportData) (ImportCounts, error)
}

var _ Importer = (*DB)(nil)

// ImportCounts reports how many rows an import actually added (duplicates by id
// are skipped, so re-importing the same backup is a no-op).
type ImportCounts struct {
	Categories int `json:"categories"`
	Projects   int `json:"projects"`
	Checkins   int `json:"checkins"`
	Skips      int `json:"skips"`
}

// Import restores an export additively: every row is inserted by its original
// id with INSERT OR IGNORE, so existing data is never overwritten and importing
// the same file twice changes nothing. References are validated first, so a
// corrupt or tampered backup is rejected whole rather than half-applied.
//
// The whole insert runs in one transaction. As everywhere else in this package,
// no pool-backed store method is called inside the tx — the single connection
// would deadlock — so the inserts are raw SQL.
func (d *DB) Import(ctx context.Context, data ExportData) (ImportCounts, error) {
	var counts ImportCounts
	today := d.Today()

	// The set of projects a check-in/skip/prerequisite may reference: those in
	// the backup plus those already in the database. Loaded before the tx.
	validProjects := map[uuid.UUID]bool{}
	existing, err := d.Projects().List(ctx, ProjectQuery{IncludeArchived: true})
	if err != nil {
		return counts, err
	}
	for _, p := range existing {
		validProjects[p.ID] = true
	}

	var problems []*FieldError
	for _, p := range data.Projects {
		if p.ID == uuid.Nil() {
			problems = append(problems, &FieldError{Field: "projects", Message: "a project has no id"})
			continue
		}
		validProjects[p.ID] = true
		in := ProjectInput{
			Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color,
			Nature: p.Nature, Schedule: p.Schedule, Mode: p.Mode, Target: p.Target,
			Unit: p.Unit, Weekdays: p.Weekdays, Interval: p.Interval,
			NotePrompt: p.NotePrompt, StartDate: p.StartDate, Pinned: p.Pinned,
			Prerequisites: uuidStrings(p.Prerequisites), Postrequisites: uuidStrings(p.Postrequisites),
		}
		// Backups made before time mode was retired still say mode=time; the
		// closest equivalent now is a check habit (one record completes it).
		if in.Mode == "time" {
			in.Mode = ModeCheck
		}
		if p.CategoryID != uuid.Nil() {
			in.CategoryID = p.CategoryID.String()
		}
		if err := in.Validate(today); err != nil {
			problems = append(problems, &FieldError{Field: "projects", Message: fmt.Sprintf("%s: %v", p.Name, err)})
		}
	}
	for _, c := range data.Categories {
		if err := (CategoryInput{Name: c.Name, Icon: c.Icon, Color: c.Color}).Validate(); err != nil {
			problems = append(problems, &FieldError{Field: "categories", Message: fmt.Sprintf("%s: %v", c.Name, err)})
		}
	}
	for _, c := range data.Checkins {
		if !validProjects[c.ProjectID] {
			problems = append(problems, &FieldError{Field: "checkins", Message: "references a project that is not in the backup"})
		}
		if c.Value <= 0 || c.Value > MaxTargetValue {
			problems = append(problems, &FieldError{Field: "checkins", Message: "value out of range"})
		}
		if photoErr := validatePhoto(c.Photo); photoErr != nil {
			problems = append(problems, &FieldError{Field: "checkins", Message: photoErr.Message})
		}
		if err := validateDate("localDate", c.LocalDate); err != nil {
			problems = append(problems, err)
		}
	}
	for _, s := range data.Skips {
		if !validProjects[s.ProjectID] {
			problems = append(problems, &FieldError{Field: "skips", Message: "references a project that is not in the backup"})
		}
		if err := validateDate("date", s.Date); err != nil {
			problems = append(problems, err)
		}
	}
	for _, p := range data.Projects {
		for _, r := range p.Prerequisites {
			if !validProjects[r] {
				problems = append(problems, &FieldError{Field: "prerequisites", Message: "references a project that is not in the backup"})
			}
		}
		for _, r := range p.Postrequisites {
			if !validProjects[r] {
				problems = append(problems, &FieldError{Field: "postrequisites", Message: "references a project that is not in the backup"})
			}
		}
	}
	// The whole combined graph must stay acyclic; raw inserts bypass
	// checkLinkGraph, so validate it here before opening the transaction.
	adj := make(map[uuid.UUID][]uuid.UUID, len(existing)+len(data.Projects))
	for _, p := range existing {
		adj[p.ID] = append(append([]uuid.UUID{}, p.Prerequisites...), p.Postrequisites...)
	}
	for _, p := range data.Projects {
		adj[p.ID] = append(adj[p.ID], p.Prerequisites...)
		adj[p.ID] = append(adj[p.ID], p.Postrequisites...)
	}
	if hasLinkCycle(adj) {
		problems = append(problems, &FieldError{Field: "links", Message: "import would create a dependency cycle"})
	}
	if err := collect(problems); err != nil {
		return counts, err
	}

	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return counts, fmt.Errorf("import begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	exec := func(query string, args ...any) (int, error) {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}

	for _, c := range data.Categories {
		n, err := exec(`INSERT OR IGNORE INTO categories (id, name, icon, color, created_at) VALUES (?, ?, ?, ?, ?)`,
			c.ID.String(), c.Name, c.Icon, c.Color, toMillis(c.CreatedAt))
		if err != nil {
			return counts, fmt.Errorf("import category: %w", err)
		}
		counts.Categories += n
	}
	for _, p := range data.Projects {
		mode, target, unit := p.Mode, p.Target, p.Unit
		if mode == "time" {
			// Retired time-mode backups become check habits.
			mode, target, unit = ModeCheck, 1, "次"
		}
		n, err := exec(`INSERT OR IGNORE INTO projects (`+projectColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID.String(), p.Name, p.Description, p.Icon, p.Color, categoryArg(p.CategoryID),
			p.Nature, string(p.Schedule), mode, target, unit, weekdaysCSV(p.Weekdays),
			p.Interval, p.NotePrompt, p.StartDate, boolInt(p.Archived), boolInt(p.Pinned),
			toMillis(p.CreatedAt), toMillis(p.UpdatedAt), p.Version)
		if err != nil {
			return counts, fmt.Errorf("import project: %w", err)
		}
		counts.Projects += n
	}
	// Links go in a second pass: a postrequisite may point at a project created
	// later in the same backup, and the foreign key requires the row to exist.
	for _, p := range data.Projects {
		for _, r := range p.Prerequisites {
			if _, err := exec(`INSERT OR IGNORE INTO project_links (source_id, target_id, kind) VALUES (?, ?, 'requires')`,
				p.ID.String(), r.String()); err != nil {
				return counts, fmt.Errorf("import prerequisite: %w", err)
			}
		}
		for _, r := range p.Postrequisites {
			if _, err := exec(`INSERT OR IGNORE INTO project_links (source_id, target_id, kind) VALUES (?, ?, 'implies')`,
				p.ID.String(), r.String()); err != nil {
				return counts, fmt.Errorf("import postrequisite: %w", err)
			}
		}
	}
	for _, c := range data.Checkins {
		n, err := exec(`INSERT OR IGNORE INTO checkins (`+checkinColumns+`, photo) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID.String(), c.ProjectID.String(), c.Value, c.Note, c.Mood, c.LocalDate,
			toMillis(c.CheckedAt), c.TZOffset, boolInt(c.Backfilled), toMillis(c.CreatedAt), c.Photo)
		if err != nil {
			return counts, fmt.Errorf("import checkin: %w", err)
		}
		counts.Checkins += n
	}
	for _, s := range data.Skips {
		n, err := exec(`INSERT OR IGNORE INTO skips (id, project_id, skip_date, reason, created_at) VALUES (?, ?, ?, ?, ?)`,
			s.ID.String(), s.ProjectID.String(), s.Date, s.Reason, toMillis(s.CreatedAt))
		if err != nil {
			return counts, fmt.Errorf("import skip: %w", err)
		}
		counts.Skips += n
	}

	if err := tx.Commit(); err != nil {
		return counts, fmt.Errorf("import commit: %w", err)
	}
	return counts, nil
}
