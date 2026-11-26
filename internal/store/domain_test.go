package store

import (
	"errors"
	"strings"
	"testing"
)

func TestProjectInputValidation(t *testing.T) {
	valid := ProjectInput{
		Name: "Water", Schedule: ScheduleDaily, Target: 8, Unit: "cups", StartDate: "2026-11-01",
	}
	tests := []struct {
		name      string
		mutate    func(*ProjectInput)
		wantField string // "" means valid
	}{
		{"valid", func(*ProjectInput) {}, ""},
		{"empty name", func(in *ProjectInput) { in.Name = "  " }, "name"},
		{"long name", func(in *ProjectInput) { in.Name = strings.Repeat("a", MaxNameLength+1) }, "name"},
		{"long description", func(in *ProjectInput) { in.Description = strings.Repeat("a", MaxDescriptionLength+1) }, "description"},
		{"bad color", func(in *ProjectInput) { in.Color = "chartreuse" }, "color"},
		{"bad category id", func(in *ProjectInput) { in.CategoryID = "nope" }, "categoryId"},
		{"bad schedule", func(in *ProjectInput) { in.Schedule = "hourly" }, "schedule"},
		{"zero target", func(in *ProjectInput) { in.Target = 0 }, "target"},
		{"negative target", func(in *ProjectInput) { in.Target = -1 }, "target"},
		{"long unit", func(in *ProjectInput) { in.Unit = strings.Repeat("u", MaxUnitLength+1) }, "unit"},
		{"weekdays without days", func(in *ProjectInput) { in.Schedule = ScheduleWeekdays; in.Weekdays = nil }, "weekdays"},
		{"weekdays out of range", func(in *ProjectInput) { in.Schedule = ScheduleWeekdays; in.Weekdays = []int{0, 8} }, "weekdays"},
		{"weekdays valid", func(in *ProjectInput) { in.Schedule = ScheduleWeekdays; in.Weekdays = []int{1, 3, 5} }, ""},
		{"interval hours too small", func(in *ProjectInput) { in.Schedule = ScheduleIntervalHours; in.Interval = 0.5 }, "interval"},
		{"interval hours valid", func(in *ProjectInput) { in.Schedule = ScheduleIntervalHours; in.Interval = 2 }, ""},
		{"interval days fractional", func(in *ProjectInput) { in.Schedule = ScheduleIntervalDays; in.Interval = 1.5 }, "interval"},
		{"interval days valid", func(in *ProjectInput) { in.Schedule = ScheduleIntervalDays; in.Interval = 3 }, ""},
		{"bad start date", func(in *ProjectInput) { in.StartDate = "2026-13-01" }, "startDate"},
		{"missing start date", func(in *ProjectInput) { in.StartDate = "" }, "startDate"},
		{"future start date", func(in *ProjectInput) { in.StartDate = "2026-12-01" }, "startDate"},
		{"ancient start date", func(in *ProjectInput) { in.StartDate = "1999-01-01" }, "startDate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.mutate(&in)
			err := in.Validate("2026-11-21")
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("valid input rejected: %v", err)
				}
				return
			}
			var problems ValidationErrors
			if !errors.As(err, &problems) {
				t.Fatalf("err = %v, want ValidationErrors", err)
			}
			if !errors.Is(err, ErrValidation) {
				t.Errorf("errors.Is(err, ErrValidation) = false")
			}
			found := false
			for _, p := range problems {
				if p.Field == tt.wantField {
					found = true
				}
			}
			if !found {
				t.Errorf("problems %v do not mention field %q", problems, tt.wantField)
			}
		})
	}
}

func TestProjectInputNormalized(t *testing.T) {
	in := ProjectInput{
		Name: "  Water  ", Schedule: ScheduleWeekdays, Target: 1,
		Weekdays: []int{5, 1, 3, 1}, StartDate: "2026-11-01", Interval: 99,
	}
	n := in.Normalized()
	if n.Name != "Water" {
		t.Errorf("name = %q", n.Name)
	}
	if n.Icon != "✅" || n.Color != "blue" || n.Unit != "次" {
		t.Errorf("defaults not applied: %+v", n)
	}
	if len(n.Weekdays) != 3 || n.Weekdays[0] != 1 || n.Weekdays[2] != 5 {
		t.Errorf("weekdays = %v, want sorted deduped [1 3 5]", n.Weekdays)
	}
	if n.Interval != 0 {
		t.Errorf("interval = %v, want 0 for a non-interval schedule", n.Interval)
	}
}

func TestProjectPatchValidate(t *testing.T) {
	current := Project{Name: "Water", Schedule: ScheduleDaily, Target: 8, Unit: "cups", StartDate: "2026-11-01"}

	empty := ProjectPatch{}
	if err := empty.Validate(current, "2026-11-21"); err == nil {
		t.Error("empty patch accepted")
	}

	// Switching to weekdays without providing days is rejected against the
	// merged project, not just the patch in isolation.
	bad := ProjectPatch{Schedule: new(ScheduleWeekdays)}
	if err := bad.Validate(current, "2026-11-21"); err == nil {
		t.Error("schedule switch without weekdays accepted")
	}

	good := ProjectPatch{Schedule: new(ScheduleWeekdays), Weekdays: new([]int{1, 3, 5})}
	if err := good.Validate(current, "2026-11-21"); err != nil {
		t.Errorf("valid schedule switch rejected: %v", err)
	}

	// A name-only patch leaves the schedule alone.
	nameOnly := ProjectPatch{Name: new("Drink more")}
	if err := nameOnly.Validate(current, "2026-11-21"); err != nil {
		t.Errorf("name patch rejected: %v", err)
	}
	next := current
	nameOnly.ApplyTo(&next)
	if next.Name != "Drink more" || next.Target != 8 {
		t.Errorf("ApplyTo = %+v", next)
	}
}

func TestCheckinInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		in        CheckinInput
		wantField string
	}{
		{"valid", CheckinInput{Value: 1}, ""},
		{"zero value", CheckinInput{Value: 0}, "value"},
		{"negative value", CheckinInput{Value: -2}, "value"},
		{"huge value", CheckinInput{Value: MaxTargetValue + 1}, "value"},
		{"long note", CheckinInput{Value: 1, Note: strings.Repeat("n", MaxNoteLength+1)}, "note"},
		{"bad mood", CheckinInput{Value: 1, Mood: "🤖"}, "mood"},
		{"good mood", CheckinInput{Value: 1, Mood: "🔥"}, ""},
		{"backfill", CheckinInput{Value: 1, Date: "2026-11-10"}, ""},
		{"tomorrow allowed (timezone)", CheckinInput{Value: 1, Date: "2026-11-22"}, ""},
		{"far future rejected", CheckinInput{Value: 1, Date: "2026-11-23"}, "date"},
		{"bad date", CheckinInput{Value: 1, Date: "yesterday"}, "date"},
		{"bad tz", CheckinInput{Value: 1, TZOffset: 9000}, "tzOffset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate("2026-11-21")
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("valid input rejected: %v", err)
				}
				return
			}
			var problems ValidationErrors
			if !errors.As(err, &problems) {
				t.Fatalf("err = %v, want ValidationErrors", err)
			}
			for _, p := range problems {
				if p.Field == tt.wantField {
					return
				}
			}
			t.Errorf("problems %v do not mention %q", problems, tt.wantField)
		})
	}
}

func TestSkipInputValidation(t *testing.T) {
	if err := (SkipInput{Date: "2026-11-20"}).Validate("2026-11-01", "2026-11-21"); err != nil {
		t.Errorf("valid skip rejected: %v", err)
	}
	if err := (SkipInput{Date: "2026-10-01"}).Validate("2026-11-01", "2026-11-21"); err == nil {
		t.Error("skip before project start accepted")
	}
	if err := (SkipInput{Date: "2026-11-25"}).Validate("2026-11-01", "2026-11-21"); err == nil {
		t.Error("future skip accepted")
	}
}

func TestCategoryInputValidation(t *testing.T) {
	if err := (CategoryInput{Name: "Health"}).Validate(); err != nil {
		t.Errorf("valid category rejected: %v", err)
	}
	if err := (CategoryInput{Name: ""}).Validate(); err == nil {
		t.Error("empty category name accepted")
	}
	if err := (CategoryInput{Name: "x", Color: "neon"}).Validate(); err == nil {
		t.Error("bad color accepted")
	}
}

func TestProjectQueryMatches(t *testing.T) {
	water := Project{Name: "Water", Description: "eight cups a day"}
	archived := Project{Name: "Old habit", Archived: true}

	defaults := ProjectQuery{}
	if !defaults.Matches(water) || defaults.Matches(archived) {
		t.Error("default query should hide archived projects only")
	}
	all := ProjectQuery{IncludeArchived: true}
	if !all.Matches(water) || !all.Matches(archived) {
		t.Error("IncludeArchived query should see everything")
	}
	search := ProjectQuery{Search: "eight cups"}
	if !search.Matches(water) || search.Matches(archived) {
		t.Error("search should match description substrings")
	}
}

func TestNatureValidationAndNormalization(t *testing.T) {
	base := ProjectInput{Name: "Late nights", Schedule: ScheduleDaily, Target: 1, StartDate: "2026-11-01"}

	// An unknown nature is a field problem.
	bad := base
	bad.Nature = "evil"
	if err := bad.Validate("2026-11-21"); err == nil {
		t.Error("unknown nature accepted")
	}

	// Bad habits cannot use the interval_hours countdown.
	ih := base
	ih.Nature = NatureBad
	ih.Schedule = ScheduleIntervalHours
	ih.Interval = 2
	err := ih.Validate("2026-11-21")
	var problems ValidationErrors
	if !errors.As(err, &problems) {
		t.Fatalf("bad + interval_hours: err = %v, want ValidationErrors", err)
	}
	found := false
	for _, p := range problems {
		if p.Field == "schedule" {
			found = true
		}
	}
	if !found {
		t.Errorf("problems %v do not mention schedule", problems)
	}

	// A bad habit normalizes to check mode with target 1, whatever was sent.
	badValue := base
	badValue.Nature = NatureBad
	badValue.Mode = ModeValue
	badValue.Target = 5
	badValue.Unit = "支"
	n := badValue.Normalized()
	if n.Nature != NatureBad || n.Mode != ModeCheck || n.Target != 1 || n.Unit != "次" {
		t.Errorf("bad habit not normalized to check mode: %+v", n)
	}

	// The default nature is good.
	if got := base.Normalized().Nature; got != NatureGood {
		t.Errorf("default nature = %q, want good", got)
	}

	// Patching nature to bad on an interval_hours project is rejected.
	current := Project{Schedule: ScheduleIntervalHours, Interval: 2, StartDate: "2026-11-01"}
	patch := ProjectPatch{Nature: new(NatureBad)}
	if err := patch.Validate(current, "2026-11-21"); err == nil {
		t.Error("patch to bad on interval_hours accepted")
	}
}

func TestLateForDeadline(t *testing.T) {
	tests := []struct {
		name     string
		deadline string
		clock    string
		want     bool
	}{
		{"early same day", "23:00", "22:40", false},
		{"exactly at the deadline", "23:00", "23:00", false},
		{"one minute late", "23:00", "23:01", true},
		{"after midnight for a bedtime deadline", "23:00", "00:10", true},
		{"evening before an after-midnight deadline", "00:30", "23:40", false},
		{"before an after-midnight deadline", "00:30", "00:20", false},
		{"after an after-midnight deadline", "00:30", "00:40", true},
		{"morning habit early", "08:00", "07:59", false},
		{"morning habit late", "08:00", "08:01", true},
		{"half a day after is late", "08:00", "20:00", true},
		{"more than half a day after reads as next day", "08:00", "20:01", false},
		{"empty deadline", "", "09:00", false},
		{"empty clock", "09:00", "", false},
		{"malformed clock", "09:00", "9:0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LateForDeadline(tt.deadline, tt.clock); got != tt.want {
				t.Errorf("LateForDeadline(%q, %q) = %v, want %v", tt.deadline, tt.clock, got, tt.want)
			}
		})
	}
}

func TestDecodePhoto(t *testing.T) {
	// One transparent 1x1 PNG, the smallest honest image for the round trip.
	const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	mime, data, err := DecodePhoto(png)
	if err != nil || mime != "image/png" || len(data) == 0 {
		t.Fatalf("DecodePhoto(png) = %q, %d bytes, %v", mime, len(data), err)
	}
	for _, bad := range []string{
		"",
		"not-a-data-url",
		"data:text/html;base64,PGgxPmhpPC9oMT4=",
		"data:image/png;base64,***",
		"data:image/png,",
	} {
		if _, _, err := DecodePhoto(bad); err == nil {
			t.Errorf("DecodePhoto(%q) accepted a bad payload", bad)
		}
	}

	// Check-in validation reports the photo field rather than failing opaquely.
	in := CheckinInput{Value: 1, Photo: "data:text/html;base64,PGgxPmhpPC9oMT4="}
	var problems ValidationErrors
	if err := in.Validate("2026-11-21"); !errors.As(err, &problems) {
		t.Fatalf("err = %v, want ValidationErrors", err)
	}
	found := false
	for _, p := range problems {
		if p.Field == "photo" {
			found = true
		}
	}
	if !found {
		t.Errorf("problems %v do not mention photo", problems)
	}

	// Oversized payloads are rejected before storage.
	if err := (CheckinInput{Value: 1, Photo: "data:image/png;base64," + strings.Repeat("A", MaxPhotoBytes)}).Validate("2026-11-21"); err == nil {
		t.Error("oversized photo accepted")
	}
}
