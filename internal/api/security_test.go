package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/httpx"
)

// These tests simulate an attacker against the real handler set (real SQLite,
// real auth middleware). They are the regression net for the access model:
// every data route is admin-only, bodies must be JSON, inputs are parameterized,
// and the login limiter stops guessing without locking out the admin.

func TestSecurityEveryDataRouteRejectsAnonymous(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	p := api.createProject(t, cookie, validProjectRequest("Water"))

	routes := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/projects", nil},
		{http.MethodPost, "/api/v1/projects", validProjectRequest("x")},
		{http.MethodGet, "/api/v1/projects/" + p.ID, nil},
		{http.MethodPatch, "/api/v1/projects/" + p.ID, projectPatchRequest{Name: new("y")}},
		{http.MethodDelete, "/api/v1/projects/" + p.ID, nil},
		{http.MethodGet, "/api/v1/projects/" + p.ID + "/checkins", nil},
		{http.MethodPost, "/api/v1/projects/" + p.ID + "/checkins", checkinRequest{Value: 1}},
		{http.MethodDelete, "/api/v1/checkins/00000000-0000-7000-8000-000000000000", nil},
		{http.MethodGet, "/api/v1/projects/" + p.ID + "/skips", nil},
		{http.MethodPost, "/api/v1/projects/" + p.ID + "/skips", skipRequest{Date: "2026-11-10"}},
		{http.MethodDelete, "/api/v1/projects/" + p.ID + "/skips/2026-11-10", nil},
		{http.MethodGet, "/api/v1/projects/" + p.ID + "/stats", nil},
		{http.MethodGet, "/api/v1/stats/overview", nil},
		{http.MethodGet, "/api/v1/categories", nil},
		{http.MethodPost, "/api/v1/categories", categoryRequest{Name: "c"}},
		{http.MethodPatch, "/api/v1/categories/00000000-0000-7000-8000-000000000000", categoryRequest{Name: "c"}},
		{http.MethodDelete, "/api/v1/categories/00000000-0000-7000-8000-000000000000", nil},
		{http.MethodGet, "/api/v1/export", nil},
	}
	for _, rt := range routes {
		resp, raw := api.do(t, rt.method, rt.path, rt.body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: status %d, body %s", rt.method, rt.path, resp.StatusCode, raw)
		}
	}
}

// A cross-site form POST cannot set application/json without a CORS preflight,
// so requiring it blocks the classic CSRF vector even before SameSite=Lax.
func TestSecurityFormEncodedBodyIsRejected(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	resp, raw := api.adminDo(t, http.MethodPost, "/api/v1/projects",
		`name=csrf&schedule=daily`, cookie, "Content-Type", "application/x-www-form-urlencoded")
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form POST: status %d, body %s", resp.StatusCode, raw)
	}
	code, _ := decodeError(t, raw)
	if code != httpx.CodeUnsupportedMedia {
		t.Errorf("code = %q, want %q", code, httpx.CodeUnsupportedMedia)
	}
}

// Search and category filters go through parameterized SQL with LIKE escaping;
// injection metacharacters must be inert literals, not executable SQL.
func TestSecuritySQLInjectionIsInert(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	api.createProject(t, cookie, validProjectRequest("Water"))

	for _, payload := range []string{
		`' OR '1'='1`,
		`'; DROP TABLE projects; --`,
		`" UNION SELECT id FROM projects --`,
		`%`,
		`_`,
		`100%`,
	} {
		resp, raw := api.adminDo(t, http.MethodGet, "/api/v1/projects?q="+url.QueryEscape(payload), nil, cookie)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("search %q: status %d, body %s", payload, resp.StatusCode, raw)
		}
		var list listProjectsResponse
		decode(t, raw, &list)
		// None of these payloads is a substring of "Water", so nothing matches;
		// crucially the table still exists and the query did not error.
		if len(list.Projects) != 0 {
			t.Errorf("search %q leaked %d projects", payload, len(list.Projects))
		}
	}
	// The projects table survived the DROP attempt.
	resp, _ := api.adminDo(t, http.MethodGet, "/api/v1/projects", nil, cookie)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("projects table gone after injection attempt: %d", resp.StatusCode)
	}
}

func TestSecurityLoginLimiterStopsGuessingButResetOnSuccess(t *testing.T) {
	api := newTestAPI(t, 0)
	// Three failed guesses (budget is 5).
	for i := 0; i < 3; i++ {
		resp, _ := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "wrong"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("guess %d: status %d", i+1, resp.StatusCode)
		}
	}
	// A correct login within budget succeeds and clears the IP's budget.
	resp, _ := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "splash123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct login after 3 guesses: %d", resp.StatusCode)
	}
	// Five fresh failed guesses are allowed again (the success reset the window).
	for i := 0; i < 5; i++ {
		resp, _ := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "wrong"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("post-reset guess %d: status %d, want 401 (budget was reset)", i+1, resp.StatusCode)
		}
	}
	// The sixth fails the budget.
	resp, _ = api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "wrong"})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("over budget after reset: %d, want 429", resp.StatusCode)
	}
}

func TestSecurityTamperedBearerTokenIsRejected(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	token := strings.TrimPrefix(cookie, auth.CookieName+"=")

	// Flip the first character of the signature (a fully significant base64
	// digit, unlike the last which may carry padding bits): the HMAC must fail.
	parts := strings.Split(token, ".")
	sig := []byte(parts[2])
	if sig[0] == 'A' {
		sig[0] = 'B'
	} else {
		sig[0] = 'A'
	}
	tampered := parts[0] + "." + parts[1] + "." + string(sig)
	resp, _ := api.do(t, http.MethodGet, "/api/v1/projects", nil, "Authorization", "Bearer "+tampered)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tampered token: %d, want 401", resp.StatusCode)
	}

	// A syntactically broken token is also rejected.
	resp, _ = api.do(t, http.MethodGet, "/api/v1/projects", nil, "Authorization", "Bearer not.a.jwt")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("garbage token: %d, want 401", resp.StatusCode)
	}
}

func TestSecurityExpiredSessionIsRejected(t *testing.T) {
	api := newTestAPI(t, 0)
	cookie := api.login(t)
	// The test TTL is 1h; advancing past it expires the token.
	api.clock.Advance(2 * time.Hour)
	resp, _ := api.do(t, http.MethodGet, "/api/v1/projects", nil, "Cookie", cookie)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expired session: %d, want 401", resp.StatusCode)
	}
}

func TestSecurityOversizedBodyIsRejected(t *testing.T) {
	api := newTestAPI(t, 64)
	cookie := api.login(t)
	resp, _ := api.adminDo(t, http.MethodPost, "/api/v1/projects",
		validProjectRequest(strings.Repeat("a", 200)), cookie)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %d, want 413", resp.StatusCode)
	}
}

func TestSecuritySessionCookieIsHttpOnlyAndLax(t *testing.T) {
	api := newTestAPI(t, 0)
	resp, _ := api.do(t, http.MethodPost, "/api/v1/auth/login", loginRequest{Username: "admin", Password: "splash123"})
	setCookie := resp.Header.Get("Set-Cookie")
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Errorf("cookie missing HttpOnly: %q", setCookie)
	}
	if !strings.Contains(setCookie, "SameSite=Lax") {
		t.Errorf("cookie missing SameSite=Lax: %q", setCookie)
	}
	// A browser-session cookie: no Max-Age/Expires, so closing the browser ends it.
	if strings.Contains(setCookie, "Max-Age") || strings.Contains(setCookie, "Expires") {
		t.Errorf("cookie should be browser-session scoped: %q", setCookie)
	}
}

func TestSecurityRequestIDCannotForgeLogLines(t *testing.T) {
	api := newTestAPI(t, 0)
	// A request id with script characters must be replaced, not echoed. (CRLF
	// is rejected by net/http itself at the transport layer, so it cannot even
	// be sent — the charset check here is the second line of defence.)
	resp, _ := api.do(t, http.MethodGet, "/api/v1/health", nil,
		"X-Request-ID", "evil<script>alert(1)</script>")
	got := resp.Header.Get("X-Request-ID")
	if strings.ContainsAny(got, "<>()") {
		t.Errorf("request id echoed unsafe characters: %q", got)
	}
}
