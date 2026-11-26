package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
)

// checkin is the wire representation of one check-in.
// web/src/api/types.ts mirrors this type. Change the two together.
type checkin struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"projectId"`
	Value       float64  `json:"value"`
	Note        string   `json:"note,omitempty"`
	Mood        string   `json:"mood,omitempty"`
	HasPhoto    bool     `json:"hasPhoto"`
	LocalDate   string   `json:"localDate"`
	CheckedAt   string   `json:"checkedAt"`
	TZOffset    int      `json:"tzOffset"`
	Backfilled  bool     `json:"backfilled"`
	Violated    bool     `json:"violated"`
	Unmet       []string `json:"unmet"`
	PostUnmet   []string `json:"postUnmet"`
	PostPending []string `json:"postPending"`
	CreatedAt   string   `json:"createdAt"`
}

func toCheckin(c store.Checkin) checkin {
	unmet := make([]string, 0, len(c.Unmet))
	for _, id := range c.Unmet {
		unmet = append(unmet, id.String())
	}
	postUnmet := make([]string, 0, len(c.PostUnmet))
	for _, id := range c.PostUnmet {
		postUnmet = append(postUnmet, id.String())
	}
	postPending := make([]string, 0, len(c.PostPending))
	for _, id := range c.PostPending {
		postPending = append(postPending, id.String())
	}
	return checkin{
		ID:          c.ID.String(),
		ProjectID:   c.ProjectID.String(),
		Value:       c.Value,
		Note:        c.Note,
		Mood:        c.Mood,
		HasPhoto:    c.HasPhoto,
		LocalDate:   c.LocalDate,
		CheckedAt:   formatTime(c.CheckedAt),
		TZOffset:    c.TZOffset,
		Backfilled:  c.Backfilled,
		Violated:    c.Violated,
		Unmet:       unmet,
		PostUnmet:   postUnmet,
		PostPending: postPending,
		CreatedAt:   formatTime(c.CreatedAt),
	}
}

// checkinRequest is the POST body. date empty means today; a past date is a
// backfill. value is in the project's unit. photo is an optional image data URL
// (400 KB cap), never echoed back in bulk responses.
type checkinRequest struct {
	Value    float64 `json:"value"`
	Note     string  `json:"note"`
	Mood     string  `json:"mood"`
	Photo    string  `json:"photo"`
	Date     string  `json:"date"`
	TZOffset int     `json:"tzOffset"`
}

// handleCreateCheckin serves POST /api/v1/projects/{id}/checkins (admin).
func (a *API) handleCreateCheckin(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[checkinRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	created, err := a.checkins.Create(r.Context(), p.ID, store.CheckinInput{
		Value:    req.Value,
		Note:     req.Note,
		Mood:     req.Mood,
		Photo:    req.Photo,
		Date:     req.Date,
		TZOffset: req.TZOffset,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.write(w, r, http.StatusCreated, toCheckin(created))
}

type listCheckinsResponse struct {
	Checkins []checkin `json:"checkins"`
}

// handleListCheckins serves GET /api/v1/projects/{id}/checkins.
// Parameters: from, to (YYYY-MM-DD bounds on the local date), limit (1..200).
func (a *API) handleListCheckins(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	q := store.CheckinQuery{
		ProjectID: p.ID,
		From:      strings.TrimSpace(r.URL.Query().Get("from")),
		To:        strings.TrimSpace(r.URL.Query().Get("to")),
	}
	for _, bound := range []string{q.From, q.To} {
		if bound != "" {
			if _, err := store.ParseDate(bound); err != nil {
				a.fail(w, r, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
					"from and to must be YYYY-MM-DD"))
				return
			}
		}
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			a.fail(w, r, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
				"limit must be a non-negative integer"))
			return
		}
		q.Limit = n
	}
	rows, err := a.checkins.List(r.Context(), q)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := make([]checkin, len(rows))
	for i, c := range rows {
		out[i] = toCheckin(c)
	}
	a.write(w, r, http.StatusOK, listCheckinsResponse{Checkins: out})
}

// handleDeleteCheckin serves DELETE /api/v1/checkins/{id} (admin): undo one
// check-in. The project's stats recompute on the next read; nothing is cached.
func (a *API) handleDeleteCheckin(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"), "checkin id")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.checkins.Delete(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCheckinPhoto serves GET /api/v1/checkins/{id}/photo (admin): the
// proof photo's raw bytes. The payload is deliberately not embedded in any list
// response — a page of history would otherwise carry megabytes of base64 — so
// the UI points an <img> at this URL and the browser caches the immutable image.
func (a *API) handleCheckinPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r.PathValue("id"), "checkin id")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	dataURL, err := a.checkins.Photo(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	mime, data, err := store.DecodePhoto(dataURL)
	if err != nil {
		// A stored row that no longer decodes is corruption, not a client error.
		a.fail(w, r, fmt.Errorf("checkin %s photo: %w", id, err))
		return
	}
	// The id never gets a second image, so the bytes are immutable. private
	// keeps shared caches out; nosniff stops a corrupt row from being guessed
	// as HTML.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	httpx.Write(w, http.StatusOK, mime, data)
}

// skip is the wire representation of an excused day.
type skip struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectId"`
	Date      string `json:"date"`
	Reason    string `json:"reason,omitempty"`
	CreatedAt string `json:"createdAt"`
}

func toSkip(s store.Skip) skip {
	return skip{
		ID:        s.ID.String(),
		ProjectID: s.ProjectID.String(),
		Date:      s.Date,
		Reason:    s.Reason,
		CreatedAt: formatTime(s.CreatedAt),
	}
}

type listSkipsResponse struct {
	Skips []skip `json:"skips"`
}

// handleListSkips serves GET /api/v1/projects/{id}/skips.
func (a *API) handleListSkips(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	rows, err := a.skips.List(r.Context(), p.ID,
		r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	out := make([]skip, len(rows))
	for i, s := range rows {
		out[i] = toSkip(s)
	}
	a.write(w, r, http.StatusOK, listSkipsResponse{Skips: out})
}

// skipRequest is the POST body.
type skipRequest struct {
	Date   string `json:"date"`
	Reason string `json:"reason"`
}

// handleCreateSkip serves POST /api/v1/projects/{id}/skips (admin): excuse a
// day so it neither counts as missed nor breaks the streak.
func (a *API) handleCreateSkip(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeJSON[skipRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	created, err := a.skips.Create(r.Context(), p.ID, store.SkipInput{Date: req.Date, Reason: req.Reason})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.write(w, r, http.StatusCreated, toSkip(created))
}

// handleDeleteSkip serves DELETE /api/v1/projects/{id}/skips/{date} (admin).
func (a *API) handleDeleteSkip(w http.ResponseWriter, r *http.Request) {
	p, ok := a.loadProject(w, r)
	if !ok {
		return
	}
	date := r.PathValue("date")
	if _, err := store.ParseDate(date); err != nil {
		a.fail(w, r, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"date must be YYYY-MM-DD"))
		return
	}
	if err := a.skips.Delete(r.Context(), p.ID, date); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
