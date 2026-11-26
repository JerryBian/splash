// Package api implements the HTTP surface: route registration, request
// parsing, delegation to the domain and response encoding.
//
// Handlers are deliberately thin. Anything that looks like a business rule
// belongs in internal/store (or a domain package beside it), because a rule
// that lives in a handler can only be tested through HTTP.
//
// Access model: the whole service sits behind one administrator account
// (credentials from configuration). Every data route — reads included — is
// wrapped by guard, which answers 401 before any work happens (the destructive
// reset uses guardReset, the same gate plus the exclusive reset lock). Only the
// auth door (login/logout/me) and the operational probes (health/ready/version)
// are reachable without a session. There is no anonymous data access and no
// per-project visibility: a project either exists for the admin or it does not.
package api

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/config"
	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
	"github.com/JerryBian/splash/internal/version"
)

// RoutePrefix is the versioned API root. Bumping it to /api/v2 is the moment to
// keep the v1 handlers around rather than edit them in place.
const RoutePrefix = "/api/v1"

// Deps are the collaborators the handler set needs. Everything is injected, so
// a test can build an API over a real SQLite database in memory and a fixed
// clock without touching the network.
type Deps struct {
	// The storage boundaries. Nil is a programming error.
	Categories store.CategoryStore
	Projects   store.ProjectStore
	Checkins   store.CheckinStore
	Skips      store.SkipStore
	Stats      store.StatsStore
	Export     store.Exporter
	Import     store.Importer
	// Reset wipes the database and returns the number of demo projects seeded.
	// The server supplies it so the API does not need to know the DB path or
	// whether demo seeding is enabled.
	Reset func(ctx context.Context) (int, error)
	// Auth checks credentials, signs sessions and reads the request role.
	Auth *auth.Authenticator
	// LoginLimiter throttles login attempts per IP. Nil becomes a default
	// (10 attempts per 15 minutes).
	LoginLimiter *auth.Limiter
	// Log receives request-scoped diagnostics. Nil becomes a discard logger.
	Log *slog.Logger
	// MaxRequestBodyBytes caps JSON request bodies.
	MaxRequestBodyBytes int64
	// Clock supplies "now". Nil becomes [time.Now].
	Clock func() time.Time
	// StartedAt is when the process began serving; the health endpoint reports
	// uptime from it. Zero becomes the time New is called.
	StartedAt time.Time
	// Environment is echoed by the version endpoint so an operator can confirm
	// which configuration a deployment actually loaded.
	Environment config.Environment
	// RepoURL is the project homepage, reported by the version endpoint.
	RepoURL string
}

// API is the set of HTTP handlers plus their dependencies.
type API struct {
	categories store.CategoryStore
	projects   store.ProjectStore
	checkins   store.CheckinStore
	skips      store.SkipStore
	stats      store.StatsStore
	export     store.Exporter
	importer   store.Importer
	reset      func(ctx context.Context) (int, error)
	authn      *auth.Authenticator
	limiter    *auth.Limiter
	log        *slog.Logger
	maxBody    int64
	clock      func() time.Time
	started    time.Time
	env        config.Environment
	repoURL    string
	info       version.Info

	// dbMu keeps the destructive reset exclusive: every data handler holds a
	// read lock, and only the reset handler upgrades to a write lock. Without
	// it, a concurrent query could hit the pool while Reset closes it.
	dbMu sync.RWMutex

	// routes records the patterns Register installed so the catch-all can tell
	// "unknown path" apart from "known path, wrong method". See handleUnmatched.
	routes []routePattern
}

// New builds the handler set. It panics when a required dependency is nil: a
// handler set that cannot serve is better discovered at startup than on the
// first request.
func New(deps Deps) *API {
	if deps.Categories == nil || deps.Projects == nil || deps.Checkins == nil ||
		deps.Skips == nil || deps.Stats == nil || deps.Export == nil || deps.Import == nil ||
		deps.Reset == nil || deps.Auth == nil {
		panic("api: Deps requires Categories, Projects, Checkins, Skips, Stats, Export, Import, Reset and Auth")
	}
	log := deps.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}
	started := deps.StartedAt
	if started.IsZero() {
		started = clock()
	}
	maxBody := deps.MaxRequestBodyBytes
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	env := deps.Environment
	if env == "" {
		env = config.Development
	}
	limiter := deps.LoginLimiter
	if limiter == nil {
		limiter = auth.NewLimiter(10, 15*time.Minute, clock)
	}
	return &API{
		categories: deps.Categories,
		projects:   deps.Projects,
		checkins:   deps.Checkins,
		skips:      deps.Skips,
		stats:      deps.Stats,
		export:     deps.Export,
		importer:   deps.Import,
		reset:      deps.Reset,
		authn:      deps.Auth,
		limiter:    limiter,
		log:        log,
		maxBody:    maxBody,
		clock:      clock,
		started:    started,
		env:        env,
		repoURL:    deps.RepoURL,
		info:       version.Get(),
	}
}

// Register installs every route on mux.
//
// Patterns carry their method, which is what net/http's ServeMux has done since
// Go 1.22: the mux dispatches on method and returns 405 for a known path with an
// unregistered method, and {id} is read back with r.PathValue("id").
func (a *API) Register(mux *http.ServeMux) {
	// Ungated: operational probes and the auth door itself.
	a.addRoute(mux, "GET "+RoutePrefix+"/health", a.handleHealth)
	a.addRoute(mux, "GET "+RoutePrefix+"/health/ready", a.handleReady)
	a.addRoute(mux, "GET "+RoutePrefix+"/version", a.handleVersion)
	a.addRoute(mux, "POST "+RoutePrefix+"/auth/login", a.handleLogin)
	a.addRoute(mux, "POST "+RoutePrefix+"/auth/logout", a.handleLogout)
	a.addRoute(mux, "GET "+RoutePrefix+"/auth/me", a.handleMe)

	// Everything else is administrator-only, reads included. guard enforces it
	// once at the edge so no handler can forget the check.
	a.addRoute(mux, "GET "+RoutePrefix+"/categories", a.guard(a.handleListCategories))
	a.addRoute(mux, "POST "+RoutePrefix+"/categories", a.guard(a.handleCreateCategory))
	a.addRoute(mux, "PATCH "+RoutePrefix+"/categories/{id}", a.guard(a.handleUpdateCategory))
	a.addRoute(mux, "DELETE "+RoutePrefix+"/categories/{id}", a.guard(a.handleDeleteCategory))

	a.addRoute(mux, "GET "+RoutePrefix+"/projects", a.guard(a.handleListProjects))
	a.addRoute(mux, "POST "+RoutePrefix+"/projects", a.guard(a.handleCreateProject))
	a.addRoute(mux, "GET "+RoutePrefix+"/projects/{id}", a.guard(a.handleGetProject))
	a.addRoute(mux, "PATCH "+RoutePrefix+"/projects/{id}", a.guard(a.handleUpdateProject))
	a.addRoute(mux, "DELETE "+RoutePrefix+"/projects/{id}", a.guard(a.handleDeleteProject))

	a.addRoute(mux, "GET "+RoutePrefix+"/projects/{id}/checkins", a.guard(a.handleListCheckins))
	a.addRoute(mux, "POST "+RoutePrefix+"/projects/{id}/checkins", a.guard(a.handleCreateCheckin))
	a.addRoute(mux, "GET "+RoutePrefix+"/checkins/{id}/photo", a.guard(a.handleCheckinPhoto))
	a.addRoute(mux, "DELETE "+RoutePrefix+"/checkins/{id}", a.guard(a.handleDeleteCheckin))

	a.addRoute(mux, "GET "+RoutePrefix+"/projects/{id}/skips", a.guard(a.handleListSkips))
	a.addRoute(mux, "POST "+RoutePrefix+"/projects/{id}/skips", a.guard(a.handleCreateSkip))
	a.addRoute(mux, "DELETE "+RoutePrefix+"/projects/{id}/skips/{date}", a.guard(a.handleDeleteSkip))

	a.addRoute(mux, "GET "+RoutePrefix+"/projects/{id}/stats", a.guard(a.handleProjectStats))
	a.addRoute(mux, "GET "+RoutePrefix+"/stats/overview", a.guard(a.handleOverview))
	a.addRoute(mux, "GET "+RoutePrefix+"/export", a.guard(a.handleExport))
	a.addRoute(mux, "POST "+RoutePrefix+"/import", a.guard(a.handleImport))
	a.addRoute(mux, "POST "+RoutePrefix+"/admin/reset", a.guardReset(a.handleReset))

	// Catch-all for the API subtree. Without it an unknown /api/... path would
	// fall through to the frontend handler mounted at "/" and be answered with
	// index.html and a 200 — the single most confusing failure mode of an
	// embedded SPA. Registered last because the mux matches by specificity, not
	// registration order, and this reads better at the bottom.
	mux.HandleFunc("/api/", a.handleUnmatched)
}

// addRoute registers a handler and records the pattern for handleUnmatched.
func (a *API) addRoute(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, handler)
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		method, path = "", pattern
	}
	a.routes = append(a.routes, routePattern{
		method:   method,
		segments: splitPath(path),
	})
}

// routePattern is a registered route, kept in a form that can be matched against
// a request path segment by segment.
type routePattern struct {
	method   string
	segments []string
}

// methodsForPath returns the methods registered for an exact path, treating
// {wildcard} segments as matching anything. GET implies HEAD, which is what
// net/http does when it dispatches.
func (a *API) methodsForPath(path string) []string {
	segments := splitPath(path)
	var methods []string
	for _, rp := range a.routes {
		if rp.method == "" || !segmentsMatch(rp.segments, segments) {
			continue
		}
		methods = append(methods, rp.method)
		if rp.method == http.MethodGet {
			methods = append(methods, http.MethodHead)
		}
	}
	slices.Sort(methods)
	return slices.Compact(methods)
}

func segmentsMatch(pattern, path []string) bool {
	if len(pattern) != len(path) {
		return false
	}
	for i, want := range pattern {
		if len(want) > 2 && strings.HasPrefix(want, "{") && strings.HasSuffix(want, "}") {
			continue // wildcard: matches any single segment
		}
		if want != path[i] {
			return false
		}
	}
	return true
}

// splitPath breaks a URL path into non-empty segments.
func splitPath(path string) []string {
	raw := strings.Split(path, "/")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// handleUnmatched answers every /api/ request that no route claimed, with the
// correct distinction between 404 and 405.
func (a *API) handleUnmatched(w http.ResponseWriter, r *http.Request) {
	if allowed := a.methodsForPath(r.URL.Path); len(allowed) > 0 {
		allow := strings.Join(allowed, ", ")
		w.Header().Set("Allow", allow)
		a.fail(w, r, httpx.Errorf(http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed,
			"%s is not allowed on %s; allowed methods: %s", r.Method, r.URL.Path, allow))
		return
	}
	a.fail(w, r, httpx.Errorf(http.StatusNotFound, httpx.CodeNotFound,
		"no route for %s %s", r.Method, r.URL.Path))
}

// write encodes a success response, logging the rare case where the write fails
// (which almost always means the client hung up mid-response).
func (a *API) write(w http.ResponseWriter, r *http.Request, status int, v any) {
	if err := httpx.WriteJSON(w, status, v); err != nil {
		a.log.DebugContext(r.Context(), "response write failed",
			slog.String("requestId", httpx.RequestIDFrom(r.Context())),
			slog.String("path", r.URL.Path),
			slog.Any("error", err),
		)
	}
}

// requireAdmin answers 401 itself and returns false when the request is not
// the administrator, so callers read `if !a.requireAdmin(w, r) { return }`.
func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if auth.IsAdmin(r.Context()) {
		return true
	}
	a.fail(w, r, httpx.Errorf(http.StatusUnauthorized, httpx.CodeUnauthorized,
		"sign in as the administrator to do this"))
	return false
}

// guard wraps a data handler with the administrator gate. Register applies it
// to every route except the auth door and the probes, so reads and writes are
// protected by construction rather than by each handler remembering to check.
//
// The read lock keeps ordinary requests from observing the database while the
// reset handler has it closed; reset is the only writer of dbMu.
func (a *API) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireAdmin(w, r) {
			return
		}
		a.dbMu.RLock()
		defer a.dbMu.RUnlock()
		next(w, r)
	}
}

// guardReset is guard's exclusive counterpart: the reset handler closes and
// replaces the database, so it must be the only request touching it.
func (a *API) guardReset(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.requireAdmin(w, r) {
			return
		}
		a.dbMu.Lock()
		defer a.dbMu.Unlock()
		next(w, r)
	}
}

// loadProject parses the path id and loads the project. Access is already
// enforced by guard; this only turns a missing or malformed id into the right
// error before any work happens.
func (a *API) loadProject(w http.ResponseWriter, r *http.Request) (store.Project, bool) {
	id, err := parseUUID(r.PathValue("id"), "project id")
	if err != nil {
		a.fail(w, r, err)
		return store.Project{}, false
	}
	p, err := a.projects.Get(r.Context(), id)
	if err != nil {
		a.fail(w, r, err)
		return store.Project{}, false
	}
	return p, true
}

// today reads the client's calendar day (?date=YYYY-MM-DD), falling back to the
// server's. The client's day is what period computations should use: a user in
// UTC+8 at 00:30 is already in tomorrow relative to a UTC server.
func (a *API) today(w http.ResponseWriter, r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("date"))
	if raw == "" {
		return store.DateKey(a.clock()), true
	}
	if _, err := store.ParseDate(raw); err != nil {
		a.fail(w, r, httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"date must be YYYY-MM-DD"))
		return "", false
	}
	return raw, true
}

// parseUUID reads a path value as a UUID, with a 400 on garbage.
func parseUUID(raw, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil(), httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"%q is not a valid %s; expected a UUID", raw, what).WithErr(err)
	}
	return id, nil
}

// clientIP extracts the host part of RemoteAddr for rate limiting.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// formatTime renders an RFC 3339 timestamp in UTC. RFC3339Nano trims trailing
// zeros, so a whole second comes out as "...:05Z" and a millisecond one as
// "...:05.123Z"; JavaScript's Date parses both.
func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
