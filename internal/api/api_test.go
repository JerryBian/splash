package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	json "encoding/json/v2"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/config"
	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
	"github.com/JerryBian/splash/internal/version"
)

var apiTestEpoch = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// manualClock keeps timestamps reproducible without freezing the process.
type manualClock struct {
	now time.Time
}

func (c *manualClock) Now() time.Time          { return c.now }
func (c *manualClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// testAPI is a running handler set over a real SQLite database in memory, so
// these tests exercise SQL, visibility and the schedule engine together.
type testAPI struct {
	srv     *httptest.Server
	client  *http.Client
	db      *store.DB
	clock   *manualClock
	maxBody int64
}

func newTestAPI(t *testing.T, maxBody int64) testAPI {
	t.Helper()
	if maxBody <= 0 {
		maxBody = 1 << 20
	}

	clock := &manualClock{now: apiTestEpoch}
	db, err := store.Open(":memory:", clock.Now)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	authn := auth.New(auth.Options{
		Username: "admin",
		Password: "splash123",
		Secret:   "test-secret-that-is-long-enough",
		TTL:      time.Hour,
		Clock:    clock.Now,
	})

	mux := http.NewServeMux()
	New(Deps{
		Categories: db.Categories(),
		Projects:   db.Projects(),
		Checkins:   db.Checkins(),
		Skips:      db.Skips(),
		Stats:      db.Stats(),
		Export:     db,
		Import:     db,
		Reset: func(ctx context.Context) (int, error) {
			return db.Reset(ctx, false)
		},
		Auth:                authn,
		LoginLimiter:        auth.NewLimiter(5, 15*time.Minute, clock.Now),
		Log:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxRequestBodyBytes: maxBody,
		Clock:               clock.Now,
		StartedAt:           clock.now.Add(-90 * time.Second),
		Environment:         config.Test,
		RepoURL:             version.RepoURL,
	}).Register(mux)

	// RequestID and the auth middleware are part of production wiring (see
	// internal/server) and are included here so error envelopes and roles are
	// exercised the same way the binary exercises them.
	srv := httptest.NewTestServer(t, httpx.Chain(mux, httpx.RequestID, authn.Middleware))
	t.Cleanup(srv.Close)

	return testAPI{srv: srv, client: srv.Client(), db: db, clock: clock, maxBody: maxBody}
}

// do sends a request and returns the response with its body already read.
func (a testAPI) do(t *testing.T, method, path string, body any, headers ...string) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	hasBody := false
	switch v := body.(type) {
	case nil:
	case string:
		// A raw string is sent verbatim, which is how the malformed-JSON tests
		// get to send bytes that would not survive a marshal round trip.
		reader = strings.NewReader(v)
		hasBody = true
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(raw)
		hasBody = true
	}

	req, err := http.NewRequestWithContext(t.Context(), method, a.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if hasBody {
		req.Header.Set("Content-Type", httpx.ContentTypeJSON)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}

	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp, raw
}

// decode unmarshals a response body into out, failing the test if it cannot.
func decode(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %q into %T: %v", raw, out, err)
	}
}

// decodeError reads the API's error envelope.
func decodeError(t *testing.T, raw []byte) (code string, details []httpx.FieldError) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string             `json:"code"`
			Details []httpx.FieldError `json:"details"`
		} `json:"error"`
	}
	decode(t, raw, &envelope)
	return envelope.Error.Code, envelope.Error.Details
}

// login signs in and returns the Cookie header value for subsequent requests.
func (a testAPI) login(t *testing.T) string {
	t.Helper()
	resp, raw := a.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "splash123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d, body %s", resp.StatusCode, raw)
	}
	setCookie := resp.Header.Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("login did not set a cookie")
	}
	return strings.Split(setCookie, ";")[0]
}

// adminDo is do with the admin session attached.
func (a testAPI) adminDo(t *testing.T, method, path string, body any, cookie string, headers ...string) (*http.Response, []byte) {
	t.Helper()
	all := append([]string{"Cookie", cookie}, headers...)
	return a.do(t, method, path, body, all...)
}

func validProjectRequest(name string) projectRequest {
	return projectRequest{
		Name: name, Icon: "💧", Color: "blue",
		Schedule: "daily", Target: 1, Unit: "times", StartDate: "2026-11-01",
	}
}

// createProject is the arrange step most tests need.
func (a testAPI) createProject(t *testing.T, cookie string, req projectRequest) project {
	t.Helper()
	resp, raw := a.adminDo(t, http.MethodPost, "/api/v1/projects", req, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create project: status %d, body %s", resp.StatusCode, raw)
	}
	var p project
	decode(t, raw, &p)
	return p
}

func TestMeStartsAnonymous(t *testing.T) {
	api := newTestAPI(t, 0)
	resp, raw := api.do(t, http.MethodGet, "/api/v1/auth/me", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var s sessionResponse
	decode(t, raw, &s)
	if s.Role != "anonymous" || s.Username != "" {
		t.Errorf("me = %+v, want anonymous", s)
	}
}

func TestLoginFlow(t *testing.T) {
	api := newTestAPI(t, 0)

	// Wrong credentials: one uniform message, no oracle for which half failed.
	// (The requestId differs per response by design; compare code and message.)
	resp, raw := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "nope"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: status %d", resp.StatusCode)
	}
	resp, raw2 := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "ghost", Password: "splash123"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong username: status %d", resp.StatusCode)
	}
	var e1, e2 struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decode(t, raw, &e1)
	decode(t, raw2, &e2)
	if e1.Error.Code != e2.Error.Code || e1.Error.Message != e2.Error.Message {
		t.Errorf("wrong username and wrong password must answer identically: %+v vs %+v", e1.Error, e2.Error)
	}

	// The session cookie is HttpOnly and Lax: the CSRF posture depends on it.
	resp, _ = api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "splash123"})
	setCookie := resp.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Errorf("Set-Cookie = %q, want HttpOnly and SameSite=Lax", setCookie)
	}
	cookie := strings.Split(setCookie, ";")[0]
	resp, raw = api.do(t, http.MethodGet, "/api/v1/auth/me", nil, "Cookie", cookie)
	var s sessionResponse
	decode(t, raw, &s)
	if s.Role != "admin" || s.Username != "admin" {
		t.Errorf("me after login = %+v", s)
	}

	resp, _ = api.do(t, http.MethodPost, "/api/v1/auth/logout", nil, "Cookie", cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	resp, raw = api.do(t, http.MethodGet, "/api/v1/auth/me", nil)
	decode(t, raw, &s)
	if s.Role != "anonymous" {
		t.Errorf("me after logout = %+v", s)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	api := newTestAPI(t, 0)
	for i := 0; i < 5; i++ {
		resp, _ := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "wrong"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i+1, resp.StatusCode)
		}
	}
	// The sixth attempt is over budget — even with the right password.
	resp, raw := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "splash123"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over-budget login: status %d, body %s", resp.StatusCode, raw)
	}
	code, _ := decodeError(t, raw)
	if code != httpx.CodeRateLimited {
		t.Errorf("code = %q", code)
	}
}

func TestBearerTokenWorks(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	token := strings.TrimPrefix(cookie, auth.CookieName+"=")
	resp, raw := api.do(t, http.MethodGet, "/api/v1/auth/me", nil, "Authorization", "Bearer "+token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var s sessionResponse
	decode(t, raw, &s)
	if s.Role != "admin" {
		t.Errorf("bearer auth: %+v", s)
	}
}

func TestWritesRequireAdmin(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))

	writes := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/v1/projects", validProjectRequest("x")},
		{http.MethodPatch, "/api/v1/projects/" + p.ID, projectPatchRequest{Name: new("y")}},
		{http.MethodDelete, "/api/v1/projects/" + p.ID, nil},
		{http.MethodPost, "/api/v1/categories", categoryRequest{Name: "c"}},
		{http.MethodPost, "/api/v1/projects/" + p.ID + "/checkins", checkinRequest{Value: 1}},
		{http.MethodPost, "/api/v1/projects/" + p.ID + "/skips", skipRequest{Date: "2026-11-10"}},
		{http.MethodGet, "/api/v1/export", nil},
	}
	for _, w := range writes {
		resp, raw := api.do(t, w.method, w.path, w.body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous: status %d, body %s", w.method, w.path, resp.StatusCode, raw)
		}
	}
}

func TestProjectCRUD(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)

	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/categories", categoryRequest{Name: "Health", Icon: "💪", Color: "green"}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create category: %d %s", resp.StatusCode, raw)
	}
	var cat category
	decode(t, raw, &cat)

	req := validProjectRequest("Water")
	req.CategoryID = cat.ID
	req.Description = "8 cups a day"
	req.Target = 8
	req.Unit = "cups"
	p := api.createProject(t, cookie, req)
	if p.CategoryID != cat.ID || p.Version != 1 || p.Status != nil {
		t.Errorf("created = %+v", p)
	}

	// Listing carries a status for every project.
	resp, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects?date=2026-11-21", nil, cookie)
	var list listProjectsResponse
	decode(t, raw, &list)
	if list.Today != "2026-11-21" || len(list.Projects) == 0 {
		t.Fatalf("list = %+v", list)
	}
	for _, item := range list.Projects {
		if item.Status == nil || item.Status.Status != "pending" {
			t.Errorf("project %s status = %+v, want pending", item.Name, item.Status)
		}
	}

	// PATCH bumps the version and the ETag.
	resp, raw = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("Drink more")}, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch: %d %s", resp.StatusCode, raw)
	}
	var updated project
	decode(t, raw, &updated)
	if updated.Name != "Drink more" || updated.Version != 2 || resp.Header.Get("ETag") != `"v2"` {
		t.Errorf("updated = %+v, ETag = %q", updated, resp.Header.Get("ETag"))
	}

	// DELETE cascades and then 404s.
	if resp, _ := api.adminDo(t, http.MethodDelete, "/api/v1/projects/"+p.ID, nil, cookie); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID, nil, cookie); resp.StatusCode != http.StatusNotFound {
		t.Errorf("get after delete: %d", resp.StatusCode)
	}
}

func TestEveryDataRouteRequiresAdmin(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))
	pid, err := uuid.Parse(p.ID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	c, err := api.db.Checkins().Create(t.Context(), pid, store.CheckinInput{Value: 1})
	if err != nil {
		t.Fatalf("seed checkin: %v", err)
	}

	// Reads are gated just like writes: an anonymous request never sees data.
	for _, path := range []string{
		"/api/v1/projects",
		"/api/v1/projects/" + p.ID,
		"/api/v1/projects/" + p.ID + "/stats",
		"/api/v1/projects/" + p.ID + "/checkins",
		"/api/v1/checkins/" + c.ID.String() + "/photo",
		"/api/v1/projects/" + p.ID + "/skips",
		"/api/v1/categories",
		"/api/v1/stats/overview",
		"/api/v1/export",
	} {
		resp, raw := api.do(t, http.MethodGet, path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s: status %d, body %s", path, resp.StatusCode, raw)
		}
	}

	// The admin sees the project and its check-in.
	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	var list listProjectsResponse
	decode(t, raw, &list)
	if len(list.Projects) != 1 {
		t.Errorf("admin list = %d, want 1", len(list.Projects))
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/stats/overview", nil, cookie)
	var overview store.OverviewStats
	decode(t, raw, &overview)
	if overview.Totals.Checkins != 1 {
		t.Errorf("admin overview checkins = %d, want 1", overview.Totals.Checkins)
	}
}

func TestCategoriesShowAllIncludingUnused(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	mkCat := func(name string) category {
		resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/categories", categoryRequest{Name: name}, cookie)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("category: %d %s", resp.StatusCode, raw)
		}
		var c category
		decode(t, raw, &c)
		return c
	}
	used, unused := mkCat("Health"), mkCat("Empty category")
	req := validProjectRequest("Water")
	req.CategoryID = used.ID
	api.createProject(t, cookie, req)

	// There is no anonymous filter anymore: the admin sees every category,
	// even one no project uses.
	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/categories", nil, cookie)
	var admin listCategoriesResponse
	decode(t, raw, &admin)
	if len(admin.Categories) != 2 {
		t.Errorf("admin categories = %d, want 2", len(admin.Categories))
	}
	_ = unused
}

func TestProjectValidationReportsEveryField(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects", projectRequest{
		Name: "", Schedule: "daily", Target: 0, StartDate: "nope",
	}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body %s", resp.StatusCode, raw)
	}
	code, details := decodeError(t, raw)
	if code != httpx.CodeValidationFailed {
		t.Errorf("code = %q", code)
	}
	fields := map[string]bool{}
	for _, d := range details {
		fields[d.Field] = true
	}
	for _, want := range []string{"name", "target", "startDate"} {
		if !fields[want] {
			t.Errorf("details %v missing field %q", details, want)
		}
	}

	// An unknown schedule is its own field problem.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects", projectRequest{
		Name: "x", Schedule: "hourly", Target: 1, StartDate: "2026-11-01",
	}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("bad schedule: %d %s", resp.StatusCode, raw)
	}
	_, details = decodeError(t, raw)
	if len(details) == 0 || details[0].Field != "schedule" {
		t.Errorf("bad schedule details = %v", details)
	}
}

func TestIfMatchConcurrency(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Original"))

	// Someone else's write lands first.
	resp, _ := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("Changed by someone else")}, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first patch: %d", resp.StatusCode)
	}
	// My stale v1 write is refused with 412, and nothing changed.
	resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("Mine")}, cookie, "If-Match", `"v1"`)
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale patch: %d %s", resp.StatusCode, raw)
	}
	// The current version succeeds.
	resp, _ = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("Mine")}, cookie, "If-Match", `"v2"`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fresh patch: %d", resp.StatusCode)
	}
	// "*" opts out of the check; garbage is a 400.
	resp, _ = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("Wildcard")}, cookie, "If-Match", "*")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wildcard patch: %d", resp.StatusCode)
	}
	resp, _ = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Name: new("x")}, cookie, "If-Match", "banana")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage If-Match: %d", resp.StatusCode)
	}
	// Delete enforces the same precondition.
	resp, _ = api.adminDo(t, http.MethodDelete, "/api/v1/projects/"+p.ID, nil, cookie, "If-Match", `"v1"`)
	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale delete: %d", resp.StatusCode)
	}
}

func TestCheckinFlow(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Meditation"))

	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1, Note: "done", Mood: "😄", TZOffset: 480}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checkin: %d %s", resp.StatusCode, raw)
	}
	var c checkin
	decode(t, raw, &c)
	if c.LocalDate != "2026-11-21" || c.Value != 1 || c.Mood != "😄" {
		t.Errorf("checkin = %+v", c)
	}

	// The project is done now.
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats", nil, cookie)
	var stats statsResponse
	decode(t, raw, &stats)
	if stats.Summary.Status != "done" || stats.Summary.Streak != 1 {
		t.Errorf("stats after checkin = %+v", stats.Summary)
	}

	// History lists it; undo removes it and the stats follow.
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/checkins", nil, cookie)
	var hist listCheckinsResponse
	decode(t, raw, &hist)
	if len(hist.Checkins) != 1 {
		t.Fatalf("history = %d", len(hist.Checkins))
	}
	if resp, _ := api.adminDo(t, http.MethodDelete, "/api/v1/checkins/"+c.ID, nil, cookie); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("undo: %d", resp.StatusCode)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats", nil, cookie)
	decode(t, raw, &stats)
	if stats.Summary.Status != "pending" || stats.Totals.Checkins != 0 {
		t.Errorf("stats after undo = %+v / %+v", stats.Summary, stats.Totals)
	}

	// Backfill lands on the requested day.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1, Date: "2026-11-19"}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("backfill: %d %s", resp.StatusCode, raw)
	}
	decode(t, raw, &c)
	if c.LocalDate != "2026-11-19" {
		t.Errorf("backfill date = %s", c.LocalDate)
	}

	// Future dates and zero values are field problems.
	resp, _ = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1, Date: "2026-12-01"}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("future checkin: %d", resp.StatusCode)
	}
	resp, _ = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 0}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("zero value: %d", resp.StatusCode)
	}
}

func TestSkipFlow(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	req := validProjectRequest("Reading")
	req.StartDate = "2026-11-18"
	p := api.createProject(t, cookie, req)
	// Two completed days, then a skip yesterday keeps the streak alive.
	for _, day := range []string{"2026-11-18", "2026-11-19"} {
		if resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1, Date: day}, cookie); resp.StatusCode != http.StatusCreated {
			t.Fatalf("checkin %s: %d %s", day, resp.StatusCode, raw)
		}
	}
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/skips", skipRequest{Date: "2026-11-20", Reason: "business trip"}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("skip: %d %s", resp.StatusCode, raw)
	}
	// Duplicate skip conflicts.
	resp, _ = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/skips", skipRequest{Date: "2026-11-20"}, cookie)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate skip: %d", resp.StatusCode)
	}

	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats", nil, cookie)
	var stats statsResponse
	decode(t, raw, &stats)
	if stats.Summary.Streak != 2 {
		t.Errorf("streak with skip = %d, want 2 (skip preserves)", stats.Summary.Streak)
	}
	if stats.Totals.Skipped != 1 || stats.Totals.Missed != 0 {
		t.Errorf("totals = %+v, want skipped 1 / missed 0", stats.Totals)
	}

	// Un-skip restores the miss.
	if resp, _ := api.adminDo(t, http.MethodDelete, "/api/v1/projects/"+p.ID+"/skips/2026-11-20", nil, cookie); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unskip: %d", resp.StatusCode)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats", nil, cookie)
	decode(t, raw, &stats)
	if stats.Totals.Missed != 1 || stats.Summary.Streak != 0 {
		t.Errorf("after unskip: missed %d streak %d, want 1/0", stats.Totals.Missed, stats.Summary.Streak)
	}
}

func TestWeekdaysAndIntervalSchedules(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)

	wd := validProjectRequest("Strength")
	wd.Schedule = "weekdays"
	wd.Weekdays = []int{1, 3, 5}
	wdProject := api.createProject(t, cookie, wd)

	ih := validProjectRequest("Stand up")
	ih.Schedule = "interval_hours"
	ih.Interval = 2
	ihProject := api.createProject(t, cookie, ih)

	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects?date=2026-11-21", nil, cookie)
	var list listProjectsResponse
	decode(t, raw, &list)
	byID := map[string]project{}
	for _, p := range list.Projects {
		byID[p.ID] = p
	}
	// 2026-11-21 is Saturday: a rest day for Mon/Wed/Fri.
	if s := byID[wdProject.ID].Status; s == nil || s.Status != "rest" {
		t.Errorf("weekdays status = %+v, want rest", s)
	}
	// Created at 12:00 with a 2h interval: due at 14:00, waiting at 12:00.
	s := byID[ihProject.ID].Status
	if s == nil || s.Status != "waiting" || s.DueAt == "" {
		t.Fatalf("interval status = %+v, want waiting with dueAt", s)
	}
	api.clock.Advance(3 * time.Hour)
	// The advance outlives the 1h session TTL, and reads are gated now, so
	// re-authenticate before the next read.
	cookie = api.login(t)
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+ihProject.ID, nil, cookie)
	var got project
	decode(t, raw, &got)
	if got.Status == nil || got.Status.Status != "due" {
		t.Errorf("interval after 3h = %+v, want due", got.Status)
	}
}

func TestStatsShapeAndWeeksParam(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	req := validProjectRequest("Reading")
	req.StartDate = "2026-11-18"
	p := api.createProject(t, cookie, req)
	for _, day := range []string{"2026-11-18", "2026-11-19", "2026-11-20", "2026-11-21"} {
		api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1, Date: day}, cookie)
	}

	resp, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats?weeks=4&date=2026-11-21", nil, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats: %d %s", resp.StatusCode, raw)
	}
	var stats statsResponse
	decode(t, raw, &stats)
	if stats.Summary.Streak != 4 || stats.Summary.BestStreak != 4 || stats.Summary.Status != "done" {
		t.Errorf("summary = %+v", stats.Summary)
	}
	// 4 weeks ending today, Monday-aligned: 2026-10-26 .. 2026-11-21 = 27 days.
	if len(stats.Heatmap) != 27 {
		t.Errorf("heatmap = %d cells, want 27", len(stats.Heatmap))
	}
	// The project is only 4 days old, so the trend shows 4 bars, not 14.
	if len(stats.Periods) != 4 {
		t.Errorf("daily bars = %d, want 4 (project age bounds the trend)", len(stats.Periods))
	}
	if len(stats.Recent) != 4 {
		t.Errorf("recent = %d, want 4", len(stats.Recent))
	}
	if stats.Project.ID != p.ID {
		t.Error("stats should embed the project")
	}

	if resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats?weeks=99", nil, cookie); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("weeks=99: %d", resp.StatusCode)
	}
}

func TestOverviewCounters(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	a := api.createProject(t, cookie, validProjectRequest("Water"))
	b := api.createProject(t, cookie, validProjectRequest("Reading"))
	api.adminDo(t, http.MethodPost, "/api/v1/projects/"+a.ID+"/checkins", checkinRequest{Value: 1}, cookie)

	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/stats/overview?date=2026-11-21", nil, cookie)
	var o store.OverviewStats
	decode(t, raw, &o)
	if o.Today.Total != 2 || o.Today.Done != 1 || o.Today.Pending != 1 {
		t.Errorf("today = %+v, want 2 total / 1 done / 1 pending", o.Today)
	}
	if o.Totals.Checkins != 1 || o.Totals.ActiveProjects != 2 {
		t.Errorf("totals = %+v", o.Totals)
	}
	if len(o.Activity) != 56 || len(o.Trend) != 8 {
		t.Errorf("activity/trend lengths = %d/%d", len(o.Activity), len(o.Trend))
	}
	if len(o.Leaders) != 2 || o.Leaders[0].ID != a.ID {
		t.Errorf("leaders = %+v, want the checked-in project first", o.Leaders)
	}
	_ = b
}

func TestBadHabitFlow(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	req := validProjectRequest("Late nights")
	req.Nature = "bad"
	req.StartDate = "2026-11-18"
	p := api.createProject(t, cookie, req)
	if p.Nature != "bad" || p.Mode != "check" {
		t.Fatalf("created bad project = %+v, want nature bad / check mode", p)
	}

	// A clean day reads as done (held); the streak counts survived days.
	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats?date=2026-11-21", nil, cookie)
	var stats statsResponse
	decode(t, raw, &stats)
	if stats.Summary.Status != "done" || stats.Summary.Streak != 3 {
		t.Errorf("clean bad stats = %+v, want done / streak 3", stats.Summary)
	}

	// Recording a lapse today spoils today and resets the streak.
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("lapse: %d %s", resp.StatusCode, raw)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"/stats?date=2026-11-21", nil, cookie)
	decode(t, raw, &stats)
	if stats.Summary.Status != "missed" || stats.Summary.Streak != 0 {
		t.Errorf("lapsed bad stats = %+v, want missed / streak 0", stats.Summary)
	}
	if stats.Totals.Missed != 1 || stats.Totals.Done != 3 {
		t.Errorf("bad totals = %+v, want 3 done / 1 missed", stats.Totals)
	}

	// The overview tallies the lapse as a slip, not a check-in, and counts it
	// in today's slipped counter.
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/stats/overview?date=2026-11-21", nil, cookie)
	var o store.OverviewStats
	decode(t, raw, &o)
	if o.Totals.Checkins != 0 || o.Totals.Slips != 1 {
		t.Errorf("overview totals = %+v, want 0 checkins / 1 slip", o.Totals)
	}
	if o.Today.Slipped != 1 || o.Today.Done != 0 {
		t.Errorf("overview today = %+v, want 1 slipped / 0 done", o.Today)
	}

	// interval_hours is rejected for a bad habit.
	ih := validProjectRequest("Bad timer")
	ih.Nature = "bad"
	ih.Schedule = "interval_hours"
	ih.Interval = 2
	resp, _ = api.adminDo(t, http.MethodPost, "/api/v1/projects", ih, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("bad + interval_hours: %d, want 422", resp.StatusCode)
	}
}

func TestExportIsAdminOnlyAndComplete(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))
	api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1}, cookie)

	resp, raw := api.adminDo(t, http.MethodGet, "/api/v1/export", nil, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export: %d %s", resp.StatusCode, raw)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	var dump store.ExportData
	decode(t, raw, &dump)
	if len(dump.Projects) != 1 || len(dump.Checkins) != 1 {
		t.Errorf("dump = %d projects / %d checkins", len(dump.Projects), len(dump.Checkins))
	}
}

func TestImportFlow(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))
	api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1}, cookie)

	// Export, then import the same payload into a fresh database via the API.
	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/export", nil, cookie)
	var dump store.ExportData
	decode(t, raw, &dump)

	// Anonymous import is refused.
	resp, _ := api.do(t, http.MethodPost, "/api/v1/import", dump)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous import: %d, want 401", resp.StatusCode)
	}

	// Admin import succeeds and reports counts.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/import", dump, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import: %d %s", resp.StatusCode, raw)
	}
	var counts store.ImportCounts
	decode(t, raw, &counts)
	// The project and check-in already exist here, so additive import adds none.
	if counts.Projects != 0 || counts.Checkins != 0 {
		t.Errorf("re-import into same db counts = %+v, want 0/0", counts)
	}

	// A backup referencing an unknown project is a 422, not a 500.
	bad := store.ExportData{Checkins: []store.Checkin{{
		ID: uuid.New(), ProjectID: uuid.New(), Value: 1, LocalDate: "2026-11-10",
	}}}
	resp, _ = api.adminDo(t, http.MethodPost, "/api/v1/import", bad, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("dangling import: %d, want 422", resp.StatusCode)
	}
}

func TestArchivedProjects(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Old habit"))
	archived := true
	api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID, projectPatchRequest{Archived: &archived}, cookie)

	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	var list listProjectsResponse
	decode(t, raw, &list)
	if len(list.Projects) != 0 {
		t.Errorf("archived project visible by default: %+v", list.Projects)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects?archived=1", nil, cookie)
	decode(t, raw, &list)
	if len(list.Projects) != 1 {
		t.Errorf("archived=1 list = %d, want 1", len(list.Projects))
	}
}

func TestHealthReadyVersion(t *testing.T) {
	api := newTestAPI(t, 0)
	resp, raw := api.do(t, http.MethodGet, "/api/v1/health", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"status":"ok"`) {
		t.Fatalf("health: %d %s", resp.StatusCode, raw)
	}
	resp, raw = api.do(t, http.MethodGet, "/api/v1/health/ready", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ready: %d %s", resp.StatusCode, raw)
	}
	var ready readyResponse
	decode(t, raw, &ready)
	if len(ready.Checks) != 1 || ready.Checks[0].Name != "database" || ready.Checks[0].Status != "ok" {
		t.Errorf("ready checks = %+v", ready.Checks)
	}
	resp, raw = api.do(t, http.MethodGet, "/api/v1/version", nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"service":"splash"`) {
		t.Fatalf("version: %d %s", resp.StatusCode, raw)
	}
}

func TestUnknownRouteAndWrongMethod(t *testing.T) {
	api := newTestAPI(t, 0)
	resp, raw := api.do(t, http.MethodGet, "/api/v1/does-not-exist", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown route: %d", resp.StatusCode)
	}
	code, _ := decodeError(t, raw)
	if code != httpx.CodeNotFound {
		t.Errorf("code = %q", code)
	}
	resp, _ = api.do(t, http.MethodPut, "/api/v1/projects", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: %d", resp.StatusCode)
	}
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Errorf("Allow = %q", allow)
	}
}

func TestMalformedJSON(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	for _, body := range []string{`{"name":`, `{"name":"a","name":"b"}`} {
		resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects", body, cookie)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q: status %d", body, resp.StatusCode)
		}
		code, _ := decodeError(t, raw)
		if code != httpx.CodeInvalidJSON {
			t.Errorf("body %q: code %q", body, code)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	api := newTestAPI(t, 64)
	cookie := api.login(t)
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects",
		validProjectRequest(strings.Repeat("a", 100)), cookie)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body %s", resp.StatusCode, raw)
	}
}

func TestConditionalGET(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Cached"))
	resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID, nil, cookie, "If-None-Match", `"v1"`)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("matching If-None-Match: %d", resp.StatusCode)
	}
	resp, _ = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID, nil, cookie, "If-None-Match", `"v9"`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("stale If-None-Match: %d", resp.StatusCode)
	}
}

func TestDateParamShiftsTheView(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))
	api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins", checkinRequest{Value: 1, Date: "2026-11-20"}, cookie)

	// From yesterday's point of view the project is done; from today's, pending.
	_, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"?date=2026-11-20", nil, cookie)
	var p1 project
	decode(t, raw, &p1)
	if p1.Status.Status != "done" {
		t.Errorf("yesterday view = %q, want done", p1.Status.Status)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+p.ID+"?date=2026-11-21", nil, cookie)
	var p2 project
	decode(t, raw, &p2)
	if p2.Status.Status != "pending" {
		t.Errorf("today view = %q, want pending", p2.Status.Status)
	}
	if resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/projects?date=banana", nil, cookie); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad date param: %d", resp.StatusCode)
	}
}

func TestMalformedIDIsA400(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	for _, path := range []string{"/api/v1/projects/not-a-uuid", "/api/v1/projects/123"} {
		resp, _ := api.adminDo(t, http.MethodGet, path, nil, cookie)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s: %d", path, resp.StatusCode)
		}
	}
}

func TestReadyFailsWhenTheDatabaseIsGone(t *testing.T) {
	api := newTestAPI(t, 0)
	if err := api.db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	resp, raw := api.do(t, http.MethodGet, "/api/v1/health/ready", nil)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ready with a closed database: %d %s", resp.StatusCode, raw)
	}
	// Liveness stays green: a dead dependency must not restart the process.
	resp, _ = api.do(t, http.MethodGet, "/api/v1/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health with a closed database: %d, want 200", resp.StatusCode)
	}
}

func TestCheckinPhotoEndpoint(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Proof"))

	const png = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1, Photo: png}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checkin: %d %s", resp.StatusCode, raw)
	}
	var c checkin
	decode(t, raw, &c)
	if !c.HasPhoto {
		t.Fatal("create response does not report hasPhoto")
	}

	// The bytes are served lazily, with the image type and an immutable cache.
	resp, raw = api.adminDo(t, http.MethodGet, "/api/v1/checkins/"+c.ID+"/photo", nil, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("photo: %d %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", got)
	}
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "private") || !strings.Contains(got, "immutable") {
		t.Errorf("Cache-Control = %q, want private + immutable", got)
	}
	if len(raw) == 0 || raw[0] != 0x89 || string(raw[1:4]) != "PNG" {
		t.Errorf("photo body is not a PNG: % x", raw[:min(len(raw), 8)])
	}

	// A record without a photo, and an unknown id, are plain 404s.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1}, cookie)
	decode(t, raw, &c)
	resp, raw = api.adminDo(t, http.MethodGet, "/api/v1/checkins/"+c.ID+"/photo", nil, cookie)
	if code, _ := decodeError(t, raw); resp.StatusCode != http.StatusNotFound || code != "not_found" {
		t.Errorf("photo without payload: %d %s", resp.StatusCode, code)
	}
	if resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/checkins/"+uuid.NewV7().String()+"/photo", nil, cookie); resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown photo: %d", resp.StatusCode)
	}
	if resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/checkins/not-a-uuid/photo", nil, cookie); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed photo id: %d", resp.StatusCode)
	}
}

func TestPinnedProjectRoundTrip(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Pinned"))
	if p.Pinned {
		t.Fatal("a new project must start unpinned")
	}

	resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID,
		projectPatchRequest{Pinned: new(true)}, cookie, "If-Match", `"v1"`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pin: %d %s", resp.StatusCode, raw)
	}
	var updated project
	decode(t, raw, &updated)
	if !updated.Pinned || updated.Version != 2 {
		t.Errorf("pinned project = %+v", updated)
	}

	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	var listed listProjectsResponse
	decode(t, raw, &listed)
	if len(listed.Projects) != 1 || !listed.Projects[0].Pinned {
		t.Errorf("list does not carry the pin: %+v", listed.Projects)
	}
}

func TestPostrequisiteFlow(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	run := api.createProject(t, cookie, validProjectRequest("Run"))
	sleep := api.createProject(t, cookie, validProjectRequest("Sleep"))

	resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+run.ID,
		projectPatchRequest{Postrequisites: &[]string{sleep.ID}}, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wire postrequisite: %d %s", resp.StatusCode, raw)
	}
	var patched project
	decode(t, raw, &patched)
	if len(patched.Postrequisites) != 1 || patched.Postrequisites[0] != sleep.ID {
		t.Fatalf("project postrequisites = %v", patched.Postrequisites)
	}

	yesterday := "2026-11-20"
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+run.ID+"/checkins",
		checkinRequest{Value: 1, Date: yesterday}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("run checkin: %d %s", resp.StatusCode, raw)
	}

	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+run.ID+"/stats", nil, cookie)
	var stats statsResponse
	decode(t, raw, &stats)
	if stats.Totals.PostReqs != 1 || stats.Totals.PostViolations != 1 || stats.Totals.CommitmentRate != 0 {
		t.Errorf("post totals = %+v", stats.Totals)
	}
	if len(stats.BrokenCommitments) != 1 || stats.BrokenCommitments[0].Date != yesterday || stats.BrokenCommitments[0].TargetID.String() != sleep.ID {
		t.Errorf("broken commitments = %+v", stats.BrokenCommitments)
	}
	if !strings.Contains(string(raw), `"targetId":"`+sleep.ID+`"`) {
		t.Errorf("broken commitment targetId is not a JSON string: %s", raw)
	}
	if len(stats.Recent) != 1 || len(stats.Recent[0].PostUnmet) != 1 || stats.Recent[0].PostUnmet[0] != sleep.ID {
		t.Errorf("recent post verdicts = %+v", stats.Recent)
	}

	// The overview surfaces the same broken commitment as a recent violation.
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/stats/overview", nil, cookie)
	var overview store.OverviewStats
	decode(t, raw, &overview)
	if overview.Totals.PostReqs != 1 || overview.Totals.PostViolations != 1 {
		t.Errorf("overview post totals = %+v", overview.Totals)
	}
	if len(overview.RecentViolations) != 1 || len(overview.RecentViolations[0].PostUnmet) != 1 {
		t.Errorf("overview recent violations = %+v", overview.RecentViolations)
	}

	// Sleeping on the same day clears it.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+sleep.ID+"/checkins",
		checkinRequest{Value: 1, Date: yesterday}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sleep checkin: %d %s", resp.StatusCode, raw)
	}
	_, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects/"+run.ID+"/stats", nil, cookie)
	decode(t, raw, &stats)
	if stats.Totals.PostViolations != 0 || len(stats.BrokenCommitments) != 0 {
		t.Errorf("after backfill: totals %+v broken %+v", stats.Totals, stats.BrokenCommitments)
	}
	if stats.Summary.PendingPosts == nil || stats.Summary.BrokenPosts == nil || stats.BrokenCommitments == nil {
		t.Errorf("wire arrays must be [] not null: %s", raw)
	}
}

func TestLinkValidationOverHTTP(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	a := api.createProject(t, cookie, validProjectRequest("A"))
	b := api.createProject(t, cookie, validProjectRequest("B"))
	bad := api.createProject(t, cookie, projectRequest{
		Name: "Bad", Icon: "🦉", Color: "indigo", Nature: "bad", Schedule: "daily",
		Target: 1, Unit: "times", StartDate: "2026-11-01",
	})

	// Postrequisites on a bad habit are rejected.
	resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+bad.ID,
		projectPatchRequest{Postrequisites: &[]string{a.ID}}, cookie)
	if code, _ := decodeError(t, raw); resp.StatusCode != http.StatusUnprocessableEntity || code != "validation_failed" {
		t.Errorf("bad postrequisite: %d %s", resp.StatusCode, code)
	}

	// The same target cannot be both directions.
	resp, raw = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+a.ID,
		projectPatchRequest{Prerequisites: &[]string{b.ID}, Postrequisites: &[]string{b.ID}}, cookie)
	if code, _ := decodeError(t, raw); resp.StatusCode != http.StatusUnprocessableEntity || code != "validation_failed" {
		t.Errorf("overlap: %d %s", resp.StatusCode, code)
	}

	// Cross-direction cycles are rejected.
	if resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+a.ID,
		projectPatchRequest{Postrequisites: &[]string{b.ID}}, cookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("a implies b: %d %s", resp.StatusCode, raw)
	}
	resp, raw = api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+b.ID,
		projectPatchRequest{Prerequisites: &[]string{a.ID}}, cookie)
	if code, details := decodeError(t, raw); resp.StatusCode != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) == 0 {
		t.Errorf("cycle: %d %s %v", resp.StatusCode, code, details)
	}
}

func TestNotePromptOverHTTP(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	req := validProjectRequest("Sleep")
	req.NotePrompt = "什么时候睡的？睡了多久？"
	p := api.createProject(t, cookie, req)
	if p.NotePrompt != req.NotePrompt {
		t.Errorf("created notePrompt = %q", p.NotePrompt)
	}
	next := "睡了多久？"
	resp, raw := api.adminDo(t, http.MethodPatch, "/api/v1/projects/"+p.ID,
		projectPatchRequest{NotePrompt: &next}, cookie, "If-Match", `"v1"`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch notePrompt: %d %s", resp.StatusCode, raw)
	}
	var patched project
	decode(t, raw, &patched)
	if patched.NotePrompt != next {
		t.Errorf("patched notePrompt = %q", patched.NotePrompt)
	}

	// The supplement is the ordinary note field on the record.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1, Note: "22:40 入睡，睡了 7 小时"}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checkin: %d %s", resp.StatusCode, raw)
	}
	var c checkin
	decode(t, raw, &c)
	if c.Note != "22:40 入睡，睡了 7 小时" {
		t.Errorf("checkin note = %q", c.Note)
	}
}

func TestResetRequiresPasswordAndWipesData(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("KeepMe"))
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects/"+p.ID+"/checkins",
		checkinRequest{Value: 1, Note: "before reset"}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed checkin: %d %s", resp.StatusCode, raw)
	}

	// A wrong password is rejected without touching the database.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/admin/reset",
		resetRequest{Password: "wrong"}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password status = %d, body %s", resp.StatusCode, raw)
	}
	code, details := decodeError(t, raw)
	if code != httpx.CodeValidationFailed || len(details) != 1 ||
		details[0].Field != "password" || details[0].Message != "is incorrect" {
		t.Errorf("wrong password error = %s %+v", code, details)
	}
	resp, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	var before struct {
		Projects []project `json:"projects"`
	}
	decode(t, raw, &before)
	if resp.StatusCode != http.StatusOK || len(before.Projects) != 1 {
		t.Fatalf("projects before reset = %d, status %d", len(before.Projects), resp.StatusCode)
	}

	// An empty confirmation is its own field error.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/admin/reset", resetRequest{}, cookie)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("empty password status = %d, body %s", resp.StatusCode, raw)
	}
	_, details = decodeError(t, raw)
	if len(details) != 1 || details[0].Message != "is required" {
		t.Errorf("empty password details = %+v", details)
	}

	// The correct password deletes and recreates the store.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/admin/reset",
		resetRequest{Password: "splash123"}, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reset status = %d, body %s", resp.StatusCode, raw)
	}
	var result resetResponse
	decode(t, raw, &result)
	if !result.Reset || result.SeededProjects != 0 {
		t.Errorf("reset response = %+v", result)
	}

	resp, raw = api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	var after struct {
		Projects []project `json:"projects"`
	}
	decode(t, raw, &after)
	if resp.StatusCode != http.StatusOK || len(after.Projects) != 0 {
		t.Fatalf("projects after reset = %d, status %d, body %s", len(after.Projects), resp.StatusCode, raw)
	}
	resp, raw = api.do(t, http.MethodGet, "/api/v1/health/ready", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readiness after reset = %d, body %s", resp.StatusCode, raw)
	}
	// The session survives (it is signed by config, not stored in SQLite), and
	// the fresh database accepts writes.
	resp, raw = api.adminDo(t, http.MethodPost, "/api/v1/projects",
		validProjectRequest("AfterReset"), cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create after reset = %d, body %s", resp.StatusCode, raw)
	}
}
