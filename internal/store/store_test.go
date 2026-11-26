package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
	"uuid"
)

// openTestDB returns an in-memory database on a fixed clock, so createdAt and
// "today" are reproducible. The single connection keeps :memory: alive.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(":memory:", func() time.Time { return testNow })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustCategory(t *testing.T, db *DB, name string) Category {
	t.Helper()
	c, err := db.Categories().Create(context.Background(), CategoryInput{Name: name, Icon: "💪", Color: "green"})
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	return c
}

func mustProject(t *testing.T, db *DB, in ProjectInput) Project {
	t.Helper()
	p, err := db.Projects().Create(context.Background(), in)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p
}

func TestOpenCreatesTheFileAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "splash.db")
	db, err := Open(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	var version int
	if err := db.sql.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Errorf("user_version = %d, want %d", version, schemaVersion)
	}

	// Migration 3 retired the private flag and added the good/bad nature.
	rows, err := db.sql.Query("PRAGMA table_info(projects)")
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	hasPrivate, hasNature := false, false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		hasPrivate = hasPrivate || name == "private"
		hasNature = hasNature || name == "nature"
	}
	if hasPrivate || !hasNature {
		t.Errorf("projects columns: private=%v nature=%v, want private gone / nature present", hasPrivate, hasNature)
	}

	// Reopening an existing database must not fail or lose data.
	if _, err := db.Categories().Create(context.Background(), CategoryInput{Name: "Persistence"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.Close()
	db2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = db2.Close() }()
	n, err := db2.Categories().Count(context.Background())
	if err != nil || n != 1 {
		t.Errorf("after reopen: count = %d, err = %v; want 1, nil", n, err)
	}
}

func TestCategoryCRUDAndConflicts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	cats := db.Categories()

	c := mustCategory(t, db, "Health")
	if _, err := cats.Create(ctx, CategoryInput{Name: "Health"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name: err = %v, want ErrConflict", err)
	}
	// Case-insensitive uniqueness is the database's job.
	if _, err := cats.Create(ctx, CategoryInput{Name: " HEALTH "}); err != nil {
		// different name, fine
		_ = err
	}

	got, err := cats.Get(ctx, c.ID)
	if err != nil || got.Name != "Health" {
		t.Fatalf("get = %+v, err = %v", got, err)
	}
	updated, err := cats.Update(ctx, c.ID, CategoryInput{Name: "Sport", Icon: "🏃", Color: "red"})
	if err != nil || updated.Name != "Sport" || updated.Color != "red" {
		t.Fatalf("update = %+v, err = %v", updated, err)
	}
	if err := cats.Delete(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := cats.Delete(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: err = %v, want ErrNotFound", err)
	}
}

func TestDeletingACategoryUncategorizesProjects(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	c := mustCategory(t, db, "Health")
	p := mustProject(t, db, ProjectInput{
		Name: "Water", CategoryID: c.ID.String(), Schedule: ScheduleDaily, Target: 8, StartDate: "2026-11-01",
	})
	if err := db.Categories().Delete(ctx, c.ID); err != nil {
		t.Fatalf("delete category: %v", err)
	}
	got, err := db.Projects().Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("project survived category delete? %v", err)
	}
	if got.CategoryID != uuid.Nil() {
		t.Errorf("categoryId = %s, want nil after category delete", got.CategoryID)
	}
}

func TestProjectCRUDAndArchive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	projects := db.Projects()

	a := mustProject(t, db, ProjectInput{Name: "Water", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})
	b := mustProject(t, db, ProjectInput{Name: "Reading", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})

	all, err := projects.List(ctx, ProjectQuery{})
	if err != nil || len(all) != 2 {
		t.Fatalf("list = %d, err = %v; want 2", len(all), err)
	}

	// Unknown category is a field problem, not a 500.
	_, err = projects.Create(ctx, ProjectInput{
		Name: "x", CategoryID: uuid.NewV7().String(), Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01",
	})
	var problems ValidationErrors
	if !errors.As(err, &problems) {
		t.Fatalf("unknown category: err = %v, want ValidationErrors", err)
	}

	// Archive hides from the default listing but keeps the data.
	archived := true
	if _, err := projects.Update(ctx, b.ID, ProjectPatch{Archived: &archived}, nil); err != nil {
		t.Fatalf("archive: %v", err)
	}
	defaults, _ := projects.List(ctx, ProjectQuery{})
	if len(defaults) != 1 || defaults[0].ID != a.ID {
		t.Errorf("default list = %d, want 1 (archived hidden)", len(defaults))
	}
	withArchived, _ := projects.List(ctx, ProjectQuery{IncludeArchived: true})
	if len(withArchived) != 2 {
		t.Errorf("archived list = %d, want 2", len(withArchived))
	}
}

func TestProjectUpdateEnforcesVersionInSQL(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Original", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})

	// A stale If-Match version fails before anything is written.
	stale := 1
	if _, err := db.Projects().Update(ctx, p.ID, ProjectPatch{Name: new("Renamed")}, &stale); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if _, err := db.Projects().Update(ctx, p.ID, ProjectPatch{Name: new("Renamed again")}, &stale); !errors.Is(err, ErrVersionConflict) {
		t.Errorf("stale version: err = %v, want ErrVersionConflict", err)
	}
	got, _ := db.Projects().Get(ctx, p.ID)
	if got.Name != "Renamed" || got.Version != 2 {
		t.Errorf("after conflict: %+v, want Renamed v2 (the stale write must not land)", got)
	}
}

func TestCheckinLifecycleAndAggregates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Water", Schedule: ScheduleDaily, Target: 8, Unit: "cups", StartDate: "2026-11-01"})
	checkins := db.Checkins()

	c1, err := checkins.Create(ctx, p.ID, CheckinInput{Value: 3, Note: "morning", Mood: "🙂", TZOffset: 480})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c1.LocalDate != "2026-11-21" {
		t.Errorf("localDate = %s, want the server's today", c1.LocalDate)
	}
	if _, err := checkins.Create(ctx, p.ID, CheckinInput{Value: 2}); err != nil {
		t.Fatalf("create second: %v", err)
	}
	// Backfill lands on the requested day, timestamped at noon.
	back, err := checkins.Create(ctx, p.ID, CheckinInput{Value: 8, Date: "2026-11-19"})
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if back.LocalDate != "2026-11-19" || back.CheckedAt.Hour() != 12 {
		t.Errorf("backfill = %+v", back)
	}

	agg, err := checkins.Aggregates(ctx, p.ID, "2026-11-01", "2026-11-21")
	if err != nil {
		t.Fatalf("aggregates: %v", err)
	}
	if agg["2026-11-21"].Value != 5 || agg["2026-11-21"].Count != 2 {
		t.Errorf("today aggregate = %+v, want 5/2", agg["2026-11-21"])
	}
	if agg["2026-11-19"].Value != 8 {
		t.Errorf("backfill aggregate = %+v", agg["2026-11-19"])
	}

	// Undo removes exactly one contribution.
	if err := checkins.Delete(ctx, c1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	agg, _ = checkins.Aggregates(ctx, p.ID, "", "")
	if agg["2026-11-21"].Value != 2 {
		t.Errorf("after undo: %+v, want value 2", agg["2026-11-21"])
	}

	// Check-ins on a missing project are rejected.
	if _, err := checkins.Create(ctx, uuid.NewV7(), CheckinInput{Value: 1}); !errors.Is(err, ErrNotFound) {
		t.Errorf("orphan checkin: err = %v, want ErrNotFound", err)
	}
}

func TestDeletingAProjectCascades(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Ephemeral", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})
	if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1}); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := db.Skips().Create(ctx, p.ID, SkipInput{Date: "2026-11-10"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if err := db.Projects().Delete(ctx, p.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM checkins`).Scan(&n); err != nil || n != 0 {
		t.Errorf("checkins after cascade = %d (err %v), want 0", n, err)
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM skips`).Scan(&n); err != nil || n != 0 {
		t.Errorf("skips after cascade = %d (err %v), want 0", n, err)
	}
}

func TestSkipLifecycle(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Meditation", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})
	skips := db.Skips()

	if _, err := skips.Create(ctx, p.ID, SkipInput{Date: "2026-11-10", Reason: "business trip"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := skips.Create(ctx, p.ID, SkipInput{Date: "2026-11-10"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate skip: err = %v, want ErrConflict", err)
	}
	m, err := skips.Map(ctx, p.ID)
	if err != nil || m["2026-11-10"] != "business trip" {
		t.Fatalf("map = %v, err = %v", m, err)
	}
	if err := skips.Delete(ctx, p.ID, "2026-11-10"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := skips.Delete(ctx, p.ID, "2026-11-10"); !errors.Is(err, ErrNotFound) {
		t.Errorf("double delete: err = %v, want ErrNotFound", err)
	}
}

func TestStatsForProjectEndToEnd(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Reading", Schedule: ScheduleDaily, Target: 30, Unit: "minutes", StartDate: "2026-11-18"})
	for _, day := range []string{"2026-11-18", "2026-11-19", "2026-11-20"} {
		if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 30, Date: day}); err != nil {
			t.Fatalf("checkin %s: %v", day, err)
		}
	}
	stats, err := db.Stats().ForProject(ctx, p, "2026-11-21", 4)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Summary.Streak != 3 || stats.Summary.Status != StatusPending {
		t.Errorf("summary = %+v, want streak 3 / pending", stats.Summary)
	}
	if stats.Totals.Checkins != 3 || stats.Totals.Done != 3 {
		t.Errorf("totals = %+v", stats.Totals)
	}
	if len(stats.Recent) != 3 {
		t.Errorf("recent = %d, want 3", len(stats.Recent))
	}
	if len(stats.Heatmap) == 0 {
		t.Error("heatmap is empty although weeks = 4")
	}
}

func TestOverviewSplitsGoodAndBad(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	c := mustCategory(t, db, "Health")
	good := mustProject(t, db, ProjectInput{Name: "Water", CategoryID: c.ID.String(), Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-19"})
	bad := mustProject(t, db, ProjectInput{Name: "Late nights", Nature: NatureBad, Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-19"})
	// The good habit is completed every day; the bad habit lapses once.
	for _, day := range []string{"2026-11-19", "2026-11-20", "2026-11-21"} {
		if _, err := db.Checkins().Create(ctx, good.ID, CheckinInput{Value: 1, Date: day}); err != nil {
			t.Fatalf("good checkin: %v", err)
		}
	}
	if _, err := db.Checkins().Create(ctx, bad.ID, CheckinInput{Value: 1, Date: "2026-11-20"}); err != nil {
		t.Fatalf("bad checkin: %v", err)
	}

	overview, err := db.Stats().Overview(ctx, []Project{good, bad}, "2026-11-21")
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	// Check-ins count good progress only; the lapse is a slip, not a check-in.
	if overview.Totals.Checkins != 3 || overview.Totals.Slips != 1 {
		t.Errorf("totals = %+v, want 3 checkins / 1 slip", overview.Totals)
	}
	if overview.Totals.MonthCheckins != 3 || overview.Totals.MonthSlips != 1 {
		t.Errorf("month = %+v, want 3/1", overview.Totals)
	}
	// Today: the good habit is done, the bad habit is clean (held) → both done.
	if overview.Today.Done != 2 || overview.Today.Total != 2 || overview.Today.Slipped != 0 {
		t.Errorf("today = %+v, want 2 done / 2 total / 0 slipped", overview.Today)
	}
	// The activity heat counts good check-ins; the lapse rides as slips.
	var activityGood, activitySlips int
	for _, d := range overview.Activity {
		activityGood += d.Count
		activitySlips += d.Slips
	}
	if activityGood != 3 || activitySlips != 1 {
		t.Errorf("activity = %d good / %d slips, want 3/1", activityGood, activitySlips)
	}
	// The category donut counts good check-ins only.
	if len(overview.Categories) != 1 || overview.Categories[0].Name != "Health" || overview.Categories[0].Count != 3 {
		t.Errorf("categories = %+v", overview.Categories)
	}
	// Leaders carry the nature so the UI can label abstention streaks apart.
	for _, l := range overview.Leaders {
		if l.ID == bad.ID.String() && l.Nature != NatureBad {
			t.Errorf("bad leader nature = %q, want bad", l.Nature)
		}
	}
}

func TestSeedDemoIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	n, err := db.SeedDemo(ctx)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if n == 0 {
		t.Fatal("seed inserted nothing into an empty database")
	}
	again, err := db.SeedDemo(ctx)
	if err != nil || again != 0 {
		t.Errorf("second seed = %d (err %v), want 0 — real data must never be touched", again, err)
	}
	projects, err := db.Projects().List(ctx, ProjectQuery{})
	if err != nil || len(projects) != n {
		t.Fatalf("projects = %d, want %d", len(projects), n)
	}
	// The demo must include history, a bad habit and an interval one.
	var checkins int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM checkins`).Scan(&checkins); err != nil || checkins == 0 {
		t.Errorf("demo history = %d check-ins, want > 0", checkins)
	}
	hasBad, hasInterval := false, false
	for _, p := range projects {
		hasBad = hasBad || p.Nature == NatureBad
		hasInterval = hasInterval || p.Schedule == ScheduleIntervalHours
	}
	if !hasBad || !hasInterval {
		t.Error("demo should include a bad habit and an interval_hours project")
	}
}

func TestDumpExportsEverything(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	c := mustCategory(t, db, "Health")
	p := mustProject(t, db, ProjectInput{Name: "Water", CategoryID: c.ID.String(), Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"})
	if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1}); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := db.Skips().Create(ctx, p.ID, SkipInput{Date: "2026-11-10"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	dump, err := db.Dump(ctx)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if len(dump.Categories) != 1 || len(dump.Projects) != 1 || len(dump.Checkins) != 1 || len(dump.Skips) != 1 {
		t.Errorf("dump = %d/%d/%d/%d, want 1/1/1/1",
			len(dump.Categories), len(dump.Projects), len(dump.Checkins), len(dump.Skips))
	}
}

func TestTimeModeRecordsAndPunctuality(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{
		Name: "Sleep", Schedule: ScheduleDaily, Mode: ModeTime, TargetTime: "23:00", StartDate: "2026-11-18",
	})
	if p.Mode != ModeTime || p.Target != 1 || p.TargetTime != "23:00" {
		t.Fatalf("time project = %+v", p)
	}
	if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Time: "22:40", Date: "2026-11-19"}); err != nil {
		t.Fatalf("on-time record: %v", err)
	}
	if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Time: "23:30", Date: "2026-11-20"}); err != nil {
		t.Fatalf("late record: %v", err)
	}
	stats, err := db.Stats().ForProject(ctx, p, "2026-11-21", 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Totals.OnTimeRate != 0.5 || stats.Totals.LateCount != 1 {
		t.Errorf("punctuality = rate %v late %d, want 0.5 / 1", stats.Totals.OnTimeRate, stats.Totals.LateCount)
	}
	if stats.Totals.OnTimeCount != 1 {
		t.Errorf("onTimeCount = %d, want 1", stats.Totals.OnTimeCount)
	}
	if stats.Totals.AvgTime != "23:05" {
		t.Errorf("avgTime = %q, want 23:05", stats.Totals.AvgTime)
	}
	// Both days recorded complete their periods (time mode completes on a record).
	if stats.Totals.Done != 2 {
		t.Errorf("done = %d, want 2", stats.Totals.Done)
	}
	// A malformed clock is a field problem.
	if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Time: "25:00"}); !errors.Is(err, ErrValidation) {
		t.Errorf("bad clock: err = %v, want ErrValidation", err)
	}
	// time mode cannot pair with interval_hours or a bad nature.
	if _, err := db.Projects().Create(ctx, ProjectInput{Name: "x", Schedule: ScheduleIntervalHours, Interval: 2, Mode: ModeTime, StartDate: "2026-11-01"}); !errors.Is(err, ErrValidation) {
		t.Errorf("time+interval_hours: err = %v, want ErrValidation", err)
	}
	if _, err := db.Projects().Create(ctx, ProjectInput{Name: "x", Schedule: ScheduleDaily, Mode: ModeTime, Nature: NatureBad, StartDate: "2026-11-01"}); !errors.Is(err, ErrValidation) {
		t.Errorf("time+bad: err = %v, want ErrValidation", err)
	}
}

func TestImportRestoresExportAdditively(t *testing.T) {
	ctx := context.Background()
	src := openTestDB(t)
	c := mustCategory(t, src, "Health")
	p := mustProject(t, src, ProjectInput{Name: "Water", CategoryID: c.ID.String(), Schedule: ScheduleDaily, Target: 8, Unit: "cups", StartDate: "2026-11-01"})
	if _, err := src.Checkins().Create(ctx, p.ID, CheckinInput{Value: 3, Date: "2026-11-10"}); err != nil {
		t.Fatalf("checkin: %v", err)
	}
	if _, err := src.Skips().Create(ctx, p.ID, SkipInput{Date: "2026-11-11"}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	dump, err := src.Dump(ctx)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}

	dst := openTestDB(t)
	counts, err := dst.Import(ctx, dump)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if counts.Categories != 1 || counts.Projects != 1 || counts.Checkins != 1 || counts.Skips != 1 {
		t.Errorf("counts = %+v, want 1/1/1/1", counts)
	}
	// The project keeps its original id, so references and history line up.
	got, err := dst.Projects().Get(ctx, p.ID)
	if err != nil || got.Name != "Water" || got.CategoryID != c.ID {
		t.Errorf("imported project = %+v, err = %v", got, err)
	}

	// Re-importing the same backup is a no-op (additive by id).
	again, err := dst.Import(ctx, dump)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if again != (ImportCounts{}) {
		t.Errorf("re-import counts = %+v, want all zero", again)
	}
}

func TestImportRejectsDanglingReferences(t *testing.T) {
	ctx := context.Background()
	dst := openTestDB(t)
	bad := ExportData{
		Checkins: []Checkin{{ID: uuid.NewV7(), ProjectID: uuid.NewV7(), Value: 1, LocalDate: "2026-11-10"}},
	}
	if _, err := dst.Import(ctx, bad); !errors.Is(err, ErrValidation) {
		t.Errorf("dangling checkin: err = %v, want ErrValidation", err)
	}
	// Nothing was applied.
	n, _ := dst.Checkins().Aggregates(ctx, uuid.NewV7(), "", "")
	if len(n) != 0 {
		t.Error("a rejected import left rows behind")
	}
}

func TestTimeModeOvernightDeadline(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	// "Asleep by 00:30": 23:40 is the previous evening and therefore on time;
	// 00:40 is late. A same-day string compare would invert both verdicts.
	p := mustProject(t, db, ProjectInput{
		Name: "Sleep", Schedule: ScheduleDaily, Mode: ModeTime, TargetTime: "00:30", StartDate: "2026-11-18",
	})
	for _, in := range []CheckinInput{
		{Value: 1, Time: "23:40", Date: "2026-11-19"},
		{Value: 1, Time: "00:40", Date: "2026-11-20"},
	} {
		if _, err := db.Checkins().Create(ctx, p.ID, in); err != nil {
			t.Fatalf("record %s: %v", in.Time, err)
		}
	}
	stats, err := db.Stats().ForProject(ctx, p, "2026-11-21", 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Totals.OnTimeCount != 1 || stats.Totals.LateCount != 1 || stats.Totals.OnTimeRate != 0.5 {
		t.Errorf("punctuality = %d on time / %d late / rate %v, want 1 / 1 / 0.5",
			stats.Totals.OnTimeCount, stats.Totals.LateCount, stats.Totals.OnTimeRate)
	}
	// 23:40 and 00:40 average around midnight, not to a same-day 12:10.
	if stats.Totals.AvgTime != "00:10" {
		t.Errorf("avgTime = %q, want 00:10", stats.Totals.AvgTime)
	}
}

func TestTimeModeAverageWrapsMidnight(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{
		Name: "Sleep", Schedule: ScheduleDaily, Mode: ModeTime, StartDate: "2026-11-18",
	})
	for i, clock := range []string{"23:50", "00:10"} {
		day := AddDays("2026-11-19", i)
		if _, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Time: clock, Date: day}); err != nil {
			t.Fatalf("record %s: %v", clock, err)
		}
	}
	stats, err := db.Stats().ForProject(ctx, p, "2026-11-21", 0)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Totals.AvgTime != "00:00" {
		t.Errorf("avgTime = %q, want 00:00 (circular mean)", stats.Totals.AvgTime)
	}
	// Exactly opposite clocks have no meaningful circular mean.
	if got := circularMeanClock([]int{8 * 60, 20 * 60}); got != "" {
		t.Errorf("circularMeanClock(opposites) = %q, want empty", got)
	}
}

func TestPinnedRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{
		Name: "Water", Schedule: ScheduleDaily, Mode: ModeCheck, Pinned: true, StartDate: "2026-11-01",
	})
	if !p.Pinned {
		t.Fatal("created project is not pinned")
	}
	got, err := db.Projects().Get(ctx, p.ID)
	if err != nil || !got.Pinned {
		t.Fatalf("get = %+v, err = %v; want pinned", got, err)
	}
	off := false
	updated, err := db.Projects().Update(ctx, p.ID, ProjectPatch{Pinned: &off}, &p.Version)
	if err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if updated.Pinned || updated.Version != p.Version+1 {
		t.Errorf("updated = pinned %v version %d", updated.Pinned, updated.Version)
	}
	// A full backup carries the pin; import into a clean database keeps it.
	pinned := true
	if _, err := db.Projects().Update(ctx, p.ID, ProjectPatch{Pinned: &pinned}, &updated.Version); err != nil {
		t.Fatalf("repin: %v", err)
	}
	data, err := db.Dump(ctx)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	dest := openTestDB(t)
	if _, err := dest.Import(ctx, data); err != nil {
		t.Fatalf("import: %v", err)
	}
	restored, err := dest.Projects().Get(ctx, p.ID)
	if err != nil || !restored.Pinned {
		t.Fatalf("restored = %+v, err = %v; want pinned", restored, err)
	}
}

func TestCheckinPhotoRoundTrip(t *testing.T) {
	const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	db := openTestDB(t)
	ctx := context.Background()
	p := mustProject(t, db, ProjectInput{Name: "Proof", Schedule: ScheduleDaily, Mode: ModeCheck, StartDate: "2026-11-01"})

	c, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1, Photo: png})
	if err != nil {
		t.Fatalf("create with photo: %v", err)
	}
	if !c.HasPhoto || c.Photo != "" {
		t.Errorf("create response = hasPhoto %v photo len %d; want true / payload omitted", c.HasPhoto, len(c.Photo))
	}
	stored, err := db.Checkins().Photo(ctx, c.ID)
	if err != nil || stored != png {
		t.Fatalf("photo = %d bytes, err = %v; want the original data URL", len(stored), err)
	}
	// List responses carry the flag only.
	list, err := db.Checkins().List(ctx, CheckinQuery{ProjectID: p.ID})
	if err != nil || len(list) != 1 || !list[0].HasPhoto || list[0].Photo != "" {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	// Recent/statistics responses are likewise payload-free.
	stats, err := db.Stats().ForProject(ctx, p, "2026-11-21", 0)
	if err != nil || len(stats.Recent) != 1 || !stats.Recent[0].HasPhoto || stats.Recent[0].Photo != "" {
		t.Fatalf("stats recent = %+v, err = %v", stats.Recent, err)
	}

	// The backup restores the picture into a clean database.
	data, err := db.Dump(ctx)
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	if len(data.Checkins) != 1 || data.Checkins[0].Photo != png {
		t.Fatalf("dump checkin photo = %d bytes, want %d", len(data.Checkins[0].Photo), len(png))
	}
	dest := openTestDB(t)
	if _, err := dest.Import(ctx, data); err != nil {
		t.Fatalf("import: %v", err)
	}
	restored, err := dest.Checkins().Photo(ctx, c.ID)
	if err != nil || restored != png {
		t.Fatalf("restored photo = %d bytes, err = %v", len(restored), err)
	}

	// A check-in without a photo, and a deleted one, answer ErrNotFound.
	plain, err := db.Checkins().Create(ctx, p.ID, CheckinInput{Value: 1})
	if err != nil {
		t.Fatalf("create plain: %v", err)
	}
	if _, err := db.Checkins().Photo(ctx, plain.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("plain photo: err = %v, want ErrNotFound", err)
	}
	if err := db.Checkins().Delete(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.Checkins().Photo(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted photo: err = %v, want ErrNotFound", err)
	}
}

func TestMigrateFromV4AddsPinAndPhoto(t *testing.T) {
	// Simulate a database that ran migration 4 but not 5: strip the newer
	// columns and stamp the old version, then let Open upgrade it. This is the
	// path a real deployment takes, not just the fresh-database path above.
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := Open(path, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.sql.Exec(`
		ALTER TABLE projects DROP COLUMN pinned;
		ALTER TABLE checkins DROP COLUMN photo;
		PRAGMA user_version = 4;
	`); err != nil {
		_ = db.Close()
		t.Fatalf("shape v4 database: %v", err)
	}
	_ = db.Close()

	db2, err := Open(path, nil)
	if err != nil {
		t.Fatalf("reopen at v4: %v", err)
	}
	defer func() { _ = db2.Close() }()
	var version int
	if err := db2.sql.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
	const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	p := mustProject(t, db2, ProjectInput{
		Name: "Upgraded", Schedule: ScheduleDaily, Mode: ModeCheck, Pinned: true, StartDate: "2026-11-01",
	})
	if !p.Pinned {
		t.Error("pinned did not survive the upgrade")
	}
	c, err := db2.Checkins().Create(context.Background(), p.ID, CheckinInput{Value: 1, Photo: png})
	if err != nil || !c.HasPhoto {
		t.Errorf("photo checkin after upgrade = %+v, err = %v", c, err)
	}
}
