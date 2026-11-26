package store

import (
	"context"
	"testing"
	"uuid"
)

func TestPrereqSatisfiedPure(t *testing.T) {
	p := Project{Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"}
	agg := map[string]DayAggregate{"2026-11-20": {Count: 1, Value: 1}}
	skips := map[string]string{"2026-11-19": "rest"}

	if !PrereqSatisfied(p, "2026-11-20", agg, nil) {
		t.Error("a completed day must satisfy the gate")
	}
	if PrereqSatisfied(p, "2026-11-21", agg, nil) {
		t.Error("an empty day must not satisfy the gate")
	}
	if !PrereqSatisfied(p, "2026-11-19", nil, skips) {
		t.Error("an excused (skipped) day satisfies the gate")
	}
	rest := Project{Schedule: ScheduleWeekdays, Target: 1, Weekdays: []int{1, 3, 5}, StartDate: "2026-11-01"}
	if !PrereqSatisfied(rest, "2026-11-21", nil, nil) { // Saturday = rest
		t.Error("a rest day satisfies the gate (no obligation)")
	}
	archived := p
	archived.Archived = true
	if !PrereqSatisfied(archived, "2026-11-21", nil, nil) {
		t.Error("a retired project satisfies the gate")
	}
}

func TestPrerequisiteGraphValidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	a := mustProject(t, db, ProjectInput{Name: "Read", Schedule: ScheduleDaily, Mode: ModeCheck, Target: 1, StartDate: "2026-11-01"})
	b := mustProject(t, db, ProjectInput{Name: "Videos", Schedule: ScheduleDaily, Mode: ModeCheck, Target: 1, StartDate: "2026-11-01"})

	// Wire b -> a.
	if _, err := db.Projects().Update(ctx, b.ID, ProjectPatch{Prerequisites: &[]string{a.ID.String()}}, nil); err != nil {
		t.Fatalf("wire prerequisite: %v", err)
	}
	got, _ := db.Projects().Get(ctx, b.ID)
	if len(got.Prerequisites) != 1 || got.Prerequisites[0] != a.ID {
		t.Fatalf("prerequisites = %v", got.Prerequisites)
	}

	// Cycle a -> b must be rejected.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Prerequisites: &[]string{b.ID.String()}}, nil); err == nil {
		t.Error("cycle accepted")
	}
	// Self reference rejected.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Prerequisites: &[]string{a.ID.String()}}, nil); err == nil {
		t.Error("self prerequisite accepted")
	}
	// Nonexistent rejected.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Prerequisites: &[]string{uuid.NewV7().String()}}, nil); err == nil {
		t.Error("nonexistent prerequisite accepted")
	}
	// Clearing works.
	empty := []string{}
	if _, err := db.Projects().Update(ctx, b.ID, ProjectPatch{Prerequisites: &empty}, nil); err != nil {
		t.Fatalf("clear prerequisites: %v", err)
	}
	got, _ = db.Projects().Get(ctx, b.ID)
	if len(got.Prerequisites) != 0 {
		t.Errorf("prerequisites not cleared: %v", got.Prerequisites)
	}
}

func TestBackfillFlagAndCheckMode(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Read", Schedule: ScheduleDaily, Mode: ModeCheck, Target: 30, Unit: "minutes", StartDate: "2026-11-01"})
	// Check mode forces target to 1 regardless of the submitted 30.
	if p.Target != 1 || p.Mode != ModeCheck {
		t.Fatalf("check mode not normalized: target=%v mode=%s", p.Target, p.Mode)
	}

	today := db.Today()
	onTime, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 5})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if onTime.Backfilled {
		t.Error("same-day check-in flagged as backfill")
	}
	if onTime.Value != 1 {
		t.Errorf("check mode value = %v, want 1", onTime.Value)
	}

	back, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Date: AddDays(today, -2)})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if !back.Backfilled {
		t.Error("past-day check-in not flagged as backfill")
	}

	var n int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM checkins WHERE project_id=? AND backfilled=1`, p.ID.String()).Scan(&n); err != nil || n != 1 {
		t.Errorf("backfilled count = %d (err %v), want 1", n, err)
	}
}

func TestViolationDerivedAndClearedByBackfill(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	read := mustProject(t, db, ProjectInput{Name: "Read", Schedule: ScheduleDaily, Mode: ModeCheck, Target: 1, StartDate: "2026-11-01"})
	videos := mustProject(t, db, ProjectInput{Name: "Videos", Schedule: ScheduleDaily, Mode: ModeCheck, Target: 1, StartDate: "2026-11-01"})
	if _, err := db.Projects().Update(ctx, videos.ID, ProjectPatch{Prerequisites: &[]string{read.ID.String()}}, nil); err != nil {
		t.Fatalf("wire: %v", err)
	}

	day := AddDays(db.Today(), -1)
	// Watch videos without reading that day -> violation.
	c, err := db.Checkins().Create(ctx, videos.ID, CheckinInput{Value: 1, Date: day})
	if err != nil {
		t.Fatalf("create videos: %v", err)
	}
	if !c.Violated || len(c.Unmet) != 1 || c.Unmet[0] != read.ID {
		t.Fatalf("expected violation unmet=[read], got violated=%v unmet=%v", c.Violated, c.Unmet)
	}

	// Listing also carries the derived verdict.
	list, err := db.Checkins().List(ctx, CheckinQuery{ProjectID: videos.ID})
	if err != nil || len(list) != 1 || !list[0].Violated {
		t.Fatalf("list violation = %v (err %v)", list, err)
	}

	// Backfill reading for that day -> violation clears on next read.
	if _, err := db.Checkins().Create(ctx, read.ID, CheckinInput{Value: 1, Date: day}); err != nil {
		t.Fatalf("backfill read: %v", err)
	}
	list, err = db.Checkins().List(ctx, CheckinQuery{ProjectID: videos.ID})
	if err != nil || len(list) != 1 || list[0].Violated {
		t.Fatalf("violation should clear after backfill, got %v (err %v)", list, err)
	}

	// Project totals count violations over history.
	stats, err := db.Stats().ForProject(ctx, videos, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Totals.Violations != 0 {
		t.Errorf("violations after backfill = %d, want 0", stats.Totals.Violations)
	}
}

func TestPostrequisiteCommitments(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	run := mustProject(t, db, ProjectInput{Name: "Run", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	sleep := mustProject(t, db, ProjectInput{Name: "Sleep", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	updated, err := db.Projects().Update(ctx, run.ID, ProjectPatch{Postrequisites: &[]string{sleep.ID.String()}}, nil)
	if err != nil {
		t.Fatalf("wire postrequisite: %v", err)
	}
	if len(updated.Postrequisites) != 1 || updated.Postrequisites[0] != sleep.ID {
		t.Fatalf("postrequisites = %v", updated.Postrequisites)
	}

	yesterday := AddDays(db.Today(), -1)
	if _, err := db.Checkins().Create(ctx, run.ID, CheckinInput{Value: 1, Date: yesterday}); err != nil {
		t.Fatalf("run checkin: %v", err)
	}
	stats, err := db.Stats().ForProject(ctx, updated, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Totals.PostReqs != 1 || stats.Totals.PostViolations != 1 || stats.Totals.CommitmentRate != 0 {
		t.Errorf("commitment totals = reqs %d violations %d rate %v, want 1/1/0",
			stats.Totals.PostReqs, stats.Totals.PostViolations, stats.Totals.CommitmentRate)
	}
	if len(stats.BrokenCommitments) != 1 || stats.BrokenCommitments[0].Date != yesterday || stats.BrokenCommitments[0].TargetID != sleep.ID {
		t.Errorf("broken commitments = %+v", stats.BrokenCommitments)
	}
	list, err := db.Checkins().List(ctx, CheckinQuery{ProjectID: run.ID})
	if err != nil || len(list) != 1 || len(list[0].PostUnmet) != 1 || list[0].PostUnmet[0] != sleep.ID || len(list[0].PostPending) != 0 {
		t.Fatalf("run checkin post verdicts = %+v, err = %v", list, err)
	}

	// Backfilling the postrequisite on the same day clears the commitment.
	if _, err := db.Checkins().Create(ctx, sleep.ID, CheckinInput{Value: 1, Date: yesterday}); err != nil {
		t.Fatalf("sleep checkin: %v", err)
	}
	stats, err = db.Stats().ForProject(ctx, updated, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats after backfill: %v", err)
	}
	if stats.Totals.PostViolations != 0 || stats.Totals.CommitmentRate != 1 || len(stats.BrokenCommitments) != 0 {
		t.Errorf("after backfill = %+v", stats.Totals)
	}
	list, err = db.Checkins().List(ctx, CheckinQuery{ProjectID: run.ID})
	if err != nil || len(list[0].PostUnmet) != 0 {
		t.Errorf("post verdicts after backfill = %+v, err = %v", list, err)
	}

	// Today is still open: the same commitment is pending, not a violation.
	if _, err := db.Checkins().Create(ctx, run.ID, CheckinInput{Value: 1}); err != nil {
		t.Fatalf("today run: %v", err)
	}
	stats, err = db.Stats().ForProject(ctx, updated, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats today: %v", err)
	}
	if len(stats.Summary.PendingPosts) != 1 || stats.Summary.PendingPosts[0] != sleep.ID.String() {
		t.Errorf("pending posts = %v, want [%s]", stats.Summary.PendingPosts, sleep.ID)
	}
	if stats.Totals.PostReqs != 1 || stats.Totals.PostViolations != 0 {
		t.Errorf("today must not accrue closed totals: %+v", stats.Totals)
	}
}

func TestPostrequisiteRespectsSkipsAndBadTargets(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	run := mustProject(t, db, ProjectInput{Name: "Run", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	noLate := mustProject(t, db, ProjectInput{Name: "No late night", Nature: NatureBad, Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	run, err := db.Projects().Update(ctx, run.ID, ProjectPatch{Postrequisites: &[]string{noLate.ID.String()}}, nil)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	day := AddDays(db.Today(), -1)

	// A clean bad-habit day satisfies the follow-up.
	if _, err := db.Checkins().Create(ctx, run.ID, CheckinInput{Value: 1, Date: day}); err != nil {
		t.Fatalf("run: %v", err)
	}
	stats, err := db.Stats().ForProject(ctx, run, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats clean: %v", err)
	}
	if stats.Totals.PostViolations != 0 {
		t.Errorf("a clean bad target must satisfy the postrequisite: %+v", stats.Totals)
	}

	// A lapse on that day breaks it.
	if _, err := db.Checkins().Create(ctx, noLate.ID, CheckinInput{Value: 1, Date: day}); err != nil {
		t.Fatalf("lapse: %v", err)
	}
	stats, err = db.Stats().ForProject(ctx, run, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats lapse: %v", err)
	}
	if stats.Totals.PostViolations != 1 || len(stats.BrokenCommitments) != 1 || stats.BrokenCommitments[0].TargetID != noLate.ID {
		t.Errorf("a lapse must break the commitment: %+v / %+v", stats.BrokenCommitments, stats.Totals)
	}

	// A skipped target day is excused.
	skipDay := AddDays(db.Today(), -2)
	if _, err := db.Checkins().Create(ctx, run.ID, CheckinInput{Value: 1, Date: skipDay}); err != nil {
		t.Fatalf("run skip day: %v", err)
	}
	if _, err := db.Skips().Create(ctx, noLate.ID, SkipInput{Date: skipDay}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	stats, err = db.Stats().ForProject(ctx, run, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats skip: %v", err)
	}
	for _, broken := range stats.BrokenCommitments {
		if broken.Date == skipDay {
			t.Errorf("skipped target day still counted broken: %+v", broken)
		}
	}
}

func TestPostrequisiteCompletionDay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	// A weekly goal completed on Wednesday commits Wednesday, not the whole week.
	week := mustProject(t, db, ProjectInput{
		Name: "Weekly run", Schedule: ScheduleWeekly, Mode: ModeValue, Target: 3, StartDate: "2026-11-01",
	})
	sleep := mustProject(t, db, ProjectInput{Name: "Sleep", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	week, err := db.Projects().Update(ctx, week.ID, ProjectPatch{Postrequisites: &[]string{sleep.ID.String()}}, nil)
	if err != nil {
		t.Fatalf("wire: %v", err)
	}
	for _, day := range []string{"2026-11-16", "2026-11-17", "2026-11-18"} {
		if _, err := db.Checkins().Create(ctx, week.ID, CheckinInput{Value: 1, Date: day}); err != nil {
			t.Fatalf("run %s: %v", day, err)
		}
	}
	// Sleep on Monday, but not on the Wednesday completion day.
	if _, err := db.Checkins().Create(ctx, sleep.ID, CheckinInput{Value: 1, Date: "2026-11-16"}); err != nil {
		t.Fatalf("sleep: %v", err)
	}
	stats, err := db.Stats().ForProject(ctx, week, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.BrokenCommitments) != 1 || stats.BrokenCommitments[0].Date != "2026-11-18" {
		t.Fatalf("broken = %+v, want only 2026-11-18", stats.BrokenCommitments)
	}
	// Adding sleep on Wednesday clears it; the other days never committed.
	if _, err := db.Checkins().Create(ctx, sleep.ID, CheckinInput{Value: 1, Date: "2026-11-18"}); err != nil {
		t.Fatalf("sleep Wednesday: %v", err)
	}
	stats, err = db.Stats().ForProject(ctx, week, db.Today(), 0)
	if err != nil {
		t.Fatalf("stats after: %v", err)
	}
	if stats.Totals.PostViolations != 0 || stats.Totals.PostReqs != 1 {
		t.Errorf("after Wednesday sleep = %+v", stats.Totals)
	}
}

func TestLinkValidation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	a := mustProject(t, db, ProjectInput{Name: "A", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	b := mustProject(t, db, ProjectInput{Name: "B", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	bad := mustProject(t, db, ProjectInput{Name: "Bad", Nature: NatureBad, Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})

	// A postrequisite on a bad-habit source has no completion event.
	if _, err := db.Projects().Update(ctx, bad.ID, ProjectPatch{Postrequisites: &[]string{a.ID.String()}}, nil); err == nil {
		t.Error("bad habit accepted a postrequisite")
	}
	// The same project cannot be both a prerequisite and a postrequisite.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{
		Prerequisites:  &[]string{b.ID.String()},
		Postrequisites: &[]string{b.ID.String()},
	}, nil); err == nil {
		t.Error("overlapping prerequisite/postrequisite accepted")
	}
	// A cycle across the two directions is still a cycle.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Postrequisites: &[]string{b.ID.String()}}, nil); err != nil {
		t.Fatalf("a implies b: %v", err)
	}
	if _, err := db.Projects().Update(ctx, b.ID, ProjectPatch{Prerequisites: &[]string{a.ID.String()}}, nil); err == nil {
		t.Error("cross-direction cycle accepted")
	}
	// Self links are rejected in the domain.
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Postrequisites: &[]string{a.ID.String()}}, nil); err == nil {
		t.Error("self postrequisite accepted")
	}
	// Clearing the direction works and leaves the other untouched.
	empty := []string{}
	if _, err := db.Projects().Update(ctx, a.ID, ProjectPatch{Postrequisites: &empty}, nil); err != nil {
		t.Fatalf("clear postrequisites: %v", err)
	}
	got, err := db.Projects().Get(ctx, a.ID)
	if err != nil || len(got.Postrequisites) != 0 || len(got.Prerequisites) != 0 {
		t.Errorf("after clear = %+v, err = %v", got, err)
	}
}

func TestCreateProjectWithBothLinkDirections(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	pre := mustProject(t, db, ProjectInput{Name: "Pre", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	post := mustProject(t, db, ProjectInput{Name: "Post", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})
	made := mustProject(t, db, ProjectInput{
		Name: "Both", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01",
		Prerequisites: []string{pre.ID.String()}, Postrequisites: []string{post.ID.String()},
	})
	if len(made.Prerequisites) != 1 || made.Prerequisites[0] != pre.ID {
		t.Errorf("created prerequisites = %v", made.Prerequisites)
	}
	if len(made.Postrequisites) != 1 || made.Postrequisites[0] != post.ID {
		t.Errorf("created postrequisites = %v", made.Postrequisites)
	}
	got, err := db.Projects().Get(ctx, made.ID)
	if err != nil || len(got.Prerequisites) != 1 || len(got.Postrequisites) != 1 {
		t.Errorf("loaded links = %+v, err = %v", got, err)
	}

	// Deleting a linked project cascades its edges away.
	if err := db.Projects().Delete(ctx, pre.ID); err != nil {
		t.Fatalf("delete pre: %v", err)
	}
	got, err = db.Projects().Get(ctx, made.ID)
	if err != nil || len(got.Prerequisites) != 0 || len(got.Postrequisites) != 1 {
		t.Errorf("after deleting prerequisite = %+v, err = %v", got, err)
	}
	if err := db.Projects().Delete(ctx, post.ID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	got, err = db.Projects().Get(ctx, made.ID)
	if err != nil || len(got.Postrequisites) != 0 {
		t.Errorf("after deleting postrequisite = %+v, err = %v", got, err)
	}
}
