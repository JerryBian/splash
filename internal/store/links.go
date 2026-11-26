package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"uuid"
)

// Same-day links come in two directions. A "requires" edge is a prerequisite:
// the source needs the target resolved on the day it is checked. An "implies"
// edge is a postrequisite: a completed source commits the target to the same
// day. Both live in one graph, so one cycle rule covers both directions.
const (
	linkRequires = "requires"
	linkImplies  = "implies"
)

// projectLinks is one project's resolved outgoing links, split by direction.
type projectLinks struct {
	prereqs []LinkTarget
	posts   []LinkTarget
}

// linkMaps loads the outgoing links for a set of projects in one query.
func (s *Projects) linkMaps(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]uuid.UUID, map[uuid.UUID][]uuid.UUID, error) {
	requires := make(map[uuid.UUID][]uuid.UUID, len(ids))
	implies := make(map[uuid.UUID][]uuid.UUID, len(ids))
	if len(ids) == 0 {
		return requires, implies, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id.String()
		requires[id] = []uuid.UUID{}
		implies[id] = []uuid.UUID{}
	}
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT source_id, target_id, kind FROM project_links WHERE source_id IN (`+placeholders+`) ORDER BY target_id`,
		args...)
	if err != nil {
		return nil, nil, fmt.Errorf("load project links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var source, target, kind string
		if err := rows.Scan(&source, &target, &kind); err != nil {
			return nil, nil, fmt.Errorf("load project links: %w", err)
		}
		sid, _ := uuid.Parse(source)
		tid, _ := uuid.Parse(target)
		switch kind {
		case linkRequires:
			requires[sid] = append(requires[sid], tid)
		case linkImplies:
			implies[sid] = append(implies[sid], tid)
		}
	}
	return requires, implies, rows.Err()
}

// setLinks replaces both outgoing lists of one project in a single transaction.
// The transaction uses raw SQL only: on the single connection, a pool-backed
// call inside it would deadlock.
func (s *Projects) setLinks(ctx context.Context, id uuid.UUID, prereqs, posts []uuid.UUID) error {
	tx, err := s.db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("replace project links: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_links WHERE source_id = ?`, id.String()); err != nil {
		return fmt.Errorf("replace project links: %w", err)
	}
	insert := func(target uuid.UUID, kind string) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO project_links (source_id, target_id, kind) VALUES (?, ?, ?)`,
			id.String(), target.String(), kind); err != nil {
			return fmt.Errorf("replace project links: %w", err)
		}
		return nil
	}
	for _, target := range prereqs {
		if err := insert(target, linkRequires); err != nil {
			return err
		}
	}
	for _, target := range posts {
		if err := insert(target, linkImplies); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("replace project links: %w", err)
	}
	return nil
}

// checkLinkGraph validates that every linked target exists, that the two
// directions do not overlap, and that wiring the links for id (uuid.Nil() for a
// not-yet-created project) introduces no cycle. The graph is small, so a full
// load + DFS is cheaper than recursive SQL.
func (s *Projects) checkLinkGraph(ctx context.Context, id uuid.UUID, prereqs, posts []uuid.UUID) error {
	if err := s.linkTargetsExist(ctx, "prerequisites", prereqs); err != nil {
		return err
	}
	if err := s.linkTargetsExist(ctx, "postrequisites", posts); err != nil {
		return err
	}
	if err := validateLinkDisjointIDs(prereqs, posts); err != nil {
		return ValidationErrors{*err}
	}
	if id == uuid.Nil() || len(prereqs)+len(posts) == 0 {
		// A brand-new project has no incoming edges yet, so its outgoing links
		// cannot close a cycle.
		return nil
	}

	rows, err := s.db.sql.QueryContext(ctx, `SELECT source_id, target_id FROM project_links`)
	if err != nil {
		return fmt.Errorf("validate project links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	adj := map[uuid.UUID][]uuid.UUID{}
	for rows.Next() {
		var source, target string
		if err := rows.Scan(&source, &target); err != nil {
			return fmt.Errorf("validate project links: %w", err)
		}
		sid, _ := uuid.Parse(source)
		tid, _ := uuid.Parse(target)
		adj[sid] = append(adj[sid], tid)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("validate project links: %w", err)
	}
	next := make([]uuid.UUID, 0, len(prereqs)+len(posts))
	next = append(next, prereqs...)
	next = append(next, posts...)
	adj[id] = next

	reaches := func(start uuid.UUID) bool {
		seen := map[uuid.UUID]bool{}
		var visit func(uuid.UUID) bool
		visit = func(node uuid.UUID) bool {
			if node == id {
				return true
			}
			if seen[node] {
				return false
			}
			seen[node] = true
			for _, candidate := range adj[node] {
				if visit(candidate) {
					return true
				}
			}
			return false
		}
		return visit(start)
	}
	for _, target := range prereqs {
		if reaches(target) {
			return ValidationErrors{{Field: "prerequisites", Message: "would create a dependency cycle"}}
		}
	}
	for _, target := range posts {
		if reaches(target) {
			return ValidationErrors{{Field: "postrequisites", Message: "would create a dependency cycle"}}
		}
	}
	return nil
}

func (s *Projects) linkTargetsExist(ctx context.Context, field string, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id.String()
	}
	var found int
	if err := s.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM projects WHERE id IN (`+placeholders+`)`, args...).Scan(&found); err != nil {
		return fmt.Errorf("validate %s: %w", field, err)
	}
	if found != len(ids) {
		return ValidationErrors{{Field: field, Message: "references a project that does not exist"}}
	}
	return nil
}

// hasLinkCycle reports whether a directed adjacency list contains a cycle.
// Import uses it on the whole combined graph before writing anything, because
// raw inserts bypass checkLinkGraph.
func hasLinkCycle(adj map[uuid.UUID][]uuid.UUID) bool {
	const (
		visiting = 1
		done     = 2
	)
	state := make(map[uuid.UUID]int, len(adj))
	var visit func(uuid.UUID) bool
	visit = func(node uuid.UUID) bool {
		switch state[node] {
		case visiting:
			return true
		case done:
			return false
		}
		state[node] = visiting
		for _, next := range adj[node] {
			if visit(next) {
				return true
			}
		}
		state[node] = done
		return false
	}
	for node := range adj {
		if visit(node) {
			return true
		}
	}
	return false
}

// loadProjectLinks loads the target snapshots for p's outgoing links up to
// maxDay. A target deleted between the project read and this load is skipped:
// the foreign key removed its edge, so a stale view is not a failure.
func (d *DB) loadProjectLinks(ctx context.Context, p Project, maxDay string) (projectLinks, error) {
	ids := make([]uuid.UUID, 0, len(p.Prerequisites)+len(p.Postrequisites))
	ids = append(ids, p.Prerequisites...)
	ids = append(ids, p.Postrequisites...)
	targets, err := d.loadLinkTargets(ctx, ids, maxDay)
	if err != nil {
		return projectLinks{}, err
	}
	byID := make(map[uuid.UUID]LinkTarget, len(targets))
	for _, target := range targets {
		byID[target.Project.ID] = target
	}
	var out projectLinks
	for _, id := range p.Prerequisites {
		if target, ok := byID[id]; ok {
			out.prereqs = append(out.prereqs, target)
		}
	}
	for _, id := range p.Postrequisites {
		if target, ok := byID[id]; ok {
			out.posts = append(out.posts, target)
		}
	}
	return out, nil
}

func (d *DB) loadLinkTargets(ctx context.Context, ids []uuid.UUID, maxDay string) ([]LinkTarget, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]LinkTarget, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		target, err := d.Projects().Get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		agg, err := d.Checkins().Aggregates(ctx, id, target.StartDate, maxDay)
		if err != nil {
			return nil, err
		}
		skips, err := d.Skips().Map(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, LinkTarget{Project: target, Agg: agg, Skips: skips})
	}
	return out, nil
}

// uuidStrings renders stored ids for the string-based input/patch structs.
func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// unmetPrereqsByDay reports which of prereqs were unresolved on each day.
func unmetPrereqsByDay(prereqs []LinkTarget, days []string) map[string][]uuid.UUID {
	out := make(map[string][]uuid.UUID, len(days))
	for _, day := range days {
		unmet := []uuid.UUID{}
		for _, target := range prereqs {
			if !PrereqSatisfied(target.Project, day, target.Agg, target.Skips) {
				unmet = append(unmet, target.Project.ID)
			}
		}
		out[day] = unmet
	}
	return out
}

// countPrereqViolations sums, over days that have check-ins, the check-ins that
// landed while a prerequisite was unmet. One per check-in, as before.
func countPrereqViolations(prereqs []LinkTarget, agg map[string]DayAggregate) int {
	if len(prereqs) == 0 {
		return 0
	}
	days := make([]string, 0, len(agg))
	for day := range agg {
		days = append(days, day)
	}
	total := 0
	for day, unmet := range unmetPrereqsByDay(prereqs, days) {
		if len(unmet) > 0 {
			total += agg[day].Count
		}
	}
	return total
}
