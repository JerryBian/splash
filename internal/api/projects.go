package api

import (
	"net/http"
	"strconv"
	"strings"
	"uuid"

	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
)

// project is the wire representation of a project.
// web/src/api/types.ts mirrors this type. Change the two together.
type project struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Icon          string         `json:"icon"`
	Color         string         `json:"color"`
	CategoryID    string         `json:"categoryId"` // "" when uncategorized
	Nature        string         `json:"nature"`     // good | bad
	Schedule      string         `json:"schedule"`
	Mode          string         `json:"mode"`
	Target        float64        `json:"target"`
	Unit          string         `json:"unit"`
	Weekdays      []int          `json:"weekdays"`
	Interval      float64        `json:"interval"`
	TargetTime    string         `json:"targetTime"`
	ReminderTime  string         `json:"reminderTime"`
	StartDate     string         `json:"startDate"`
	Archived      bool           `json:"archived"`
	Pinned        bool           `json:"pinned"`
	Prerequisites []string       `json:"prerequisites"`
	CreatedAt     string         `json:"createdAt"`
	UpdatedAt     string         `json:"updatedAt"`
	Version       int            `json:"version"`
	Status        *projectStatus `json:"status,omitempty"`
}

// projectStatus mirrors store.Summary with instants rendered as RFC 3339.
type projectStatus struct {
	Status        string   `json:"status"`
	PeriodStart   string   `json:"periodStart"`
	PeriodEnd     string   `json:"periodEnd"`
	Value         float64  `json:"value"`
	Target        float64  `json:"target"`
	TodayValue    float64  `json:"todayValue"`
	Count         int      `json:"count"`
	Streak        int      `json:"streak"`
	BestStreak    int      `json:"bestStreak"`
	DueAt         string   `json:"dueAt,omitempty"`
	LastCheckinAt string   `json:"lastCheckinAt,omitempty"`
	Unmet         []string `json:"unmet"`
}

func toProjectStatus(s store.Summary) *projectStatus {
	out := &projectStatus{
		Status:      s.Status,
		PeriodStart: s.PeriodStart,
		PeriodEnd:   s.PeriodEnd,
		Value:       s.Value,
		Target:      s.Target,
		TodayValue:  s.TodayValue,
		Count:       s.Count,
		Streak:      s.Streak,
		BestStreak:  s.BestStreak,
		Unmet:       s.Unmet,
	}
	if out.Unmet == nil {
		out.Unmet = []string{}
	}
	if s.DueAt != nil {
		out.DueAt = formatTime(*s.DueAt)
	}
	if s.LastCheckinAt != nil {
		out.LastCheckinAt = formatTime(*s.LastCheckinAt)
	}
	return out
}

func toProject(p store.Project, status *store.Summary) project {
	out := project{
		ID:           p.ID.String(),
		Name:         p.Name,
		Description:  p.Description,
		Icon:         p.Icon,
		Color:        p.Color,
		Nature:       p.Nature,
		Schedule:     string(p.Schedule),
		Mode:         p.Mode,
		Target:       p.Target,
		Unit:         p.Unit,
		Weekdays:     p.Weekdays,
		Interval:     p.Interval,
		TargetTime:   p.TargetTime,
		ReminderTime: p.ReminderTime,
		StartDate:    p.StartDate,
		Archived:     p.Archived,
		Pinned:       p.Pinned,
		CreatedAt:    formatTime(p.CreatedAt),
		UpdatedAt:    formatTime(p.UpdatedAt),
		Version:      p.Version,
	}
	if p.CategoryID != uuid.Nil() {
		out.CategoryID = p.CategoryID.String()
	}
	if out.Weekdays == nil {
		out.Weekdays = []int{}
	}
	out.Prerequisites = make([]string, 0, len(p.Prerequisites))
	for _, id := range p.Prerequisites {
		out.Prerequisites = append(out.Prerequisites, id.String())
	}
	if status != nil {
		out.Status = toProjectStatus(*status)
	}
	return out
}

type listProjectsResponse struct {
	Projects []project `json:"projects"`
	Today    string    `json:"today"`
}

// handleListProjects serves GET /api/v1/projects (administrator).
//
// Every project carries its current status (progress, streak, countdown), so
// the Today page is one request. Query parameters:
//
//	date      the client's calendar day, YYYY-MM-DD (default: server's)
//	archived  "1" includes archived projects
//	category  filter by category id
//	q         substring match on name and description
func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	today, ok := a.today(w, r)
	if !ok {
		return
	}
	q := store.ProjectQuery{
		IncludeArchived: r.URL.Query().Get("archived") == "1",
		CategoryID:      strings.TrimSpace(r.URL.Query().Get("category")),
		Search:          r.URL.Query().Get("q"),
	}
	projects, err := a.projects.List(r.Context(), q)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := make([]project, len(projects))
	for i, p := range projects {
		stats, err := a.stats.ForProject(r.Context(), p, today, 0)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		out[i] = toProject(p, &stats.Summary)
	}
	a.write(w, r, http.StatusOK, listProjectsResponse{Projects: out, Today: today})
}

// projectRequest is the POST body.
type projectRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Icon          string   `json:"icon"`
	Color         string   `json:"color"`
	CategoryID    string   `json:"categoryId"`
	Nature        string   `json:"nature"`
	Schedule      string   `json:"schedule"`
	Mode          string   `json:"mode"`
	Target        float64  `json:"target"`
	Unit          string   `json:"unit"`
	Weekdays      []int    `json:"weekdays"`
	Interval      float64  `json:"interval"`
	TargetTime    string   `json:"targetTime"`
	ReminderTime  string   `json:"reminderTime"`
	StartDate     string   `json:"startDate"`
	Pinned        bool     `json:"pinned"`
	Prerequisites []string `json:"prerequisites"`
}

// handleCreateProject serves POST /api/v1/projects (administrator).
func (a *API) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeJSON[projectRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	created, err := a.projects.Create(r.Context(), store.ProjectInput{
		Name:          req.Name,
		Description:   req.Description,
		Icon:          req.Icon,
		Color:         req.Color,
		CategoryID:    req.CategoryID,
		Nature:        req.Nature,
		Schedule:      store.ScheduleType(req.Schedule),
		Mode:          req.Mode,
		Target:        req.Target,
		Unit:          req.Unit,
		Weekdays:      req.Weekdays,
		Interval:      req.Interval,
		TargetTime:    req.TargetTime,
		ReminderTime:  req.ReminderTime,
		StartDate:     req.StartDate,
		Pinned:        req.Pinned,
		Prerequisites: req.Prerequisites,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Location", RoutePrefix+"/projects/"+created.ID.String())
	w.Header().Set("ETag", created.ETag())
	a.write(w, r, http.StatusCreated, toProject(created, nil))
}

// handleGetProject serves GET /api/v1/projects/{id}, with its current status.
func (a *API) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	today, ok := a.today(w, r)
	if !ok {
		return
	}
	stats, err := a.stats.ForProject(r.Context(), p, today, 0)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	etag := p.ETag()
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	a.write(w, r, http.StatusOK, toProject(p, &stats.Summary))
}

// projectPatchRequest is the PATCH body. Pointers distinguish "absent" from
// "set to the zero value"; categoryId is a *string so "" can clear it.
type projectPatchRequest struct {
	Name          *string   `json:"name,omitempty"`
	Description   *string   `json:"description,omitempty"`
	Icon          *string   `json:"icon,omitempty"`
	Color         *string   `json:"color,omitempty"`
	CategoryID    *string   `json:"categoryId,omitempty"`
	Nature        *string   `json:"nature,omitempty"`
	Schedule      *string   `json:"schedule,omitempty"`
	Mode          *string   `json:"mode,omitempty"`
	Target        *float64  `json:"target,omitempty"`
	Unit          *string   `json:"unit,omitempty"`
	Weekdays      *[]int    `json:"weekdays,omitempty"`
	Interval      *float64  `json:"interval,omitempty"`
	TargetTime    *string   `json:"targetTime,omitempty"`
	ReminderTime  *string   `json:"reminderTime,omitempty"`
	StartDate     *string   `json:"startDate,omitempty"`
	Archived      *bool     `json:"archived,omitempty"`
	Pinned        *bool     `json:"pinned,omitempty"`
	Prerequisites *[]string `json:"prerequisites,omitempty"`
}

func (req projectPatchRequest) toPatch() store.ProjectPatch {
	patch := store.ProjectPatch{
		Name:          req.Name,
		Description:   req.Description,
		Icon:          req.Icon,
		Color:         req.Color,
		CategoryID:    req.CategoryID,
		Nature:        req.Nature,
		Mode:          req.Mode,
		Target:        req.Target,
		Unit:          req.Unit,
		Weekdays:      req.Weekdays,
		Interval:      req.Interval,
		TargetTime:    req.TargetTime,
		ReminderTime:  req.ReminderTime,
		StartDate:     req.StartDate,
		Archived:      req.Archived,
		Pinned:        req.Pinned,
		Prerequisites: req.Prerequisites,
	}
	if req.Schedule != nil {
		s := store.ScheduleType(*req.Schedule)
		patch.Schedule = &s
	}
	return patch
}

// handleUpdateProject serves PATCH /api/v1/projects/{id} (admin).
//
// If-Match, when present, is parsed into a version and enforced inside the
// store's UPDATE ... WHERE version = ? — not by a check-then-write gap here.
func (a *API) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[projectPatchRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	expectedVersion, err := parseIfMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	updated, err := a.projects.Update(r.Context(), p.ID, req.toPatch(), expectedVersion)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", updated.ETag())
	a.write(w, r, http.StatusOK, toProject(updated, nil))
}

// handleDeleteProject serves DELETE /api/v1/projects/{id} (admin). Check-ins
// and skips cascade.
func (a *API) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	expectedVersion, err := parseIfMatchVersion(r.Header.Get("If-Match"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if expectedVersion != nil && *expectedVersion != p.Version {
		w.Header().Set("ETag", p.ETag())
		a.fail(w, r, httpx.Errorf(http.StatusPreconditionFailed, httpx.CodePrecondition,
			"the project changed since you read it; fetch it again before deleting"))
		return
	}
	if err := a.projects.Delete(r.Context(), p.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parseIfMatchVersion turns an If-Match header into the version it selects.
// "" and "*" mean "no precondition"; anything else must be a "vN" revision tag.
func parseIfMatchVersion(header string) (*int, error) {
	header = strings.TrimSpace(header)
	if header == "" || header == "*" {
		return nil, nil
	}
	tag := normalizeETag(header)
	if !strings.HasPrefix(tag, "v") {
		return nil, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"If-Match must be a revision tag like \"v3\"")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(tag, "v"))
	if err != nil || n < 1 {
		return nil, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"If-Match must be a revision tag like \"v3\"")
	}
	return &n, nil
}
