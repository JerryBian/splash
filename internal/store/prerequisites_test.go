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
