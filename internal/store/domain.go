package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

// Field limits enforced by validation. They exist so a hostile or buggy client
// cannot make the service store unbounded strings.
const (
	MaxNameLength        = 40
	MaxDescriptionLength = 500
	MaxNoteLength        = 200
	MaxReasonLength      = 100
	MaxUnitLength        = 8
	MaxIconLength        = 8
	MaxTargetValue       = 1e6
	MaxIntervalHours     = 720
	MaxIntervalDays      = 365
	// MinStartDate bounds history walks: a project "started" in 1970 would make
	// every stats request iterate twenty thousand periods for no reason.
	MinStartDate = "2000-01-01"
)

// InkColors is the closed palette a project or category may pick from. Colors
// are stored as token names rather than hex values so the frontend can render
// them differently in light and dark themes without a data migration.
var InkColors = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink"}

// Moods is the closed set of check-in moods, stored as the emoji itself so no
// translation table is needed on either end.
var Moods = []string{"😄", "🙂", "😐", "😪", "🔥"}

// ScheduleType selects how a project's periods are laid out on the calendar.
// The zero value is invalid; validation rejects it.
type ScheduleType string

// Supported schedules. Target means "units per period" for all of them; the
// period is a day, a week, a month, or an N-day window. interval_hours is the
// exception: it has no calendar periods, only a rolling countdown between
// check-ins, and Target is unused.
const (
	ScheduleDaily         ScheduleType = "daily"          // every day
	ScheduleWeekdays      ScheduleType = "weekdays"       // chosen days of the week
	ScheduleWeekly        ScheduleType = "weekly"         // N units per ISO week
	ScheduleMonthly       ScheduleType = "monthly"        // N units per calendar month
	ScheduleIntervalDays  ScheduleType = "interval_days"  // one N-day window after another
	ScheduleIntervalHours ScheduleType = "interval_hours" // check in every N hours
)

// ValidSchedule reports whether s is one of the supported schedules.
func ValidSchedule(s ScheduleType) bool {
	switch s {
	case ScheduleDaily, ScheduleWeekdays, ScheduleWeekly, ScheduleMonthly,
		ScheduleIntervalDays, ScheduleIntervalHours:
		return true
	}
	return false
}

// Category groups projects. Names are unique case-insensitively; deleting a
// category uncategorizes its projects rather than deleting them.
type Category struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
}

// CategoryInput is the payload for creating or replacing a category.
type CategoryInput struct {
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// Validate reports every problem with the input at once.
func (in CategoryInput) Validate() error {
	return collect([]*FieldError{
		validateName("name", in.Name),
		validateIcon(in.Icon),
		validateColor(in.Color),
	})
}

// Normalized returns a trimmed copy with defaults applied. Call after Validate.
func (in CategoryInput) Normalized() CategoryInput {
	return CategoryInput{
		Name:  strings.TrimSpace(in.Name),
		Icon:  orDefault(strings.TrimSpace(in.Icon), "🗂️"),
		Color: orDefault(strings.TrimSpace(in.Color), "blue"),
	}
}

// Completion modes, orthogonal to the schedule (period layout).
//
// ModeCheck means ONE tick completes the period: "read 30 minutes" is a
// description of the obligation, not a quantity to accumulate — tick it or
// leave an X. ModeValue accumulates check-in values toward Target (8 cups,
// 15 km). Check mode forces Target to 1.
const (
	ModeCheck = "check"
	ModeValue = "value"
)

// Project is one check-in habit: what to do, how often, and who may see it.
type Project struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	Color       string       `json:"color"`
	CategoryID  uuid.UUID    `json:"categoryId"` // uuid.Nil() means uncategorized
	Private     bool         `json:"private"`    // hidden from anonymous visitors
	Schedule    ScheduleType `json:"schedule"`
	Mode        string       `json:"mode"`   // ModeCheck | ModeValue
	Target      float64      `json:"target"` // units per period; unused by interval_hours
	Unit        string       `json:"unit"`   // UI-facing: "times", "cups", "minutes", "km"…
	Weekdays    []int        `json:"weekdays"`
	Interval    float64      `json:"interval"` // hours or days, per Schedule
	StartDate   string       `json:"startDate"`
	Archived    bool         `json:"archived"`
	// Prerequisites are projects that must be satisfied on the same day before
	// this one counts as unlocked. All of them (AND), any depth of chain, no
	// cycles. Violations are derived at read time, never stored.
	Prerequisites []uuid.UUID `json:"prerequisites"`
	CreatedAt     time.Time   `json:"createdAt"`
	UpdatedAt     time.Time   `json:"updatedAt"`
	// Version backs the ETag / If-Match optimistic concurrency on updates.
	Version int `json:"version"`
}

// ETag returns the entity tag for this revision, e.g. `"v3"`.
func (p Project) ETag() string { return fmt.Sprintf(`"v%d"`, p.Version) }

// ProjectInput is the payload accepted when creating a project.
type ProjectInput struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Icon        string       `json:"icon"`
	Color       string       `json:"color"`
	CategoryID  string       `json:"categoryId"` // "" or a UUID
	Private     bool         `json:"private"`
	Schedule    ScheduleType `json:"schedule"`
	Mode        string       `json:"mode"`
	Target      float64      `json:"target"`
	Unit        string       `json:"unit"`
	Weekdays    []int        `json:"weekdays"`
	Interval    float64      `json:"interval"`
	StartDate   string       `json:"startDate"`
	// Prerequisites lists project ids that gate this one (same-day, AND).
	Prerequisites []string `json:"prerequisites"`
}

// Validate reports every problem with the input at once. today (YYYY-MM-DD)
// bounds StartDate so a project cannot claim to have started in the future.
func (in ProjectInput) Validate(today string) error {
	problems := []*FieldError{
		validateName("name", in.Name),
		validateDescription(in.Description),
		validateIcon(in.Icon),
		validateColor(in.Color),
		validateCategoryID(in.CategoryID),
		validateMode(in.Mode),
		validateSchedule(in.Schedule, in.Target, in.Unit, in.Weekdays, in.Interval),
		validateStartDate(in.StartDate, today),
		validatePrerequisites(in.Prerequisites, uuid.Nil()),
	}
	return collect(problems)
}

// Normalized returns a copy with whitespace trimmed and defaults applied.
func (in ProjectInput) Normalized() ProjectInput {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	out.Icon = orDefault(strings.TrimSpace(in.Icon), "✅")
	out.Color = orDefault(strings.TrimSpace(in.Color), "blue")
	out.CategoryID = strings.TrimSpace(in.CategoryID)
	out.Mode = orDefault(strings.TrimSpace(in.Mode), ModeValue)
	out.Unit = orDefault(strings.TrimSpace(in.Unit), "次")
	out.StartDate = strings.TrimSpace(in.StartDate)
	out.Prerequisites = normalizePrerequisites(in.Prerequisites)
	if out.Mode == ModeCheck {
		out.Target = 1
		out.Unit = "次"
	}
	if out.Schedule == ScheduleWeekdays {
		out.Weekdays = normalizeWeekdays(in.Weekdays)
	} else {
		out.Weekdays = []int{}
	}
	if out.Schedule != ScheduleIntervalDays && out.Schedule != ScheduleIntervalHours {
		out.Interval = 0
	}
	if out.Schedule == ScheduleIntervalHours {
		out.Target = 0
	}
	return out
}

// ProjectPatch carries the fields a client wants to change; nil means "leave
// alone". CategoryID is a *string so a patch can clear it with "".
type ProjectPatch struct {
	Name        *string       `json:"name,omitempty"`
	Description *string       `json:"description,omitempty"`
	Icon        *string       `json:"icon,omitempty"`
	Color       *string       `json:"color,omitempty"`
	CategoryID  *string       `json:"categoryId,omitempty"`
	Private     *bool         `json:"private,omitempty"`
	Schedule    *ScheduleType `json:"schedule,omitempty"`
	Mode        *string       `json:"mode,omitempty"`
	Target      *float64      `json:"target,omitempty"`
	Unit        *string       `json:"unit,omitempty"`
	Weekdays    *[]int        `json:"weekdays,omitempty"`
	Interval    *float64      `json:"interval,omitempty"`
	StartDate   *string       `json:"startDate,omitempty"`
	Archived    *bool         `json:"archived,omitempty"`
	// Prerequisites replaces the whole edge list; an empty slice clears it.
	Prerequisites *[]string `json:"prerequisites,omitempty"`
}

// Empty reports whether the patch would change nothing.
func (p ProjectPatch) Empty() bool {
	return p.Name == nil && p.Description == nil && p.Icon == nil && p.Color == nil &&
		p.CategoryID == nil && p.Private == nil && p.Schedule == nil && p.Mode == nil && p.Target == nil &&
		p.Unit == nil && p.Weekdays == nil && p.Interval == nil && p.StartDate == nil && p.Archived == nil &&
		p.Prerequisites == nil
}

// Validate checks only the fields present in the patch, against the project it
// will be applied to: changing the schedule re-validates the schedule fields
// that survive the merge, so a patch cannot leave an inconsistent combination.
func (p ProjectPatch) Validate(current Project, today string) error {
	if p.Empty() {
		return ValidationErrors{{Field: "(root)", Message: "must set at least one field"}}
	}
	merged := current
	p.ApplyTo(&merged)

	var problems []*FieldError
	if p.Name != nil {
		problems = append(problems, validateName("name", *p.Name))
	}
	if p.Description != nil {
		problems = append(problems, validateDescription(*p.Description))
	}
	if p.Icon != nil {
		problems = append(problems, validateIcon(*p.Icon))
	}
	if p.Color != nil {
		problems = append(problems, validateColor(*p.Color))
	}
	if p.CategoryID != nil {
		problems = append(problems, validateCategoryID(*p.CategoryID))
	}
	if p.Mode != nil {
		problems = append(problems, validateMode(*p.Mode))
	}
	if p.Schedule != nil || p.Target != nil || p.Unit != nil || p.Weekdays != nil || p.Interval != nil || p.Mode != nil {
		problems = append(problems, validateSchedule(merged.Schedule, merged.Target, merged.Unit, merged.Weekdays, merged.Interval))
	}
	if p.StartDate != nil {
		problems = append(problems, validateStartDate(*p.StartDate, today))
	}
	if p.Prerequisites != nil {
		problems = append(problems, validatePrerequisites(*p.Prerequisites, current.ID))
	}
	return collect(problems)
}

// ApplyTo writes the patch onto project, normalizing as it goes. It does not
// touch UpdatedAt or Version; the caller owns those.
func (p ProjectPatch) ApplyTo(project *Project) {
	if p.Name != nil {
		project.Name = strings.TrimSpace(*p.Name)
	}
	if p.Description != nil {
		project.Description = strings.TrimSpace(*p.Description)
	}
	if p.Icon != nil {
		project.Icon = orDefault(strings.TrimSpace(*p.Icon), "✅")
	}
	if p.Color != nil {
		project.Color = orDefault(strings.TrimSpace(*p.Color), "blue")
	}
	if p.CategoryID != nil {
		id := uuid.Nil()
		if raw := strings.TrimSpace(*p.CategoryID); raw != "" {
			// Validate ran first; a parse failure here would be a programming error.
			id, _ = uuid.Parse(raw)
		}
		project.CategoryID = id
	}
	if p.Private != nil {
		project.Private = *p.Private
	}
	if p.Schedule != nil {
		project.Schedule = *p.Schedule
	}
	if p.Target != nil {
		project.Target = *p.Target
	}
	if p.Unit != nil {
		project.Unit = orDefault(strings.TrimSpace(*p.Unit), "次")
	}
	if p.Weekdays != nil {
		project.Weekdays = normalizeWeekdays(*p.Weekdays)
	}
	if p.Interval != nil {
		project.Interval = *p.Interval
	}
	if p.StartDate != nil {
		project.StartDate = strings.TrimSpace(*p.StartDate)
	}
	if p.Archived != nil {
		project.Archived = *p.Archived
	}
	if p.Mode != nil {
		project.Mode = orDefault(strings.TrimSpace(*p.Mode), ModeValue)
	}
	if p.Prerequisites != nil {
		project.Prerequisites = parsePrerequisites(*p.Prerequisites)
	}
	// Keep the invariants that Normalized enforces on create.
	if project.Mode == ModeCheck {
		project.Target = 1
		project.Unit = "次"
	}
	if project.Schedule != ScheduleWeekdays {
		project.Weekdays = []int{}
	}
	if project.Schedule != ScheduleIntervalDays && project.Schedule != ScheduleIntervalHours {
		project.Interval = 0
	}
	if project.Schedule == ScheduleIntervalHours {
		project.Target = 0
	}
}

func validateMode(raw string) *FieldError {
	m := strings.TrimSpace(raw)
	if m != "" && m != ModeCheck && m != ModeValue {
		return &FieldError{Field: "mode", Message: "must be one of check, value"}
	}
	return nil
}

// validatePrerequisites checks ids parse and none is the project itself.
// Existence and cycle-freedom need the graph, so the store checks those.
func validatePrerequisites(ids []string, self uuid.UUID) *FieldError {
	for _, raw := range ids {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return &FieldError{Field: "prerequisites", Message: "must be a list of project UUIDs"}
		}
		if self != uuid.Nil() && id == self {
			return &FieldError{Field: "prerequisites", Message: "must not include the project itself"}
		}
	}
	return nil
}

func normalizePrerequisites(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// parsePrerequisites converts validated id strings; invalid entries are
// dropped because Validate ran first (a parse failure here is a caller bug).
func parsePrerequisites(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, raw := range normalizePrerequisites(ids) {
		if id, err := uuid.Parse(raw); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// Checkin is one completed (or partial) contribution to a project's period.
// Value is in the project's unit: 1 for a plain check-in, 8 for "8 cups", 5.2
// for "5.2 km". LocalDate is the client's calendar day, which is what every
// period computation uses; CheckedAt is the actual instant, used by
// interval_hours countdowns and history ordering.
type Checkin struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"projectId"`
	Value     float64   `json:"value"`
	Note      string    `json:"note"`
	Mood      string    `json:"mood"`
	LocalDate string    `json:"localDate"`
	CheckedAt time.Time `json:"checkedAt"`
	TZOffset  int       `json:"tzOffset"` // minutes east of UTC, as reported by the client
	// Backfilled marks a check-in recorded on a later calendar day than the
	// day it credits ("I forgot to tick that day"). Derived verdicts about
	// prerequisites (Violated/Unmet) are computed at read time, not stored.
	Backfilled bool      `json:"backfilled"`
	CreatedAt  time.Time `json:"createdAt"`
	// Violated/Unmet are DERIVED at read time (never stored): a check-in whose
	// project had unmet prerequisites on its credited day. Backfilling the
	// prerequisite later clears the violation automatically.
	Violated bool        `json:"violated"`
	Unmet    []uuid.UUID `json:"unmet"`
}

// CheckinInput is the payload for recording a check-in. An empty Date means
// "today on the server"; a past date is a backfill.
type CheckinInput struct {
	Value    float64 `json:"value"`
	Note     string  `json:"note"`
	Mood     string  `json:"mood"`
	Date     string  `json:"date"`
	TZOffset int     `json:"tzOffset"`
}

// Validate reports every problem with the input. today is the server's calendar
// day; dates up to one day ahead are accepted because a client east of the
// server can legitimately be in "tomorrow" already.
func (in CheckinInput) Validate(today string) error {
	var problems []*FieldError
	if in.Value <= 0 || in.Value > MaxTargetValue {
		problems = append(problems, &FieldError{Field: "value", Message: fmt.Sprintf("must be between 0 and %g", MaxTargetValue)})
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Note)) > MaxNoteLength {
		problems = append(problems, &FieldError{Field: "note", Message: fmt.Sprintf("must be at most %d characters", MaxNoteLength)})
	}
	if in.Mood != "" && !slices.Contains(Moods, in.Mood) {
		problems = append(problems, &FieldError{Field: "mood", Message: "is not a supported mood"})
	}
	if in.Date != "" {
		if err := validateDate("date", in.Date); err != nil {
			problems = append(problems, err)
		} else if in.Date > AddDays(today, 1) {
			problems = append(problems, &FieldError{Field: "date", Message: "must not be in the future"})
		}
	}
	if in.TZOffset < -840 || in.TZOffset > 840 {
		problems = append(problems, &FieldError{Field: "tzOffset", Message: "must be between -840 and 840 minutes"})
	}
	return collect(problems)
}

// Normalized returns a trimmed copy. Call after Validate.
func (in CheckinInput) Normalized() CheckinInput {
	return CheckinInput{
		Value:    in.Value,
		Note:     strings.TrimSpace(in.Note),
		Mood:     in.Mood,
		Date:     strings.TrimSpace(in.Date),
		TZOffset: in.TZOffset,
	}
}

// Skip excuses one calendar day: it does not break a streak and does not count
// as missed. For week/month/window schedules a skip anywhere inside the period
// excuses the whole period, which is the forgiving reading of "excused day".
type Skip struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"projectId"`
	Date      string    `json:"date"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

// SkipInput is the payload for excusing a day.
type SkipInput struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

// Validate reports every problem with the input.
func (in SkipInput) Validate(startDate, today string) error {
	var problems []*FieldError
	if err := validateDate("date", in.Date); err != nil {
		problems = append(problems, err)
	} else {
		if in.Date < startDate {
			problems = append(problems, &FieldError{Field: "date", Message: "must not be before the project start date"})
		}
		if in.Date > AddDays(today, 1) {
			problems = append(problems, &FieldError{Field: "date", Message: "must not be in the future"})
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(in.Reason)) > MaxReasonLength {
		problems = append(problems, &FieldError{Field: "reason", Message: fmt.Sprintf("must be at most %d characters", MaxReasonLength)})
	}
	return collect(problems)
}

// Normalized returns a trimmed copy. Call after Validate.
func (in SkipInput) Normalized() SkipInput {
	return SkipInput{Date: strings.TrimSpace(in.Date), Reason: strings.TrimSpace(in.Reason)}
}

// ProjectQuery filters a project listing. Visibility is enforced here rather
// than in handlers so no code path can forget it.
type ProjectQuery struct {
	// VisibleOnly hides private projects (anonymous visitors).
	VisibleOnly bool
	// IncludeArchived keeps archived projects in the result (admin only).
	IncludeArchived bool
	// CategoryID, when set, keeps only projects in that category.
	CategoryID string
	// Search is a case-insensitive substring match on name and description.
	Search string
}

// Matches reports whether p satisfies the filter. SQL listings push the same
// predicates into the WHERE clause; this is the in-memory twin used by tests
// and by the overview's category roll-up.
func (q ProjectQuery) Matches(p Project) bool {
	if q.VisibleOnly && p.Private {
		return false
	}
	if !q.IncludeArchived && p.Archived {
		return false
	}
	if q.CategoryID != "" && p.CategoryID.String() != q.CategoryID {
		return false
	}
	if q.Search != "" {
		needle := strings.ToLower(q.Search)
		if !strings.Contains(strings.ToLower(p.Name), needle) &&
			!strings.Contains(strings.ToLower(p.Description), needle) {
			return false
		}
	}
	return true
}

// CheckinQuery filters a check-in history listing.
type CheckinQuery struct {
	ProjectID uuid.UUID
	From      string // inclusive YYYY-MM-DD, "" = no bound
	To        string // inclusive YYYY-MM-DD, "" = no bound
	Limit     int
}

// Normalize clamps the page size.
func (q *CheckinQuery) Normalize() {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.Limit = min(q.Limit, 200)
}

// DayAggregate is one calendar day's check-in roll-up, the input to every
// period computation.
type DayAggregate struct {
	Count int
	Value float64
}

func validateName(field, raw string) *FieldError {
	name := strings.TrimSpace(raw)
	if name == "" {
		return &FieldError{Field: field, Message: "is required"}
	}
	if n := utf8.RuneCountInString(name); n > MaxNameLength {
		return &FieldError{Field: field, Message: fmt.Sprintf("must be at most %d characters, got %d", MaxNameLength, n)}
	}
	return nil
}

func validateDescription(raw string) *FieldError {
	if n := utf8.RuneCountInString(strings.TrimSpace(raw)); n > MaxDescriptionLength {
		return &FieldError{Field: "description", Message: fmt.Sprintf("must be at most %d characters, got %d", MaxDescriptionLength, n)}
	}
	return nil
}

func validateIcon(raw string) *FieldError {
	if n := utf8.RuneCountInString(strings.TrimSpace(raw)); n > MaxIconLength {
		return &FieldError{Field: "icon", Message: fmt.Sprintf("must be at most %d characters", MaxIconLength)}
	}
	return nil
}

func validateColor(raw string) *FieldError {
	color := strings.TrimSpace(raw)
	if color != "" && !slices.Contains(InkColors, color) {
		return &FieldError{Field: "color", Message: "must be one of " + strings.Join(InkColors, ", ")}
	}
	return nil
}

func validateCategoryID(raw string) *FieldError {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if _, err := uuid.Parse(strings.TrimSpace(raw)); err != nil {
		return &FieldError{Field: "categoryId", Message: "must be a UUID or empty"}
	}
	return nil
}

func validateSchedule(s ScheduleType, target float64, unit string, weekdays []int, interval float64) *FieldError {
	if !ValidSchedule(s) {
		return &FieldError{Field: "schedule", Message: "must be one of daily, weekdays, weekly, monthly, interval_days, interval_hours"}
	}
	if utf8.RuneCountInString(strings.TrimSpace(unit)) > MaxUnitLength {
		return &FieldError{Field: "unit", Message: fmt.Sprintf("must be at most %d characters", MaxUnitLength)}
	}
	switch s {
	case ScheduleDaily, ScheduleWeekdays, ScheduleWeekly, ScheduleMonthly, ScheduleIntervalDays:
		if target <= 0 || target > MaxTargetValue {
			return &FieldError{Field: "target", Message: fmt.Sprintf("must be between 0 and %g", MaxTargetValue)}
		}
	}
	if s == ScheduleWeekdays {
		if len(weekdays) == 0 {
			return &FieldError{Field: "weekdays", Message: "must list at least one day"}
		}
		for _, d := range weekdays {
			if d < 1 || d > 7 {
				return &FieldError{Field: "weekdays", Message: "must contain days 1 (Monday) through 7 (Sunday)"}
			}
		}
	}
	if s == ScheduleIntervalHours && (interval < 1 || interval > MaxIntervalHours) {
		return &FieldError{Field: "interval", Message: fmt.Sprintf("must be 1..%d hours", MaxIntervalHours)}
	}
	if s == ScheduleIntervalDays && (interval < 1 || interval > MaxIntervalDays || interval != float64(int(interval))) {
		return &FieldError{Field: "interval", Message: fmt.Sprintf("must be a whole number of days, 1..%d", MaxIntervalDays)}
	}
	return nil
}

func validateStartDate(raw, today string) *FieldError {
	date := strings.TrimSpace(raw)
	if date == "" {
		return &FieldError{Field: "startDate", Message: "is required"}
	}
	if err := validateDate("startDate", date); err != nil {
		return err
	}
	if date < MinStartDate {
		return &FieldError{Field: "startDate", Message: "must not be before " + MinStartDate}
	}
	if today != "" && date > AddDays(today, 1) {
		return &FieldError{Field: "startDate", Message: "must not be in the future"}
	}
	return nil
}

func validateDate(field, raw string) *FieldError {
	t, err := time.Parse(DateLayout, strings.TrimSpace(raw))
	if err != nil || t.Format(DateLayout) != strings.TrimSpace(raw) {
		return &FieldError{Field: field, Message: "must be a date in YYYY-MM-DD form"}
	}
	return nil
}

// normalizeWeekdays sorts and de-duplicates, keeping the canonical 1..7 order.
func normalizeWeekdays(days []int) []int {
	out := slices.Clone(days)
	slices.Sort(out)
	return slices.Compact(out)
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
