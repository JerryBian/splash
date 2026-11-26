package api

import (
	"net/http"
	"strconv"

	"github.com/JerryBian/splash/internal/config"
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
	p, ok := a.loadProject(w, r)
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
// dashboard over every active project, with good and bad habits tallied apart.
func (a *API) handleOverview(w http.ResponseWriter, r *http.Request) {
	today, ok := a.today(w, r)
	if !ok {
		return
	}
	projects, err := a.projects.List(r.Context(), store.ProjectQuery{})
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
// as a download. POST /import restores the same shape additively; the two are
// deliberately separate routes so a backup can be taken through a browser link.
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	data, err := a.export.Dump(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="splash-export.json"`)
	a.write(w, r, http.StatusOK, data)
}

// maxImportBytes is the body cap for POST /import. It uses the configuration's
// hard ceiling rather than the per-route default: a backup carries proof photos
// as base64, and the 1 MiB default would reject even a handful of pictures.
// Ordinary routes keep the configured limit, so the hard ceiling still bounds
// the largest request the service will ever accept.
const maxImportBytes = config.MaxAllowedRequestBodyBytes

// handleImport serves POST /api/v1/import (admin): restore a JSON backup. The
// body is the same shape GET /export produces. Import is additive by id, so it
// never overwrites existing data and re-importing a backup is a no-op.
func (a *API) handleImport(w http.ResponseWriter, r *http.Request) {
	data, err := httpx.DecodeJSON[store.ExportData](w, r, maxImportBytes)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	counts, err := a.importer.Import(r.Context(), data)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.write(w, r, http.StatusOK, counts)
}
