package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"
)

// StatsStore computes statistics. It is an interface so handlers depend on the
// computation contract, not on the queries behind it.
type StatsStore interface {
	ForProject(ctx context.Context, p Project, today string, weeks int) (ProjectStats, error)
	Overview(ctx context.Context, projects []Project, today string) (OverviewStats, error)
}

// Stats is the SQLite-backed StatsStore.
type Stats struct{ db *DB }

var _ StatsStore = (*Stats)(nil)

// TodayCounters is the headline of the Today page.
type TodayCounters struct {
	Date    string `json:"date"`
	Total   int    `json:"total"` // projects with an obligation today (rest days excluded)
	Done    int    `json:"done"`
	Pending int    `json:"pending"` // pending + partial + waiting
	Skipped int    `json:"skipped"`
	Due     int    `json:"due"` // interval_hours countdowns that elapsed
}

// OverviewTotals are the lifetime stat tiles.
type OverviewTotals struct {
	Checkins       int `json:"checkins"`
	ActiveProjects int `json:"activeProjects"`
	BestStreak     int `json:"bestStreak"`
	MonthCheckins  int `json:"monthCheckins"`
}

// DayActivity is one day of the cross-project activity heatmap.
type DayActivity struct {
	Date  string  `json:"date"`
	Count int     `json:"count"`
	Value float64 `json:"value"`
}

// CategoryCount is one slice of the category distribution (last 30 days).
type CategoryCount struct {
	ID    string `json:"id"` // "" for uncategorized
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
	Count int    `json:"count"`
}

// Leader is one row of the streak leaderboard.
type Leader struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Icon       string `json:"icon"`
	Color      string `json:"color"`
	Streak     int    `json:"streak"`
	BestStreak int    `json:"bestStreak"`
}

// WeekCount is one bar of the 8-week trend.
type WeekCount struct {
	WeekStart string `json:"weekStart"`
	Count     int    `json:"count"`
}

// OverviewStats is everything the Stats page renders, in one response.
type OverviewStats struct {
	Today      TodayCounters   `json:"today"`
	Totals     OverviewTotals  `json:"totals"`
	Activity   []DayActivity   `json:"activity"` // last 56 days
	Categories []CategoryCount `json:"categories"`
	Leaders    []Leader        `json:"leaders"`
	Trend      []WeekCount     `json:"trend"`
}

// ForProject computes one project's stats. weeks controls the heatmap length;
// 0 skips heatmap and bars (the Today listing only needs summary and totals).
func (s *Stats) ForProject(ctx context.Context, p Project, today string, weeks int) (ProjectStats, error) {
	agg, err := s.db.Checkins().Aggregates(ctx, p.ID, p.StartDate, today)
	if err != nil {
		return ProjectStats{}, err
	}
	skips, err := s.db.Skips().Map(ctx, p.ID)
	if err != nil {
		return ProjectStats{}, err
	}
	var checkins []Checkin
	if p.Schedule == ScheduleIntervalHours {
		checkins, err = s.db.Checkins().AllAsc(ctx, p.ID)
		if err != nil {
			return ProjectStats{}, err
		}
	}

	out := BuildStats(StatsInput{
		Project:    p,
		Today:      today,
		Now:        s.db.now(),
		Weeks:      weeks,
		Aggregates: agg,
		Skips:      skips,
		Checkins:   checkins,
	})

	recent, err := s.db.Checkins().List(ctx, CheckinQuery{ProjectID: p.ID, Limit: 30})
	if err != nil {
		return ProjectStats{}, err
	}
	out.Recent = recent
	skipList, err := s.db.Skips().List(ctx, p.ID, "", "")
	if err != nil {
		return ProjectStats{}, err
	}
	out.Skips = skipList
	return out, nil
}

// Overview computes the cross-project dashboard for the given (already
// visibility-filtered) projects.
func (s *Stats) Overview(ctx context.Context, projects []Project, today string) (OverviewStats, error) {
	out := OverviewStats{
		Today:      TodayCounters{Date: today},
		Activity:   []DayActivity{},
		Categories: []CategoryCount{},
		Leaders:    []Leader{},
		Trend:      []WeekCount{},
	}
	out.Totals.ActiveProjects = len(projects)
	if len(projects) == 0 {
		return out, nil
	}

	ids := make([]string, len(projects))
	for i, p := range projects {
		ids[i] = p.ID.String()
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	idArgs := make([]any, len(ids))
	for i, id := range ids {
		idArgs[i] = id
	}

	// Per-project summaries feed the today counters and the leaderboard.
	for _, p := range projects {
		stats, err := s.ForProject(ctx, p, today, 0)
		if err != nil {
			return OverviewStats{}, err
		}
		switch stats.Summary.Status {
		case StatusDone:
			out.Today.Total++
			out.Today.Done++
		case StatusSkipped:
			out.Today.Total++
			out.Today.Skipped++
		case StatusRest:
			// no obligation today
		case StatusDue:
			out.Today.Total++
			out.Today.Due++
		default:
			out.Today.Total++
			out.Today.Pending++
		}
		out.Totals.BestStreak = max(out.Totals.BestStreak, stats.Summary.BestStreak)
		out.Leaders = append(out.Leaders, Leader{
			ID: p.ID.String(), Name: p.Name, Icon: p.Icon, Color: p.Color,
			Streak: stats.Summary.Streak, BestStreak: stats.Summary.BestStreak,
		})
	}
	slices.SortFunc(out.Leaders, func(a, b Leader) int {
		if c := b.Streak - a.Streak; c != 0 {
			return c
		}
		return b.BestStreak - a.BestStreak
	})
	if len(out.Leaders) > 5 {
		out.Leaders = out.Leaders[:5]
	}

	// Lifetime and monthly check-in counts, one query each.
	if err := s.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM checkins WHERE project_id IN (`+placeholders+`)`, idArgs...,
	).Scan(&out.Totals.Checkins); err != nil {
		return OverviewStats{}, fmt.Errorf("overview totals: %w", err)
	}
	monthStart := MonthStart(today)
	if err := s.db.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM checkins WHERE local_date >= ? AND project_id IN (`+placeholders+`)`,
		append([]any{monthStart}, idArgs...)...,
	).Scan(&out.Totals.MonthCheckins); err != nil {
		return OverviewStats{}, fmt.Errorf("overview month: %w", err)
	}

	// 56 days of activity, one query; the 8-week trend is derived from it.
	from := AddDays(today, -55)
	rows, err := s.db.sql.QueryContext(ctx,
		`SELECT local_date, COUNT(*), SUM(value) FROM checkins
		 WHERE local_date >= ? AND local_date <= ? AND project_id IN (`+placeholders+`)
		 GROUP BY local_date ORDER BY local_date`,
		append([]any{from, today}, idArgs...)...)
	if err != nil {
		return OverviewStats{}, fmt.Errorf("overview activity: %w", err)
	}
	byDate := map[string]DayAggregate{}
	for rows.Next() {
		var d DayActivity
		if err := rows.Scan(&d.Date, &d.Count, &d.Value); err != nil {
			_ = rows.Close()
			return OverviewStats{}, fmt.Errorf("overview activity: %w", err)
		}
		byDate[d.Date] = DayAggregate{Count: d.Count, Value: d.Value}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return OverviewStats{}, fmt.Errorf("overview activity: %w", err)
	}
	for d := from; d <= today; d = AddDays(d, 1) {
		agg := byDate[d]
		out.Activity = append(out.Activity, DayActivity{Date: d, Count: agg.Count, Value: agg.Value})
	}
	thisWeek := WeekStart(today)
	for i := 7; i >= 0; i-- {
		ws := AddDays(thisWeek, -7*i)
		count := 0
		for d := ws; d <= AddDays(ws, 6) && d <= today; d = AddDays(d, 1) {
			count += byDate[d].Count
		}
		out.Trend = append(out.Trend, WeekCount{WeekStart: ws, Count: count})
	}

	// Category distribution over the last 30 days. Uncategorized projects roll
	// up into one bucket with an empty id.
	catFrom := AddDays(today, -29)
	catRows, err := s.db.sql.QueryContext(ctx,
		`SELECT COALESCE(c.id, ''), COALESCE(c.name, '未分类'), COALESCE(c.icon, '🗂️'), COALESCE(c.color, 'blue'), COUNT(*)
		 FROM checkins k
		 JOIN projects p ON p.id = k.project_id
		 LEFT JOIN categories c ON c.id = p.category_id
		 WHERE k.local_date >= ? AND k.project_id IN (`+placeholders+`)
		 GROUP BY c.id ORDER BY COUNT(*) DESC`,
		append([]any{catFrom}, idArgs...)...)
	if err != nil {
		return OverviewStats{}, fmt.Errorf("overview categories: %w", err)
	}
	defer func() { _ = catRows.Close() }()
	for catRows.Next() {
		var cc CategoryCount
		if err := catRows.Scan(&cc.ID, &cc.Name, &cc.Icon, &cc.Color, &cc.Count); err != nil {
			return OverviewStats{}, fmt.Errorf("overview categories: %w", err)
		}
		out.Categories = append(out.Categories, cc)
	}
	return out, catRows.Err()
}

// ExportData is a full backup of the database, the payload of GET /export.
type ExportData struct {
	ExportedAt time.Time  `json:"exportedAt"`
	Categories []Category `json:"categories"`
	Projects   []Project  `json:"projects"`
	Checkins   []Checkin  `json:"checkins"`
	Skips      []Skip     `json:"skips"`
}

// Exporter is the backup contract; *DB implements it.
type Exporter interface {
	Dump(ctx context.Context) (ExportData, error)
}

var _ Exporter = (*DB)(nil)

// Dump reads every row, for backups. Check-ins come back in instant order.
func (d *DB) Dump(ctx context.Context) (ExportData, error) {
	out := ExportData{ExportedAt: d.now().UTC()}
	var err error
	out.Categories, err = d.Categories().List(ctx)
	if err != nil {
		return ExportData{}, err
	}
	out.Projects, err = d.Projects().List(ctx, ProjectQuery{IncludeArchived: true})
	if err != nil {
		return ExportData{}, err
	}

	rows, err := d.sql.QueryContext(ctx, `SELECT `+checkinColumns+` FROM checkins ORDER BY checked_at, id`)
	if err != nil {
		return ExportData{}, fmt.Errorf("dump checkins: %w", err)
	}
	out.Checkins = []Checkin{}
	for rows.Next() {
		c, err := scanCheckin(rows)
		if err != nil {
			_ = rows.Close()
			return ExportData{}, fmt.Errorf("dump checkins: %w", err)
		}
		out.Checkins = append(out.Checkins, c)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return ExportData{}, fmt.Errorf("dump checkins: %w", err)
	}

	skipRows, err := d.sql.QueryContext(ctx, `SELECT id, project_id, skip_date, reason, created_at FROM skips ORDER BY skip_date`)
	if err != nil {
		return ExportData{}, fmt.Errorf("dump skips: %w", err)
	}
	defer func() { _ = skipRows.Close() }()
	out.Skips = []Skip{}
	for skipRows.Next() {
		var sk Skip
		var id, pid string
		var createdAt int64
		if err := skipRows.Scan(&id, &pid, &sk.Date, &sk.Reason, &createdAt); err != nil {
			return ExportData{}, fmt.Errorf("dump skips: %w", err)
		}
		sk.ID, _ = uuid.Parse(id)
		sk.ProjectID, _ = uuid.Parse(pid)
		sk.CreatedAt = fromMillis(createdAt)
		out.Skips = append(out.Skips, sk)
	}
	return out, skipRows.Err()
}
