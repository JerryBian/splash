package api

import (
	"net/http"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/httpx"
)

// loginRequest is the POST /auth/login body.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// sessionResponse is what login, logout and me all answer: the caller's role.
type sessionResponse struct {
	Role     string `json:"role"`
	Username string `json:"username,omitempty"`
}

// handleLogin serves POST /api/v1/auth/login.
//
// Failures are deliberately uniform ("username or password is incorrect") and
// rate-limited per IP, so the endpoint is not an oracle for which half of the
// credential pair was wrong, nor a cheap place to guess the password.
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.Allow(clientIP(r)) {
		a.fail(w, r, httpx.Errorf(http.StatusTooManyRequests, httpx.CodeRateLimited,
			"too many login attempts; try again in a few minutes"))
		return
	}
	req, err := httpx.DecodeJSON[loginRequest](w, r, a.maxBody)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if !a.authn.Check(req.Username, req.Password) {
		a.fail(w, r, httpx.Errorf(http.StatusUnauthorized, httpx.CodeUnauthorized,
			"username or password is incorrect"))
		return
	}
	// A correct sign-in clears this IP's guess budget: the limiter exists to
	// stop password guessing, not to lock out the admin's own logins.
	a.limiter.Reset(clientIP(r))
	token, err := a.authn.Issue()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.authn.SetCookie(w, token)
	a.write(w, r, http.StatusOK, sessionResponse{Role: string(auth.RoleAdmin), Username: req.Username})
}

// handleLogout serves POST /api/v1/auth/logout. It succeeds even without a
// session, so a stale client can always reach a clean state.
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.authn.ClearCookie(w)
	a.write(w, r, http.StatusOK, sessionResponse{Role: string(auth.RoleAnonymous)})
}

// handleMe serves GET /api/v1/auth/me: who the request is, according to its
// session. The frontend bootstraps from this.
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	role := auth.RoleFrom(r.Context())
	a.write(w, r, http.StatusOK, sessionResponse{
		Role:     string(role),
		Username: auth.UsernameFrom(r.Context()),
	})
}
