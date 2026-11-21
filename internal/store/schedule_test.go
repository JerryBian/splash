package store

import (
	"strconv"
	"testing"
	"time"
)

// The tests share one "today": Saturday 2026-11-21. The Monday of that week
// is 2026-11-16, which makes week and weekday expectations easy to read.
const testToday = "2026-11-21"

var testNow = time.Date(2026, 11, 21, 13, 0, 0, 0, time.UTC)

func agg(days map[string]float64) map[string]DayAggregate {
	out := map[string]DayAggregate{}
	for d, v := range days {
		out[d] = DayAggregate{Count: 1, Value: v}
	}
	return out
}

func dailyProject(start string, target float64) Project {
	return Project{Schedule: ScheduleDaily, Target: target, Unit: "times", StartDate: start,
		CreatedAt: testNow.Add(-24 * time.Hour)}
}

func TestDateHelpers(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"weekday of saturday", strconv.Itoa(WeekdayOf("2026-11-21")), "6"},
		{"weekday of sunday", strconv.Itoa(WeekdayOf("2026-11-22")), "7"},
		{"weekday of monday", strconv.Itoa(WeekdayOf("2026-11-16")), "1"},
		{"week start", WeekStart("2026-11-21"), "2026-11-16"},
		{"week start on monday", WeekStart("2026-11-16"), "2026-11-16"},
		{"week start on sunday", WeekStart("2026-11-22"), "2026-11-16"},
		{"month start", MonthStart("2026-11-21"), "2026-11-01"},
		{"month end", MonthEnd("2026-11-21"), "2026-11-30"},
		{"month end february", MonthEnd("2026-02-10"), "2026-02-28"},
		{"add days across month", AddDays("2026-11-30", 1), "2026-12-01"},
		{"add negative days", AddDays("2026-11-01", -1), "2026-10-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
	if d := DaysBetween("2026-11-16", "2026-11-21"); d != 5 {
		t.Errorf("DaysBetween = %d, want 5", d)
	}
}

func TestPeriodFor(t *testing.T) {
	weekly := Project{Schedule: ScheduleWeekly, StartDate: "2026-11-02"}
	if per := PeriodFor(weekly, "2026-11-21"); per.Start != "2026-11-16" || per.End != "2026-11-22" {
		t.Errorf("weekly period = %+v", per)
	}
	monthly := Project{Schedule: ScheduleMonthly, StartDate: "2026-10-01"}
	if per := PeriodFor(monthly, "2026-11-21"); per.Start != "2026-11-01" || per.End != "2026-11-30" || per.Key != "2026-11" {
		t.Errorf("monthly period = %+v", per)
	}
	window := Project{Schedule: ScheduleIntervalDays, Interval: 3, StartDate: "2026-11-16"}
	if per := PeriodFor(window, "2026-11-21"); per.Start != "2026-11-19" || per.End != "2026-11-21" {
		t.Errorf("interval_days period = %+v", per)
	}
	// Before the start date clamps to the first window instead of going negative.
	if per := PeriodFor(window, "2026-11-10"); per.Start != "2026-11-16" {
		t.Errorf("pre-start period = %+v", per)
	}
}

func TestDailyStreaksAndMisses(t *testing.T) {
	tests := []struct {
		name       string
		start      string
		checkins   map[string]DayAggregate
		skips      map[string]string
		wantStatus string
		wantStreak int
		wantBest   int
		wantDone   int
		wantMissed int
		wantSkip   int
	}{
		{
			name: "three in a row, today open", start: "2026-11-15",
			checkins:   agg(map[string]float64{"2026-11-18": 1, "2026-11-19": 1, "2026-11-20": 1}),
			wantStatus: StatusPending, wantStreak: 3, wantBest: 3, wantDone: 3, wantMissed: 3,
		},
		{
			name: "today done extends the streak", start: "2026-11-19",
			checkins:   agg(map[string]float64{"2026-11-19": 1, "2026-11-20": 1, "2026-11-21": 1}),
			wantStatus: StatusDone, wantStreak: 3, wantBest: 3, wantDone: 3,
		},
		{
			name: "a miss resets the run", start: "2026-11-18",
			checkins:   agg(map[string]float64{"2026-11-18": 1, "2026-11-20": 1}),
			wantStatus: StatusPending, wantStreak: 1, wantBest: 1, wantDone: 2, wantMissed: 1,
		},
		{
			name: "a skip preserves the run without extending it", start: "2026-11-17",
			checkins:   agg(map[string]float64{"2026-11-18": 1, "2026-11-19": 1, "2026-11-20": 1}),
			skips:      map[string]string{"2026-11-17": "rest"},
			wantStatus: StatusPending, wantStreak: 3, wantBest: 3, wantDone: 3, wantSkip: 1,
		},
		{
			name: "skipping today excuses today", start: "2026-11-20",
			checkins:   agg(map[string]float64{"2026-11-20": 1}),
			skips:      map[string]string{"2026-11-21": "sick"},
			wantStatus: StatusSkipped, wantStreak: 1, wantBest: 1, wantDone: 1, wantSkip: 1,
		},
		{
			name: "best streak survives a later miss", start: "2026-11-15",
			checkins:   agg(map[string]float64{"2026-11-15": 1, "2026-11-16": 1, "2026-11-17": 1, "2026-11-19": 1, "2026-11-20": 1}),
			wantStatus: StatusPending, wantStreak: 2, wantBest: 3, wantDone: 5, wantMissed: 1,
		},
		{
			name: "backfill repairs a broken streak", start: "2026-11-19",
			checkins:   agg(map[string]float64{"2026-11-19": 1, "2026-11-20": 1, "2026-11-21": 1}),
			wantStatus: StatusDone, wantStreak: 3, wantBest: 3, wantDone: 3,
		},
		{
			name: "fresh project", start: testToday,
			wantStatus: StatusPending, wantStreak: 0, wantBest: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := dailyProject(tt.start, 1)
			got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow,
				Aggregates: tt.checkins, Skips: tt.skips})
			if got.Summary.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Summary.Status, tt.wantStatus)
			}
			if got.Summary.Streak != tt.wantStreak {
				t.Errorf("streak = %d, want %d", got.Summary.Streak, tt.wantStreak)
			}
			if got.Summary.BestStreak != tt.wantBest {
				t.Errorf("best = %d, want %d", got.Summary.BestStreak, tt.wantBest)
			}
			if got.Totals.Done != tt.wantDone || got.Totals.Missed != tt.wantMissed || got.Totals.Skipped != tt.wantSkip {
				t.Errorf("done/missed/skipped = %d/%d/%d, want %d/%d/%d",
					got.Totals.Done, got.Totals.Missed, got.Totals.Skipped, tt.wantDone, tt.wantMissed, tt.wantSkip)
			}
		})
	}
}

func TestDailyValueTarget(t *testing.T) {
	p := dailyProject("2026-11-19", 8)
	p.Unit = "cups"
	in := StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: map[string]DayAggregate{
		"2026-11-19": {Count: 3, Value: 8},
		"2026-11-20": {Count: 2, Value: 6}, // under target: missed
		"2026-11-21": {Count: 2, Value: 5}, // today, partial
	}}
	got := BuildStats(in)
	if got.Summary.Status != StatusPartial {
		t.Errorf("status = %q, want partial", got.Summary.Status)
	}
	if got.Summary.Value != 5 || got.Summary.Target != 8 {
		t.Errorf("progress = %v/%v, want 5/8", got.Summary.Value, got.Summary.Target)
	}
	if got.Summary.Streak != 0 {
		t.Errorf("streak = %d, want 0 (yesterday missed)", got.Summary.Streak)
	}
	if got.Totals.Done != 1 || got.Totals.Missed != 1 {
		t.Errorf("done/missed = %d/%d, want 1/1", got.Totals.Done, got.Totals.Missed)
	}
	if got.Totals.Checkins != 7 {
		t.Errorf("checkins = %d, want 7", got.Totals.Checkins)
	}
}

func TestWeeklySchedule(t *testing.T) {
	p := Project{Schedule: ScheduleWeekly, Target: 3, Unit: "times", StartDate: "2026-11-02"}
	in := StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: agg(map[string]float64{
		"2026-11-03": 1, "2026-11-05": 1, // week 11-02: 2/3 → missed
		"2026-11-09": 1, "2026-11-11": 1, "2026-11-13": 1, // week 11-09: done
		"2026-11-17": 1, // week 11-16 (current): 1/3
	})}
	got := BuildStats(in)
	if got.Summary.Status != StatusPartial {
		t.Errorf("status = %q, want partial", got.Summary.Status)
	}
	if got.Summary.Value != 1 || got.Summary.Target != 3 {
		t.Errorf("progress = %v/%v, want 1/3", got.Summary.Value, got.Summary.Target)
	}
	if got.Summary.PeriodStart != "2026-11-16" || got.Summary.PeriodEnd != "2026-11-22" {
		t.Errorf("period = %s..%s", got.Summary.PeriodStart, got.Summary.PeriodEnd)
	}
	if got.Summary.Streak != 1 || got.Summary.BestStreak != 1 {
		t.Errorf("streak/best = %d/%d, want 1/1", got.Summary.Streak, got.Summary.BestStreak)
	}
	if got.Totals.Done != 1 || got.Totals.Missed != 1 {
		t.Errorf("done/missed = %d/%d, want 1/1", got.Totals.Done, got.Totals.Missed)
	}

	// A skip inside an ended, incomplete week excuses the whole week.
	in.Skips = map[string]string{"2026-11-04": "trip"}
	got = BuildStats(in)
	if got.Totals.Missed != 0 || got.Totals.Skipped != 1 {
		t.Errorf("with skip: missed/skipped = %d/%d, want 0/1", got.Totals.Missed, got.Totals.Skipped)
	}
	if got.Summary.Streak != 1 {
		t.Errorf("excused week broke the streak: %d", got.Summary.Streak)
	}
}

func TestMonthlySchedule(t *testing.T) {
	p := Project{Schedule: ScheduleMonthly, Target: 4, Unit: "times", StartDate: "2026-10-01"}
	got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: agg(map[string]float64{
		"2026-10-05": 1, "2026-10-10": 1, "2026-10-15": 1, "2026-10-20": 1, // October done
		"2026-11-05": 1, "2026-11-10": 1, // November 2/4, open
	})})
	if got.Summary.Streak != 1 || got.Summary.BestStreak != 1 {
		t.Errorf("streak/best = %d/%d, want 1/1", got.Summary.Streak, got.Summary.BestStreak)
	}
	if got.Summary.Status != StatusPartial || got.Summary.Value != 2 {
		t.Errorf("status/value = %s/%v, want partial/2", got.Summary.Status, got.Summary.Value)
	}
	if got.Totals.Done != 1 || got.Totals.Missed != 0 {
		t.Errorf("done/missed = %d/%d, want 1/0", got.Totals.Done, got.Totals.Missed)
	}
}

func TestWeekdaysSchedule(t *testing.T) {
	p := Project{Schedule: ScheduleWeekdays, Target: 1, Unit: "times",
		Weekdays: []int{1, 3, 5}, StartDate: "2026-11-16"}
	got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: agg(map[string]float64{
		"2026-11-16": 1, "2026-11-18": 1, "2026-11-20": 1,
	})})
	// Today is Saturday: a rest day, not a miss, and the streak stands at 3.
	if got.Summary.Status != StatusRest {
		t.Errorf("status = %q, want rest", got.Summary.Status)
	}
	if got.Summary.Streak != 3 {
		t.Errorf("streak = %d, want 3", got.Summary.Streak)
	}
	if got.Totals.Missed != 0 {
		t.Errorf("missed = %d, want 0 (rest days never miss)", got.Totals.Missed)
	}

	// Missing Monday breaks the run; Wednesday starts a new one.
	got = BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: agg(map[string]float64{
		"2026-11-18": 1, "2026-11-20": 1,
	})})
	if got.Summary.Streak != 2 || got.Totals.Missed != 1 {
		t.Errorf("streak/missed = %d/%d, want 2/1", got.Summary.Streak, got.Totals.Missed)
	}
}

func TestIntervalDaysSchedule(t *testing.T) {
	p := Project{Schedule: ScheduleIntervalDays, Interval: 3, Target: 1, Unit: "times", StartDate: "2026-11-16"}
	got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow, Aggregates: agg(map[string]float64{
		"2026-11-17": 1, // window 16..18 done
	})})
	if got.Summary.Streak != 1 {
		t.Errorf("streak = %d, want 1", got.Summary.Streak)
	}
	if got.Summary.PeriodStart != "2026-11-19" || got.Summary.PeriodEnd != "2026-11-21" {
		t.Errorf("current window = %s..%s", got.Summary.PeriodStart, got.Summary.PeriodEnd)
	}
	if got.Summary.Status != StatusPending {
		t.Errorf("status = %q, want pending", got.Summary.Status)
	}
}

func TestIntervalHoursSchedule(t *testing.T) {
	p := Project{Schedule: ScheduleIntervalHours, Interval: 2, StartDate: "2026-11-20",
		CreatedAt: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)}
	at := func(day string, hour int) time.Time {
		d, _ := ParseDate(day)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.UTC)
	}
	checkins := []Checkin{
		{CheckedAt: at("2026-11-21", 8), LocalDate: "2026-11-21", Value: 1},
		{CheckedAt: at("2026-11-21", 10), LocalDate: "2026-11-21", Value: 1},
		{CheckedAt: at("2026-11-21", 12), LocalDate: "2026-11-21", Value: 1},
	}
	in := StatsInput{Project: p, Today: testToday, Now: testNow, Checkins: checkins,
		Aggregates: map[string]DayAggregate{"2026-11-21": {Count: 3, Value: 3}}}

	got := BuildStats(in)
	if got.Summary.Status != StatusWaiting {
		t.Errorf("status at 13:00 = %q, want waiting (due 14:00)", got.Summary.Status)
	}
	if got.Summary.DueAt == nil || got.Summary.DueAt.Hour() != 14 {
		t.Errorf("dueAt = %v, want 14:00", got.Summary.DueAt)
	}
	if got.Summary.Streak != 3 || got.Summary.BestStreak != 3 {
		t.Errorf("streak/best = %d/%d, want 3/3", got.Summary.Streak, got.Summary.BestStreak)
	}
	if got.Totals.OnTimeRate != 1 {
		t.Errorf("onTimeRate = %v, want 1", got.Totals.OnTimeRate)
	}

	// 14:30: the countdown has elapsed.
	in.Now = at("2026-11-21", 14).Add(30 * time.Minute)
	got = BuildStats(in)
	if got.Summary.Status != StatusDue {
		t.Errorf("status at 14:30 = %q, want due", got.Summary.Status)
	}

	// A late check-in resets the run but not the best.
	in.Checkins = append(checkins, Checkin{CheckedAt: at("2026-11-21", 17), LocalDate: "2026-11-21", Value: 1})
	in.Now = at("2026-11-21", 18)
	got = BuildStats(in)
	if got.Summary.Streak != 1 || got.Summary.BestStreak != 3 {
		t.Errorf("after late check-in streak/best = %d/%d, want 1/3", got.Summary.Streak, got.Summary.BestStreak)
	}
	if got.Totals.LateCount != 1 {
		t.Errorf("lateCount = %d, want 1", got.Totals.LateCount)
	}

	// No check-ins yet: the countdown anchors at creation, not at epoch zero.
	fresh := BuildStats(StatsInput{Project: p, Today: "2026-11-20",
		Now: at("2026-11-20", 9), Aggregates: map[string]DayAggregate{}})
	if fresh.Summary.Status != StatusWaiting || fresh.Summary.DueAt.Hour() != 10 {
		t.Errorf("fresh project: status %s dueAt %v, want waiting/10:00", fresh.Summary.Status, fresh.Summary.DueAt)
	}
}

func TestHeatmapStatuses(t *testing.T) {
	p := dailyProject("2026-11-15", 1)
	got := BuildStats(StatsInput{
		Project: p, Today: testToday, Now: testNow, Weeks: 2,
		Aggregates: agg(map[string]float64{"2026-11-18": 1, "2026-11-21": 1}),
		Skips:      map[string]string{"2026-11-17": "rest"},
	})
	byDate := map[string]HeatCell{}
	for _, c := range got.Heatmap {
		byDate[c.Date] = c
	}
	want := map[string]string{
		"2026-11-14": StatusNone,    // before start
		"2026-11-15": StatusMissed,  // past, empty
		"2026-11-17": StatusSkipped, // excused
		"2026-11-18": StatusDone,
		"2026-11-20": StatusMissed,
		"2026-11-21": StatusDone, // today, completed
	}
	for date, status := range want {
		if cell, ok := byDate[date]; !ok {
			t.Errorf("heatmap missing %s", date)
		} else if cell.Status != status {
			t.Errorf("heatmap[%s] = %q, want %q", date, cell.Status, status)
		}
	}
	// Two weeks ending today, aligned to Monday: 11-09 .. 11-21 = 13 cells.
	if len(got.Heatmap) != 13 {
		t.Errorf("heatmap length = %d, want 13", len(got.Heatmap))
	}
}

func TestPeriodBars(t *testing.T) {
	p := Project{Schedule: ScheduleWeekly, Target: 3, StartDate: "2026-09-28"}
	got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow, Weeks: 26,
		Aggregates: agg(map[string]float64{"2026-11-17": 1, "2026-11-18": 1, "2026-11-19": 1})})
	if len(got.Periods) != 8 {
		t.Fatalf("bars = %d, want 8", len(got.Periods))
	}
	last := got.Periods[len(got.Periods)-1]
	if last.Start != "2026-11-16" || last.Value != 3 || last.Status != StatusDone {
		t.Errorf("last bar = %+v, want current week done with 3", last)
	}
}

func TestCompletionRate(t *testing.T) {
	p := dailyProject("2026-11-17", 1)
	got := BuildStats(StatsInput{Project: p, Today: testToday, Now: testNow,
		Aggregates: agg(map[string]float64{"2026-11-17": 1, "2026-11-18": 1, "2026-11-20": 1})})
	// 17, 18, 20 done; 19 missed; 21 open → 3/4.
	if got.Totals.CompletionRate != 0.75 {
		t.Errorf("completionRate = %v, want 0.75", got.Totals.CompletionRate)
	}
}
