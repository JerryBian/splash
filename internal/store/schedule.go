package store

import (
	"math"
	"time"
	"uuid"
)

// DateLayout is the canonical calendar-day format. Every date in the domain is
// a string in this layout, compared lexicographically, so period arithmetic
// never depends on a location or a wall clock.
const DateLayout = "2006-01-02"

// maxPeriodWalk bounds the forward walk from StartDate to today. Validation
// keeps StartDate at or after 2000-01-01, so a daily project can produce at
// most ~10k periods; the cap is belt-and-braces against a corrupt row.
const maxPeriodWalk = 12000

// Day statuses. They describe one calendar day (heatmap cells) or one period
// (summary, bars) relative to the project's schedule:
//
//	done     target met
//	partial  progress recorded, target not met, period still open
//	pending  scheduled, nothing recorded yet, period still open
//	missed   period ended without meeting the target and without an excuse
//	skipped  excused by a skip record; never breaks a streak
//	rest     not a scheduled day (weekdays schedule)
//	due      interval_hours: the countdown has elapsed, check in now
//	waiting  interval_hours: counting down to the next check-in
//	none     outside the project's life, or no day-level verdict (week/month)
//
// For a BAD project (Nature == NatureBad) the same tokens carry the inverted
// verdict, because the goal is abstention rather than accumulation:
//
//	done     no lapse recorded — the period was survived (or is clean so far)
//	missed   a lapse was recorded — the period is spoiled, even while open
//	pending  unused (a clean open period reads as done, not waiting)
//
// The streak therefore counts survived periods, and CompletionRate is the
// share of periods kept clean. partial/due/waiting never occur for bad habits.
const (
	StatusDone    = "done"
	StatusPartial = "partial"
	StatusPending = "pending"
	StatusMissed  = "missed"
	StatusSkipped = "skipped"
	StatusRest    = "rest"
	StatusDue     = "due"
	StatusWaiting = "waiting"
	StatusNone    = "none"
)

// ParseDate parses a canonical date string at UTC midnight.
func ParseDate(s string) (time.Time, error) { return time.Parse(DateLayout, s) }

// DateKey renders t as a canonical date string in t's own location.
func DateKey(t time.Time) string { return t.Format(DateLayout) }

// AddDays shifts a date string by n days.
func AddDays(date string, n int) string {
	t, err := ParseDate(date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 0, n).Format(DateLayout)
}

// WeekdayOf returns 1 (Monday) through 7 (Sunday).
func WeekdayOf(date string) int {
	t, err := ParseDate(date)
	if err != nil {
		return 1
	}
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

// WeekStart returns the Monday of the week containing date.
func WeekStart(date string) string { return AddDays(date, -(WeekdayOf(date) - 1)) }

// MonthStart returns the first day of date's month.
func MonthStart(date string) string { return date[:7] + "-01" }

// MonthEnd returns the last day of date's month.
func MonthEnd(date string) string {
	t, err := ParseDate(date)
	if err != nil {
		return date
	}
	return t.AddDate(0, 1, -t.Day()).Format(DateLayout)
}

// DaysBetween returns b - a in whole days.
func DaysBetween(a, b string) int {
	ta, err1 := ParseDate(a)
	tb, err2 := ParseDate(b)
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

func maxDate(a, b string) string {
	if a > b {
		return a
	}
	return b
}

func minDate(a, b string) string {
	if a < b {
		return a
	}
	return b
}

// Period is one scheduled window, inclusive on both ends. Key identifies it
// stably: the date itself, the Monday, the month, or the window's first day.
type Period struct {
	Key   string
	Start string
	End   string
}

// PeriodFor returns the period containing date. Dates before the project's
// start clamp to the first period, so callers never get a negative window.
func PeriodFor(p Project, date string) Period {
	switch p.Schedule {
	case ScheduleWeekly:
		start := WeekStart(date)
		return Period{Key: start, Start: start, End: AddDays(start, 6)}
	case ScheduleMonthly:
		start := MonthStart(date)
		return Period{Key: date[:7], Start: start, End: MonthEnd(date)}
	case ScheduleIntervalDays:
		n := int(p.Interval)
		if n < 1 {
			n = 1
		}
		idx := DaysBetween(p.StartDate, date) / n
		if idx < 0 {
			idx = 0
		}
		start := AddDays(p.StartDate, idx*n)
		return Period{Key: start, Start: start, End: AddDays(start, n-1)}
	default: // daily, weekdays: one day is one period
		return Period{Key: date, Start: date, End: date}
	}
}

// NextPeriod returns the period after per.
func NextPeriod(p Project, per Period) Period {
	switch p.Schedule {
	case ScheduleWeekly:
		return PeriodFor(p, AddDays(per.Start, 7))
	case ScheduleMonthly:
		return PeriodFor(p, AddDays(per.End, 1))
	default:
		return PeriodFor(p, AddDays(per.End, 1))
	}
}

// periodShiftBack moves n periods into the past, for the trend bars.
func periodShiftBack(p Project, date string, n int) string {
	switch p.Schedule {
	case ScheduleWeekly:
		return AddDays(date, -7*n)
	case ScheduleMonthly:
		t, err := ParseDate(MonthStart(date))
		if err != nil {
			return date
		}
		return t.AddDate(0, -n, 0).Format(DateLayout)
	case ScheduleIntervalDays:
		step := int(p.Interval)
		if step < 1 {
			step = 1
		}
		return AddDays(date, -step*n)
	default:
		return AddDays(date, -n)
	}
}

// ScheduledOn reports whether date carries an obligation for p. Rest days
// (weekdays schedule) never count as missed and never break a streak.
func ScheduledOn(p Project, date string) bool {
	if p.Schedule == ScheduleWeekdays {
		wd := WeekdayOf(date)
		for _, d := range p.Weekdays {
			if d == wd {
				return true
			}
		}
		return false
	}
	return true
}

// Summary is the project's state right now: what the Today card renders.
type Summary struct {
	Status        string     `json:"status"`
	PeriodStart   string     `json:"periodStart"`
	PeriodEnd     string     `json:"periodEnd"`
	Value         float64    `json:"value"`  // progress in the current period
	Target        float64    `json:"target"` // 0 for interval_hours (no period target)
	TodayValue    float64    `json:"todayValue"`
	Count         int        `json:"count"` // check-ins in the current period
	Streak        int        `json:"streak"`
	BestStreak    int        `json:"bestStreak"`
	DueAt         *time.Time `json:"dueAt,omitempty"`         // interval_hours
	LastCheckinAt *time.Time `json:"lastCheckinAt,omitempty"` // interval_hours
	// Unmet lists prerequisite project ids not resolved today (gate open?).
	Unmet []string `json:"unmet"`
	// PendingPosts lists postrequisites committed by a completion today that
	// are still unresolved. They are not violations yet: the day is still open.
	PendingPosts []string `json:"pendingPosts"`
	// BrokenPosts lists postrequisites that were still unresolved after the
	// current period's completion day closed.
	BrokenPosts []string `json:"brokenPosts"`
}

// Totals is the lifetime roll-up shown on the detail page. For a bad project
// the tokens invert with the nature: Checkins/Value/ActiveDays count lapse
// records, Done counts survived periods, Missed counts spoiled ones, and
// CompletionRate is the share kept clean.
type Totals struct {
	Checkins       int     `json:"checkins"`
	Value          float64 `json:"value"`
	ActiveDays     int     `json:"activeDays"`
	Done           int     `json:"done"`
	Missed         int     `json:"missed"`
	Skipped        int     `json:"skipped"`
	CompletionRate float64 `json:"completionRate"` // done / (done + missed); the clean rate for bad habits
	OnTimeRate     float64 `json:"onTimeRate"`     // interval_hours
	OnTimeCount    int     `json:"onTimeCount"`    // records inside the interval
	LateCount      int     `json:"lateCount"`      // interval_hours
	Backfilled     int     `json:"backfilled"`     // check-ins recorded after their credited day
	Violations     int     `json:"violations"`     // check-ins landed while a prerequisite was unmet
	// PostReqs and PostViolations count postrequisite link-days on closed days:
	// every (completion day, target) pair is one commitment, and a violation is
	// one pair left unresolved after that day. CommitmentRate is the share kept.
	PostReqs       int     `json:"postReqs"`
	PostViolations int     `json:"postViolations"`
	CommitmentRate float64 `json:"commitmentRate"`
}

// HeatCell is one day of the calendar heatmap.
type HeatCell struct {
	Date   string  `json:"date"`
	Value  float64 `json:"value"`
	Count  int     `json:"count"`
	Status string  `json:"status"`
}

// PeriodBar is one period of the trend chart.
type PeriodBar struct {
	Key    string  `json:"key"`
	Start  string  `json:"start"`
	End    string  `json:"end"`
	Value  float64 `json:"value"`
	Target float64 `json:"target"`
	Status string  `json:"status"`
}

// ProjectStats is everything the detail page needs. Recent and Skips are
// filled by the store (they are queries, not computations); BuildStats fills
// the rest from aggregates.
type ProjectStats struct {
	Summary Summary     `json:"summary"`
	Totals  Totals      `json:"totals"`
	Heatmap []HeatCell  `json:"heatmap"`
	Periods []PeriodBar `json:"periods"`
	Recent  []Checkin   `json:"recent"`
	Skips   []Skip      `json:"skips"`
	// BrokenCommitments lists postrequisite links whose completion day closed
	// without the target being resolved. Derived, never stored.
	BrokenCommitments []BrokenCommitment `json:"brokenCommitments"`
}

// BrokenCommitment is one postrequisite link missed on one completion day.
type BrokenCommitment struct {
	Date     string    `json:"date"`
	TargetID uuid.UUID `json:"targetId"`
}

// LinkTarget is one end of a same-day link plus the snapshots needed to decide
// whether it was resolved on a day. Verdicts are pure input to BuildStats.
type LinkTarget struct {
	Project Project
	Agg     map[string]DayAggregate
	Skips   map[string]string
}

// StatsInput carries everything BuildStats needs. Aggregates and Skips cover
// the project's whole life; Checkins (ascending by CheckedAt) is only required
// for interval_hours streaks.
type StatsInput struct {
	Project    Project
	Today      string
	Now        time.Time
	Weeks      int // heatmap length in weeks; 0 skips heatmap and bars
	Aggregates map[string]DayAggregate
	Skips      map[string]string
	Checkins   []Checkin
	// Posts are the source project's postrequisite targets with their day
	// snapshots. Empty means the project made no follow-up commitments.
	Posts []LinkTarget
}

// BuildStats computes summary, totals, heatmap and trend bars. It is pure: no
// clock reads, no queries, so every rule below is table-testable.
func BuildStats(in StatsInput) ProjectStats {
	if in.Project.Schedule == ScheduleIntervalHours {
		return buildIntervalStats(in)
	}
	return buildPeriodStats(in)
}

// periodValue sums the aggregates inside per, clipped to the project's life
// and to today (future days contribute nothing).
func periodValue(in StatsInput, per Period) (float64, int) {
	from := maxDate(per.Start, in.Project.StartDate)
	to := minDate(per.End, in.Today)
	var value float64
	var count int
	for d := from; d <= to; d = AddDays(d, 1) {
		agg := in.Aggregates[d]
		value += agg.Value
		count += agg.Count
	}
	return value, count
}

func periodHasSkip(in StatsInput, per Period) bool {
	from := maxDate(per.Start, in.Project.StartDate)
	to := minDate(per.End, in.Today)
	for d := from; d <= to; d = AddDays(d, 1) {
		if _, ok := in.Skips[d]; ok {
			return true
		}
	}
	return false
}

func buildPeriodStats(in StatsInput) ProjectStats {
	p := in.Project
	out := ProjectStats{}

	// Lifetime totals from the raw aggregates, independent of the schedule.
	for date, agg := range in.Aggregates {
		if date < p.StartDate || date > in.Today {
			continue
		}
		out.Totals.Checkins += agg.Count
		out.Totals.Value += agg.Value
		if agg.Count > 0 {
			out.Totals.ActiveDays++
		}
	}

	// Forward walk over every period from start to today. One pass yields the
	// current streak, the best streak and the done/missed/skipped counts:
	// completed periods extend the run, excused periods preserve it, missed
	// periods reset it, and the still-open current period does neither.
	run, best := 0, 0
	per := PeriodFor(p, p.StartDate)
	current := PeriodFor(p, in.Today)
	for i := 0; i < maxPeriodWalk && per.Start <= current.Start; i++ {
		if !ScheduledOn(p, per.Start) {
			per = NextPeriod(p, per)
			continue
		}
		value, _ := periodValue(in, per)
		excused := periodHasSkip(in, per)
		ended := per.End < in.Today
		// A day-granular period that is skipped today is already excused; a
		// week/month with a skip inside is only excused once it ends, because
		// it can still be completed.
		excusedNow := excused && (ended || (per.Start == per.End && per.End == in.Today))
		if p.Nature != NatureBad && len(in.Posts) > 0 {
			// A completed period commits its postrequisites on the day the
			// target was reached. Closed days accrue lifetime totals; today is
			// still pending and is reported on the summary instead.
			if day := periodCompletionDay(p, per, in.Today, in.Aggregates); day != "" && day < in.Today {
				unmet := unresolvedPostTargets(in.Posts, day)
				out.Totals.PostReqs += len(in.Posts)
				out.Totals.PostViolations += len(unmet)
				for _, target := range unmet {
					out.BrokenCommitments = append(out.BrokenCommitments, BrokenCommitment{Date: day, TargetID: target})
				}
			}
		}
		if p.Nature == NatureBad {
			// Inverted: a lapse spoils the period outright (even while open),
			// and only a fully survived, unexcused period extends the run. The
			// still-open clean period is neither done nor missed yet.
			switch {
			case value > 0:
				out.Totals.Missed++
				run = 0
			case excusedNow:
				out.Totals.Skipped++
			case ended:
				out.Totals.Done++
				run++
				best = max(best, run)
			}
		} else {
			switch {
			case value >= p.Target:
				out.Totals.Done++
				run++
				best = max(best, run)
			case excusedNow:
				out.Totals.Skipped++
			case ended:
				out.Totals.Missed++
				run = 0
			}
		}
		per = NextPeriod(p, per)
	}
	out.Totals.CompletionRate = rate(out.Totals.Done, out.Totals.Done+out.Totals.Missed)
	out.Totals.CommitmentRate = rate(out.Totals.PostReqs-out.Totals.PostViolations, out.Totals.PostReqs)

	// Summary for the current period.
	value, count := periodValue(in, current)
	todayAgg := in.Aggregates[in.Today]
	_, skippedToday := in.Skips[in.Today]
	status := StatusPending
	if p.Nature == NatureBad {
		// A clean period reads as done ("held") whether it has ended or is
		// still open, so the Today ring counts abstention as success. A lapse
		// is terminal and reads as missed immediately.
		switch {
		case value > 0:
			status = StatusMissed
		case !ScheduledOn(p, in.Today):
			status = StatusRest
		case (p.Schedule == ScheduleDaily || p.Schedule == ScheduleWeekdays) && skippedToday:
			status = StatusSkipped
		default:
			status = StatusDone
		}
	} else {
		switch {
		case value >= p.Target:
			status = StatusDone
		case !ScheduledOn(p, in.Today):
			status = StatusRest
		case p.Schedule == ScheduleDaily || p.Schedule == ScheduleWeekdays:
			if skippedToday {
				status = StatusSkipped
			} else if value > 0 {
				status = StatusPartial
			}
		case value > 0:
			status = StatusPartial
		}
	}
	out.Summary = Summary{
		Status:      status,
		PeriodStart: current.Start,
		PeriodEnd:   current.End,
		Value:       value,
		Target:      p.Target,
		TodayValue:  todayAgg.Value,
		Count:       count,
		Streak:      run,
		BestStreak:  best,
	}
	if p.Nature != NatureBad && len(in.Posts) > 0 {
		if day := periodCompletionDay(p, current, in.Today, in.Aggregates); day != "" {
			unmet := unresolvedPostTargets(in.Posts, day)
			switch {
			case day == in.Today:
				out.Summary.PendingPosts = linkIDs(unmet)
			case day < in.Today:
				out.Summary.BrokenPosts = linkIDs(unmet)
			}
		}
	}

	if in.Weeks > 0 {
		out.Heatmap = buildHeatmap(in)
		out.Periods = buildPeriodBars(in)
	}
	return out
}

// periodCompletionDay returns the first day inside per on which p's target was
// reached, or "" when it was not reached by today. Postrequisites trigger on
// this day only: a weekly goal of three does not demand its follow-up on every
// later day of the same week. Bad habits have no completion event.
func periodCompletionDay(p Project, per Period, today string, agg map[string]DayAggregate) string {
	if p.Nature == NatureBad || p.Target <= 0 {
		return ""
	}
	from := maxDate(per.Start, p.StartDate)
	to := minDate(per.End, today)
	var value float64
	for d := from; d <= to; d = AddDays(d, 1) {
		value += agg[d].Value
		if value >= p.Target {
			return d
		}
	}
	return ""
}

// unresolvedPostTargets reports which postrequisite targets were not resolved
// on day. Resolution reuses the prerequisite rule: archived, skipped and
// not-scheduled targets are resolved; a good target needs its period target,
// a bad target needs a clean day.
func unresolvedPostTargets(posts []LinkTarget, day string) []uuid.UUID {
	unmet := []uuid.UUID{}
	for _, target := range posts {
		if !PrereqSatisfied(target.Project, day, target.Agg, target.Skips) {
			unmet = append(unmet, target.Project.ID)
		}
	}
	return unmet
}

// linkIDs renders resolved link targets for the Summary's wire shape.
func linkIDs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func buildIntervalStats(in StatsInput) ProjectStats {
	p := in.Project
	out := ProjectStats{}
	interval := time.Duration(p.Interval * float64(time.Hour))

	for date, agg := range in.Aggregates {
		if date < p.StartDate || date > in.Today {
			continue
		}
		out.Totals.Checkins += agg.Count
		out.Totals.Value += agg.Value
		if agg.Count > 0 {
			out.Totals.ActiveDays++
		}
	}

	// Streaks count consecutive check-ins that arrived within the interval.
	// The first check-in always counts; a late one starts a new run.
	run, best, onTime, late := 0, 0, 0, 0
	var prev time.Time
	for i, c := range in.Checkins {
		if i == 0 {
			run = 1
			onTime = 1
		} else {
			gap := c.CheckedAt.Sub(prev)
			if gap <= interval {
				run++
				onTime++
			} else {
				run = 1
				late++
			}
		}
		best = max(best, run)
		prev = c.CheckedAt
	}
	out.Totals.OnTimeRate = rate(onTime, len(in.Checkins))
	out.Totals.OnTimeCount = onTime
	out.Totals.LateCount = late

	// Day-level verdicts for totals and heatmap: a day "done" met the expected
	// number of check-ins (24h / interval), a past empty day is missed.
	expectedPerDay := 24 / p.Interval
	doneTarget := int(math.Floor(expectedPerDay))
	if doneTarget < 1 {
		doneTarget = 1
	}
	var pendingPosts []string
	for d := p.StartDate; d <= in.Today; d = AddDays(d, 1) {
		if d > in.Today {
			break
		}
		if _, skipped := in.Skips[d]; skipped {
			out.Totals.Skipped++
			continue
		}
		agg := in.Aggregates[d]
		if p.Nature != NatureBad && len(in.Posts) > 0 && agg.Count > 0 {
			// Interval check-ins are events, not periods: every day with a
			// check-in commits its postrequisites on that same day.
			unmet := unresolvedPostTargets(in.Posts, d)
			switch {
			case d < in.Today:
				out.Totals.PostReqs += len(in.Posts)
				out.Totals.PostViolations += len(unmet)
				for _, target := range unmet {
					out.BrokenCommitments = append(out.BrokenCommitments, BrokenCommitment{Date: d, TargetID: target})
				}
			case d == in.Today:
				pendingPosts = linkIDs(unmet)
			}
		}
		switch {
		case agg.Count >= doneTarget:
			out.Totals.Done++
		case d == in.Today:
			// still open
		default:
			out.Totals.Missed++
		}
	}
	out.Totals.CompletionRate = rate(out.Totals.Done, out.Totals.Done+out.Totals.Missed)
	out.Totals.CommitmentRate = rate(out.Totals.PostReqs-out.Totals.PostViolations, out.Totals.PostReqs)

	var dueAt time.Time
	var lastAt *time.Time
	if n := len(in.Checkins); n > 0 {
		last := in.Checkins[n-1].CheckedAt
		lastAt = &last
		dueAt = last.Add(interval)
	} else {
		dueAt = p.CreatedAt.Add(interval)
	}
	status := StatusWaiting
	if !in.Now.Before(dueAt) {
		status = StatusDue
	}
	todayAgg := in.Aggregates[in.Today]
	out.Summary = Summary{
		Status:        status,
		PeriodStart:   in.Today,
		PeriodEnd:     in.Today,
		Value:         todayAgg.Value,
		Target:        0,
		TodayValue:    todayAgg.Value,
		Count:         todayAgg.Count,
		Streak:        run,
		BestStreak:    best,
		DueAt:         &dueAt,
		LastCheckinAt: lastAt,
		PendingPosts:  pendingPosts,
	}

	if in.Weeks > 0 {
		out.Heatmap = buildIntervalHeatmap(in, doneTarget)
		out.Periods = buildIntervalBars(in, expectedPerDay)
	}
	return out
}

// buildHeatmap renders weeks*7 days ending today, aligned to whole weeks.
func buildHeatmap(in StatsInput) []HeatCell {
	p := in.Project
	start := AddDays(WeekStart(in.Today), -(in.Weeks-1)*7)
	cells := make([]HeatCell, 0, in.Weeks*7)
	for d := start; d <= in.Today; d = AddDays(d, 1) {
		agg := in.Aggregates[d]
		cell := HeatCell{Date: d, Value: agg.Value, Count: agg.Count, Status: StatusNone}
		_, skipped := in.Skips[d]
		if p.Nature == NatureBad {
			// Day-level abstention calendar, uniform across schedules: a lapse
			// marks the day (×), a clean day fills with the ink, rest and excuse
			// keep their shapes. The period verdict still lives in the bars.
			switch {
			case d < p.StartDate:
			case !ScheduledOn(p, d):
				cell.Status = StatusRest
			case agg.Value > 0:
				cell.Status = StatusMissed
			case skipped:
				cell.Status = StatusSkipped
			default:
				cell.Status = StatusDone
			}
			cells = append(cells, cell)
			continue
		}
		switch {
		case d < p.StartDate:
			// before the project existed
		case skipped && agg.Value < p.Target:
			cell.Status = StatusSkipped
		case !ScheduledOn(p, d):
			cell.Status = StatusRest
		case p.Schedule == ScheduleDaily || p.Schedule == ScheduleWeekdays:
			switch {
			case agg.Value >= p.Target:
				cell.Status = StatusDone
			case d == in.Today:
				if agg.Value > 0 {
					cell.Status = StatusPartial
				} else {
					cell.Status = StatusPending
				}
			default:
				cell.Status = StatusMissed
			}
		default:
			// week/month/window: the day shows contribution; the period verdict
			// lives in the bars, so empty past days stay neutral.
			per := PeriodFor(p, d)
			value, _ := periodValue(in, per)
			switch {
			case agg.Value >= p.Target:
				cell.Status = StatusDone
			case agg.Value > 0:
				if value >= p.Target {
					cell.Status = StatusDone
				} else {
					cell.Status = StatusPartial
				}
			case d == in.Today:
				cell.Status = StatusPending
			}
		}
		cells = append(cells, cell)
	}
	return cells
}

func buildIntervalHeatmap(in StatsInput, doneTarget int) []HeatCell {
	p := in.Project
	start := AddDays(WeekStart(in.Today), -(in.Weeks-1)*7)
	cells := make([]HeatCell, 0, in.Weeks*7)
	for d := start; d <= in.Today; d = AddDays(d, 1) {
		agg := in.Aggregates[d]
		cell := HeatCell{Date: d, Value: agg.Value, Count: agg.Count, Status: StatusNone}
		_, skipped := in.Skips[d]
		switch {
		case d < p.StartDate:
		case skipped:
			cell.Status = StatusSkipped
		case agg.Count >= doneTarget:
			cell.Status = StatusDone
		case agg.Count > 0:
			cell.Status = StatusPartial
		case d == in.Today:
			cell.Status = StatusPending
		default:
			cell.Status = StatusMissed
		}
		cells = append(cells, cell)
	}
	return cells
}

// barCount is how many periods the trend chart shows, per schedule.
func barCount(s ScheduleType) int {
	switch s {
	case ScheduleWeekly, ScheduleIntervalDays:
		return 8
	case ScheduleMonthly:
		return 6
	default:
		return 14
	}
}

func buildPeriodBars(in StatsInput) []PeriodBar {
	p := in.Project
	n := barCount(p.Schedule)
	current := PeriodFor(p, in.Today)
	firstStart := maxDate(p.StartDate, periodShiftBack(p, current.Start, n-1))
	per := PeriodFor(p, firstStart)
	bars := make([]PeriodBar, 0, n)
	for i := 0; i < n && per.Start <= current.Start; i++ {
		value, _ := periodValue(in, per)
		excused := periodHasSkip(in, per)
		status := StatusPending
		target := p.Target
		if p.Nature == NatureBad {
			// No goal line: the bar's height is the lapse count, and a clean
			// period (open or ended) reads as done.
			target = 0
			switch {
			case !ScheduledOn(p, per.Start) && p.Schedule == ScheduleWeekdays:
				status = StatusRest
			case value > 0:
				status = StatusMissed
			case per.End < in.Today && excused:
				status = StatusSkipped
			default:
				status = StatusDone
			}
		} else {
			switch {
			case !ScheduledOn(p, per.Start) && p.Schedule == ScheduleWeekdays:
				status = StatusRest
			case value >= p.Target:
				status = StatusDone
			case per.End < in.Today && excused:
				status = StatusSkipped
			case per.End < in.Today:
				status = StatusMissed
			case value > 0:
				status = StatusPartial
			}
		}
		bars = append(bars, PeriodBar{
			Key: per.Key, Start: per.Start, End: per.End,
			Value: value, Target: target, Status: status,
		})
		per = NextPeriod(p, per)
	}
	return bars
}

func buildIntervalBars(in StatsInput, expectedPerDay float64) []PeriodBar {
	n := 14
	bars := make([]PeriodBar, 0, n)
	for i := n - 1; i >= 0; i-- {
		d := AddDays(in.Today, -i)
		agg := in.Aggregates[d]
		status := StatusPending
		_, skipped := in.Skips[d]
		switch {
		case d < in.Project.StartDate:
			status = StatusNone
		case skipped:
			status = StatusSkipped
		case float64(agg.Count) >= expectedPerDay:
			status = StatusDone
		case agg.Count > 0:
			status = StatusPartial
		case d < in.Today:
			status = StatusMissed
		}
		bars = append(bars, PeriodBar{
			Key: d, Start: d, End: d,
			Value: float64(agg.Count), Target: expectedPerDay, Status: status,
		})
	}
	return bars
}

func rate(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

// PrereqSatisfied reports whether project p counts as "resolved" on a given
// day, for the purpose of gating a dependent project. Resolved means: done,
// excused (skip), not scheduled (rest), or retired (archived). A pending or
// partial day is NOT resolved, so checking the dependent project then is a
// prerequisite violation. For a BAD prerequisite, resolved means the opposite
// of a lapse: the day is clean only when nothing was recorded. Pure: aggregates
// and skips are supplied by caller.
func PrereqSatisfied(p Project, day string, agg map[string]DayAggregate, skips map[string]string) bool {
	if p.Archived {
		return true
	}
	if _, skipped := skips[day]; skipped {
		return true
	}
	if !ScheduledOn(p, day) {
		return true
	}
	if p.Schedule == ScheduleIntervalHours {
		expected := int(math.Floor(24 / p.Interval))
		if expected < 1 {
			expected = 1
		}
		return agg[day].Count >= expected
	}
	per := PeriodFor(p, day)
	from := maxDate(per.Start, p.StartDate)
	to := minDate(per.End, day)
	var value float64
	for d := from; d <= to; d = AddDays(d, 1) {
		value += agg[d].Value
	}
	if p.Nature == NatureBad {
		return value == 0
	}
	return value >= p.Target
}
