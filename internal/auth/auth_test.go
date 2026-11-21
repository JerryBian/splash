package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

func testAuth(clock func() time.Time) *Authenticator {
	return New(Options{
		Username: "admin",
		Password: "splash123",
		Secret:   "test-secret-that-is-long-enough",
		TTL:      time.Hour,
		Clock:    clock,
	})
}

func TestCheckCredentials(t *testing.T) {
	a := testAuth(nil)
	if !a.Check("admin", "splash123") {
		t.Error("correct credentials rejected")
	}
	if a.Check("admin", "wrong") || a.Check("root", "splash123") || a.Check("", "") {
		t.Error("wrong credentials accepted")
	}
}

func TestIssueAndParseRoundTrip(t *testing.T) {
	a := testAuth(func() time.Time { return epoch })
	token, err := a.Issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("token %q is not in JWT compact form", token)
	}
	claims, err := a.Parse(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != "admin" {
		t.Errorf("subject = %q", claims.Subject)
	}
	if claims.Expires != epoch.Add(time.Hour).Unix() {
		t.Errorf("exp = %d, want %d", claims.Expires, epoch.Add(time.Hour).Unix())
	}
}

func TestParseRejectsTamperedAndExpiredTokens(t *testing.T) {
	now := epoch
	a := testAuth(func() time.Time { return now })
	token, _ := a.Issue()

	tests := []struct {
		name  string
		token string
	}{
		{"garbage", "not-a-token"},
		{"two parts", "aaa.bbb"},
		{"flipped signature", token[:len(token)-2] + "xx"},
		{"empty signature", token[:strings.LastIndex(token, ".")+1]},
		{"wrong subject", forge(t, "someone-else", epoch.Add(time.Hour).Unix())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := a.Parse(tt.token); err == nil {
				t.Error("invalid token accepted")
			}
		})
	}

	// Expiry: advance the clock past exp.
	now = epoch.Add(2 * time.Hour)
	if _, err := a.Parse(token); err == nil {
		t.Error("expired token accepted")
	}

	// A different secret must not validate the old token.
	other := New(Options{Username: "admin", Password: "splash123", Secret: "another-secret-long-enough", Clock: func() time.Time { return epoch }})
	if _, err := other.Parse(token); err == nil {
		t.Error("token signed with a different secret accepted")
	}
}

// forge builds a token with the test secret but arbitrary claims, to prove the
// subject check is enforced even on a correctly signed token.
func forge(t *testing.T, subject string, exp int64) string {
	t.Helper()
	a := New(Options{Username: subject, Password: "x", Secret: "test-secret-that-is-long-enough", TTL: time.Hour,
		Clock: func() time.Time { return time.Unix(exp-3600, 0) }})
	token, err := a.Issue()
	if err != nil {
		t.Fatalf("forge: %v", err)
	}
	return token
}

func TestMiddlewareLabelsRequests(t *testing.T) {
	a := testAuth(func() time.Time { return epoch })
	token, _ := a.Issue()

	var seen Role
	var seenUser string
	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RoleFrom(r.Context())
		seenUser = UsernameFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("anonymous without a token", func(t *testing.T) {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if seen != RoleAnonymous || seenUser != "" {
			t.Errorf("role/user = %s/%q", seen, seenUser)
		}
	})

	t.Run("admin via cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if seen != RoleAdmin || seenUser != "admin" {
			t.Errorf("role/user = %s/%q", seen, seenUser)
		}
	})

	t.Run("admin via bearer header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if seen != RoleAdmin {
			t.Errorf("role = %s", seen)
		}
	})

	t.Run("garbage cookie stays anonymous", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: CookieName, Value: "garbage"})
		handler.ServeHTTP(httptest.NewRecorder(), req)
		if seen != RoleAnonymous {
			t.Errorf("role = %s, want anonymous (fail closed)", seen)
		}
	})
}

func TestRoleFromFailsClosedWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if RoleFrom(req.Context()) != RoleAnonymous || IsAdmin(req.Context()) {
		t.Error("a context without the middleware must be anonymous")
	}
}

func TestCookieFlags(t *testing.T) {
	a := New(Options{Username: "admin", Password: "splash123", Secret: "test-secret-that-is-long-enough",
		TTL: time.Hour, Secure: true})
	rec := httptest.NewRecorder()
	a.SetCookie(rec, "token-value")
	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Errorf("cookie flags = %+v", cookie)
	}
	// Browser-session cookie: closing the browser must force a fresh login.
	if cookie.MaxAge != 0 {
		t.Errorf("maxAge = %d, want 0 (browser-session scoped)", cookie.MaxAge)
	}

	rec = httptest.NewRecorder()
	a.ClearCookie(rec)
	if rec.Result().Cookies()[0].MaxAge != -1 {
		t.Error("ClearCookie must expire the cookie")
	}
}

func TestMiddlewareSlidesExpiringSessions(t *testing.T) {
	now := epoch
	a := testAuth(func() time.Time { return now })
	token, _ := a.Issue() // expires at epoch+1h

	var seen Role
	handler := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RoleFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Fresh token (50 min left of 60): no reissue.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	now = epoch.Add(10 * time.Minute)
	handler.ServeHTTP(rec, req)
	if seen != RoleAdmin {
		t.Fatalf("role = %s", seen)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a fresh session should not be reissued")
	}

	// Past half the window (40 min elapsed, 20 left): activity reissues.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	now = epoch.Add(40 * time.Minute)
	handler.ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected a sliding reissue, got %d cookies", len(cookies))
	}
	claims, err := a.Parse(cookies[0].Value)
	if err != nil {
		t.Fatalf("reissued token invalid: %v", err)
	}
	if claims.Expires != now.Add(time.Hour).Unix() {
		t.Errorf("reissued exp = %d, want now+1h", claims.Expires)
	}

	// The old token still works until its own expiry (stateless, no revocation).
	now = epoch.Add(50 * time.Minute)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	handler.ServeHTTP(rec, req)
	if seen != RoleAdmin {
		t.Error("old token should remain valid until its expiry")
	}

	// Idle past expiry: anonymous, no reissue.
	now = epoch.Add(2 * time.Hour)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	handler.ServeHTTP(rec, req)
	if seen != RoleAnonymous {
		t.Error("expired session must fall back to anonymous")
	}
}

func TestLimiter(t *testing.T) {
	now := epoch
	l := NewLimiter(3, 15*time.Minute, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d within budget rejected", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("fourth attempt should be rejected")
	}
	// A different key has its own budget.
	if !l.Allow("5.6.7.8") {
		t.Error("a different IP should not be punished")
	}
	// The window resets.
	now = epoch.Add(16 * time.Minute)
	if !l.Allow("1.2.3.4") {
		t.Error("after the window the budget should reset")
	}
}
