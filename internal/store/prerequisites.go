package store

import (
	"context"
	"fmt"
	"uuid"
)

// Prerequisite verdicts are derived, never stored (same invariant as streaks
// and misses): a check-in is "violated" when its project had an unmet
// prerequisite on the credited day. Backfilling the prerequisite later clears
// the violation on the next read, which is exactly the forgiveness the product
// wants ("I forgot to tick reading, then I backfilled it").

// UnmetByDay evaluates, for each requested day, which of p's prerequisites were
// NOT resolved on that day. One batched load per prerequisite keeps this cheap
// even for a long history page.
func (d *DB) UnmetByDay(ctx context.Context, p Project, days []string) (map[string][]uuid.UUID, error) {
	out := make(map[string][]uuid.UUID, len(days))
	if len(p.Prerequisites) == 0 || len(days) == 0 {
		return out, nil
	}
	maxDay := days[0]
	for _, day := range days {
		if day > maxDay {
			maxDay = day
		}
	}

	type prereqData struct {
		project Project
		agg     map[string]DayAggregate
		skips   map[string]string
	}
	loaded := make([]prereqData, 0, len(p.Prerequisites))
	for _, id := range p.Prerequisites {
		prereq, err := d.Projects().Get(ctx, id)
		if err != nil {
			// A deleted prerequisite cascades its edges away, so this only
			// happens on a stale read; treat it as resolved rather than fail.
			continue
		}
		agg, err := d.Checkins().Aggregates(ctx, id, prereq.StartDate, maxDay)
		if err != nil {
			return nil, err
		}
		skips, err := d.Skips().Map(ctx, id)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, prereqData{project: prereq, agg: agg, skips: skips})
	}

	for _, day := range days {
		unmet := []uuid.UUID{}
		for _, pd := range loaded {
			if !PrereqSatisfied(pd.project, day, pd.agg, pd.skips) {
				unmet = append(unmet, pd.project.ID)
			}
		}
		out[day] = unmet
	}
	return out, nil
}

// enrichCheckins fills the derived Violated/Unmet fields on a page of
// check-ins, grouping by project so each project's prerequisites load once.
func (d *DB) enrichCheckins(ctx context.Context, checkins []Checkin) error {
	if len(checkins) == 0 {
		return nil
	}
	byProject := map[uuid.UUID][]string{}
	for _, c := range checkins {
		byProject[c.ProjectID] = append(byProject[c.ProjectID], c.LocalDate)
	}
	verdicts := map[uuid.UUID]map[string][]uuid.UUID{}
	for id, days := range byProject {
		p, err := d.Projects().Get(ctx, id)
		if err != nil {
			return fmt.Errorf("enrich checkins: %w", err)
		}
		m, err := d.UnmetByDay(ctx, p, days)
		if err != nil {
			return err
		}
		verdicts[id] = m
	}
	for i := range checkins {
		unmet := verdicts[checkins[i].ProjectID][checkins[i].LocalDate]
		checkins[i].Unmet = unmet
		checkins[i].Violated = len(unmet) > 0
	}
	return nil
}

// countViolations sums, over the days that have check-ins, how many check-ins
// landed while a prerequisite was unmet. Used by project totals and overview.
func (d *DB) countViolations(ctx context.Context, p Project, agg map[string]DayAggregate) (int, error) {
	if len(p.Prerequisites) == 0 {
		return 0, nil
	}
	days := make([]string, 0, len(agg))
	for day := range agg {
		days = append(days, day)
	}
	m, err := d.UnmetByDay(ctx, p, days)
	if err != nil {
		return 0, err
	}
	total := 0
	for day, unmet := range m {
		if len(unmet) > 0 {
			total += agg[day].Count
		}
	}
	return total, nil
}
