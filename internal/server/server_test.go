package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JerryBian/splash/internal/config"
	"github.com/JerryBian/splash/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.Env = config.Test
	// Port 0 lets the OS choose, so parallel tests never collide.
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.ShutdownTimeout = 3 * time.Second
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test configuration is invalid: %v", err)
	}
	return cfg
}

// testDB is a real SQLite database in memory: these tests exercise the same
// wiring the binary runs, including the storage layer.
func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(":memory:", nil)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewRejectsANilStore(t *testing.T) {
	if _, err := New(testConfig(t), discardLogger(), nil); err == nil {
		t.Fatal("New accepted a nil database")
	}
}

func TestNewAcceptsANilLogger(t *testing.T) {
	srv, err := New(testConfig(t), nil, testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Handler() == nil {
		t.Error("Handler() is nil")
	}
}

func TestHandlerServesTheAPIAndTheFrontend(t *testing.T) {
	srv, err := New(testConfig(t), discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := httptest.NewServer(srv.Handler())
	t.Cleanup(test.Close)

	// An API route answers JSON.
	resp, err := test.Client().Get(test.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("health status = %d, body %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Errorf("health body = %s", body)
	}

	// Everything else is the frontend's.
	resp, err = test.Client().Get(test.URL + "/some/client/route")
	if err != nil {
		t.Fatalf("GET client route: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Errorf("Content-Type = %q, want HTML from the SPA handler", got)
	}
	if len(body) == 0 {
		t.Error("the SPA handler returned an empty body")
	}
}

func TestMiddlewareIsPartOfTheWiring(t *testing.T) {
	cfg := testConfig(t)
	cfg.CORSAllowedOrigins = []string{"https://app.example"}
	cfg.CORSMaxAge = new(10 * time.Minute)

	srv, err := New(cfg, discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := httptest.NewServer(srv.Handler())
	t.Cleanup(test.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, test.URL+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Origin", "https://app.example")

	resp, err := test.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := resp.Header.Get("X-Request-ID"); got == "" {
		t.Error("no X-Request-ID header; the RequestID middleware is not installed")
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q; the security headers are not installed", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Access-Control-Allow-Origin = %q; CORS is not wired to the configuration", got)
	}
}

func TestAuthMiddlewareIsWired(t *testing.T) {
	cfg := testConfig(t)
	srv, err := New(cfg, discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := httptest.NewServer(srv.Handler())
	t.Cleanup(test.Close)

	// Without a session the request is anonymous.
	resp, err := test.Client().Get(test.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatalf("GET me: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `"role":"anonymous"`) {
		t.Errorf("me = %s, want anonymous", body)
	}

	// With the configured credentials it becomes admin.
	resp, err = test.Client().Post(test.URL+"/api/v1/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"splash123"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("login set no cookie")
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, test.URL+"/api/v1/auth/me", nil)
	req.AddCookie(cookies[0])
	resp, err = test.Client().Do(req)
	if err != nil {
		t.Fatalf("me with cookie: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !strings.Contains(string(body), `"role":"admin"`) {
		t.Errorf("me with cookie = %s, want admin", body)
	}
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	srv, err := New(testConfig(t), discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	test := httptest.NewServer(srv.Handler())
	t.Cleanup(test.Close)

	resp, err := test.Client().Get(test.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("API Cache-Control = %q, want no-store", got)
	}
}

func TestPprofIsMountedOnlyWhenEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		cfg := testConfig(t)
		cfg.EnablePprof = true

		srv, err := New(cfg, discardLogger(), testDB(t))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		test := httptest.NewServer(srv.Handler())
		t.Cleanup(test.Close)

		resp, err := test.Client().Get(test.URL + "/debug/pprof/goroutine?debug=1")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), "goroutine") {
			t.Errorf("body does not look like a goroutine profile:\n%s", body)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		srv, err := New(testConfig(t), discardLogger(), testDB(t))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		test := httptest.NewServer(srv.Handler())
		t.Cleanup(test.Close)

		resp, err := test.Client().Get(test.URL + "/debug/pprof/goroutine?debug=1")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)

		// The request falls through to the frontend handler, so the profile is
		// not exposed. Profiles leak memory contents; the default must be off.
		if strings.Contains(string(body), "goroutine profile") {
			t.Error("pprof is reachable although it is disabled")
		}
		if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/html") {
			t.Errorf("Content-Type = %q, want the SPA handler's HTML", got)
		}
	})
}

func TestRunStopsWhenTheContextIsCanceled(t *testing.T) {
	srv, err := New(testConfig(t), discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	select {
	case <-srv.Ready():
	case err := <-done:
		t.Fatalf("Run returned before binding: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the server never became ready")
	}

	addr := srv.Addr()
	if addr == "" || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("Addr() = %q, want the resolved ephemeral address", addr)
	}

	resp, err := http.Get("http://" + addr + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET while running: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v after a clean shutdown, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}

	// The listener must be closed, not just idle.
	if conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Error("the port is still accepting connections after shutdown")
	}
}

func TestShutdownDrainsInFlightRequests(t *testing.T) {
	// A request that is already being served must be allowed to finish; that is
	// the entire point of a graceful shutdown, and the thing a rolling deploy
	// depends on. The handler is swapped for a slow one (this test lives in
	// package server, so it can reach the field) to make "in flight" precise.
	srv, err := New(testConfig(t), discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	<-srv.Ready()

	type result struct {
		status int
		err    error
	}
	inFlight := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + srv.Addr() + "/api/v1/health")
		if err != nil {
			inFlight <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.ReadAll(resp.Body)
		inFlight <- result{status: resp.StatusCode}
	}()

	// Cancel while the slow handler is still working.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case res := <-inFlight:
		if res.err != nil {
			t.Errorf("the in-flight request failed: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Errorf("the in-flight request got status %d, want 200", res.status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the in-flight request never completed")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil after a graceful drain", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after draining")
	}
}

func TestRunReportsABusyAddress(t *testing.T) {
	// Hold the port so Run cannot bind it.
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = blocker.Close() }()

	cfg := testConfig(t)
	cfg.HTTPAddr = blocker.Addr().String()

	srv, err := New(cfg, discardLogger(), testDB(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = srv.Run(t.Context())
	if err == nil {
		t.Fatal("Run succeeded although the address was already taken")
	}
	if !strings.Contains(err.Error(), "listen") {
		t.Errorf("error = %v, want it to name the failure", err)
	}
}
