package api

import (
	"net/http"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
)

// category is the wire representation of a category.
// web/src/api/types.ts mirrors this type. Change the two together.
type category struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
}

func toCategory(c store.Category) category {
	return category{
		ID:        c.ID.String(),
		Name:      c.Name,
		Icon:      c.Icon,
		Color:     c.Color,
		CreatedAt: formatTime(c.CreatedAt),
	}
}

type listCategoriesResponse struct {
	Categories []category `json:"categories"`
}

// handleListCategories serves GET /api/v1/categories.
//
// Anonymous visitors see only categories that hold at least one public
// project: an empty category could otherwise hint at hidden work.
func (a *API) handleListCategories(w http.ResponseWriter, r *http.Request) {
	all, err := a.categories.List(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if auth.IsAdmin(r.Context()) {
		out := make([]category, len(all))
		for i, c := range all {
			out[i] = toCategory(c)
		}
		a.write(w, r, http.StatusOK, listCategoriesResponse{Categories: out})
		return
	}

	visible, err := a.projects.List(r.Context(), store.ProjectQuery{VisibleOnly: true})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	used := map[string]bool{}
	for _, p := range visible {
		used[p.CategoryID.String()] = true
	}
	out := []category{}
	for _, c := range all {
		if used[c.ID.String()] {
			out = append(out, toCategory(c))
		}
	}
	a.write(w, r, http.StatusOK, listCategoriesResponse{Categories: out})
}

// categoryRequest is the POST/PATCH body: a full replacement of the three
// editable fields, which is all a category has.
type categoryRequest struct {
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// handleCreateCategory serves POST /api/v1/categories (admin).
func (a *API) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	req, err := httpx.DecodeJSON[categoryRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	created, err := a.categories.Create(r.Context(), store.CategoryInput{
		Name: req.Name, Icon: req.Icon, Color: req.Color,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Location", RoutePrefix+"/categories/"+created.ID.String())
	a.write(w, r, http.StatusCreated, toCategory(created))
}

// handleUpdateCategory serves PATCH /api/v1/categories/{id} (admin).
func (a *API) handleUpdateCategory(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := parseUUID(r.PathValue("id"), "category id")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	req, err := httpx.DecodeJSON[categoryRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	updated, err := a.categories.Update(r.Context(), id, store.CategoryInput{
		Name: req.Name, Icon: req.Icon, Color: req.Color,
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.write(w, r, http.StatusOK, toCategory(updated))
}

// handleDeleteCategory serves DELETE /api/v1/categories/{id} (admin).
// Projects in the category survive, uncategorized.
func (a *API) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	id, err := parseUUID(r.PathValue("id"), "category id")
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.categories.Delete(r.Context(), id); err != nil {
		a.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
