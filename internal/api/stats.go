package api

import (
	"net/http"
	"strconv"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
)

// statsResponse is the project detail payload. Summary, Totals, Heatmap and
// Periods are pure-data store types with wire-ready JSON tags; Recent and
// Skips are converted to DTOs because they carry instants.
type statsResponse struct {
	Project project           `json:"project"`
	Summary store.Summary     `json:"summary"`
	Totals  store.Totals      `json:"totals"`
	Heatmap []store.HeatCell  `json:"heatmap"`
	Periods []store.PeriodBar `json:"periods"`
	Recent  []checkin         `json:"recent"`
	Skips   []skip            `json:"skips"`
}

// handleProjectStats serves GET /api/v1/projects/{id}/stats.
// weeks controls the heatmap length (1..53, default 26).
func (a *API) handleProjectStats(w http.ResponseWriter, r *http.Request) {
	p, ok := a.visibleProject(w, r)
	if !ok {
		return
	}
	today, ok := a.today(w, r)
	if !ok {
		return
	}
	weeks := 26
	if raw := r.URL.Query().Get("weeks"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 53 {
			a.fail(w, r, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
				"weeks must be 1..53"))
			return
		}
		weeks = n
	}
	stats, err := a.stats.ForProject(r.Context(), p, today, weeks)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	recent := make([]checkin, len(stats.Recent))
	for i, c := range stats.Recent {
		recent[i] = toCheckin(c)
	}
	skips := make([]skip, len(stats.Skips))
	for i, s := range stats.Skips {
		skips[i] = toSkip(s)
	}
	a.write(w, r, http.StatusOK, statsResponse{
		Project: toProject(p, &stats.Summary),
		Summary: stats.Summary,
		Totals:  stats.Totals,
		Heatmap: stats.Heatmap,
		Periods: stats.Periods,
		Recent:  recent,
		Skips:   skips,
	})
}

// handleOverview serves GET /api/v1/stats/overview: the cross-project
// dashboard, computed over exactly the projects the caller may see.
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	today, ok := a.today(w, r)
	if !ok {
		return
	}
	projects, err := a.projects.List(r.Context(), store.ProjectQuery{
		VisibleOnly: !auth.IsAdmin(r.Context()),
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	overview, err := a.stats.Overview(r.Context(), projects, today)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.write(w, r, http.StatusOK, overview)
}

// handleExport serves GET /api/v1/export (admin): a full JSON backup, offered
// as a download. Import is deliberately not offered — restoring is an
// operator action on the database file, not an HTTP write.
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	data, err := a.export.Dump(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="splash-export.json"`)
	a.write(w, r, http.StatusOK, data)
}
