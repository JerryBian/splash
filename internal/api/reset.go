package api

import (
	"log/slog"
	"net/http"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/httpx"
)

// resetRequest is the destructive confirmation: a valid session is not enough,
// the administrator must re-enter the configured password.
type resetRequest struct {
	Password string `json:"password"`
}

// resetResponse reports the wipe. SeededProjects lets the UI explain whether
// the configured demo data came back.
type resetResponse struct {
	Reset          bool `json:"reset"`
	SeededProjects int  `json:"seededProjects"`
}

// handleReset deletes the database and recreates it, optionally re-seeding the
// demo. It is the only route that closes the store, so Register wraps it in
// guardReset instead of guard: all ordinary requests hold a read lock and this
// one holds the write lock for the whole destructive operation.
func (a *API) handleReset(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeJSON[resetRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	// A wrong password is an input problem, not an expired session: 422 keeps
	// the client's unauthorized handler from treating it as a logout.
	if req.Password == "" || !a.authn.Check(auth.UsernameFrom(r.Context()), req.Password) {
		detail := "is incorrect"
		if req.Password == "" {
			detail = "is required"
		}
		a.fail(w, r, httpx.Errorf(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"the administrator password is required to reset the database").
			WithDetails(httpx.FieldError{Field: "password", Message: detail}))
		return
	}

	seeded, err := a.reset(r.Context())
	if err != nil {
		a.fail(w, r, httpx.Errorf(http.StatusInternalServerError, httpx.CodeInternal,
			"could not reset the database").WithErr(err))
		return
	}

	a.log.WarnContext(r.Context(), "database reset",
		slog.Bool("reset", true),
		slog.Int("seededProjects", seeded),
	)
	a.write(w, r, http.StatusOK, resetResponse{Reset: true, SeededProjects: seeded})
}
