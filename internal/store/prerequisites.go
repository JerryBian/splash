package store

import (
	"context"
	"fmt"
	"uuid"
)

// Link verdicts are derived, never stored (same invariant as streaks and
// misses). A prerequisite violation is judged on the day of the check-in; a
// postrequisite commitment is only a violation once its completion day has
// ended. Backfilling either side later clears the verdict on the next read,
// which is the forgiveness the product wants.

// enrichCheckins fills the derived link fields on a page of check-ins, grouping
// by project so each project's links load once.
//
// Prerequisites: Unmet/Violated, as before.
// Postrequisites: PostUnmet on a closed completion day, PostPending while the
// completion day is still today. Only the check-in day that actually reached
// the target carries the verdict, so a weekly goal's earlier partial days stay
// quiet.
func (d *DB) enrichCheckins(ctx context.Context, checkins []Checkin) error {
	if len(checkins) == 0 {
		return nil
	}
	byProject := map[uuid.UUID][]string{}
	for _, c := range checkins {
		byProject[c.ProjectID] = append(byProject[c.ProjectID], c.LocalDate)
	}

	type projectVerdicts struct {
		prereqs     map[string][]uuid.UUID
		postUnmet   map[string][]uuid.UUID
		postPending map[string][]uuid.UUID
	}
	verdicts := map[uuid.UUID]projectVerdicts{}
	today := d.Today()
	for id, days := range byProject {
		p, err := d.Projects().Get(ctx, id)
		if err != nil {
			return fmt.Errorf("enrich checkins: %w", err)
		}
		links, err := d.loadProjectLinks(ctx, p, today)
		if err != nil {
			return err
		}
		verdict := projectVerdicts{
			prereqs:     unmetPrereqsByDay(links.prereqs, days),
			postUnmet:   map[string][]uuid.UUID{},
			postPending: map[string][]uuid.UUID{},
		}
		if p.Nature != NatureBad && len(links.posts) > 0 {
			agg, err := d.Checkins().Aggregates(ctx, p.ID, p.StartDate, today)
			if err != nil {
				return err
			}
			// Completion days are per period; cache them so a page with many
			// check-ins in the same week does not re-walk the period.
			completionByPeriod := map[string]string{}
			for _, day := range days {
				completion := ""
				if p.Schedule == ScheduleIntervalHours {
					// Interval check-ins are events: any check-in day triggers.
					if agg[day].Count > 0 {
						completion = day
					}
				} else {
					period := PeriodFor(p, day)
					cached, seen := completionByPeriod[period.Key]
					if !seen {
						cached = periodCompletionDay(p, period, today, agg)
						completionByPeriod[period.Key] = cached
					}
					completion = cached
				}
				if completion == "" || completion != day {
					continue
				}
				unmet := unresolvedPostTargets(links.posts, day)
				if len(unmet) == 0 {
					continue
				}
				if day == today {
					verdict.postPending[day] = unmet
				} else {
					verdict.postUnmet[day] = unmet
				}
			}
		}
		verdicts[id] = verdict
	}

	for i := range checkins {
		checkin := &checkins[i]
		verdict := verdicts[checkin.ProjectID]
		unmet := verdict.prereqs[checkin.LocalDate]
		checkin.Unmet = unmet
		checkin.Violated = len(unmet) > 0
		checkin.PostUnmet = verdict.postUnmet[checkin.LocalDate]
		checkin.PostPending = verdict.postPending[checkin.LocalDate]
	}
	return nil
}
