package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testLogger captures records so middleware can be asserted on what it logged,
// not just on what it wrote to the client.
func testLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

func TestRequestIDGeneratesOneWhenAbsent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	var seen string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	handler.ServeHTTP(rec, req)

	if seen == "" {
		t.Fatal("no request id reached the handler")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("response header = %q, handler saw %q", got, seen)
	}
	if len(seen) != 36 {
		t.Errorf("generated id %q does not look like a UUID", seen)
	}
}

func TestRequestIDKeepsAUsableIncomingID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, "trace-abc.123_XYZ")

	var seen string
	RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	})).ServeHTTP(rec, req)

	if seen != "trace-abc.123_XYZ" {
		t.Errorf("handler saw %q, want the caller's id preserved", seen)
	}
}

func TestRequestIDRejectsUnsafeValues(t *testing.T) {
	// The value ends up in log lines and a response header, so anything that
	// could forge either is replaced with a generated id.
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"newline", "abc\nlevel=ERROR msg=forged"},
		{"carriage return", "abc\rdef"},
		{"space", "abc def"},
		{"quotes", `abc"def`},
		{"slash", "abc/def"},
		{"unicode", "abc😀"},
		{"too long", strings.Repeat("a", maxRequestIDLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(RequestIDHeader, tt.value)

			var seen string
			RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = RequestIDFrom(r.Context())
			})).ServeHTTP(rec, req)

			if seen == tt.value && tt.value != "" {
				t.Errorf("the unsafe value %q was passed through", tt.value)
			}
			if len(seen) != 36 {
				t.Errorf("id = %q, want a freshly generated UUID", seen)
			}
			if got := rec.Header().Get(RequestIDHeader); got != seen {
				t.Errorf("response header = %q, want %q", got, seen)
			}
		})
	}
}

func TestRequestIDFromAnEmptyContext(t *testing.T) {
	if got := RequestIDFrom(t.Context()); got != "" {
		t.Errorf("RequestIDFrom = %q, want empty when the middleware is absent", got)
	}
}

func TestLoggingRecordsTheRequest(t *testing.T) {
	log, buf := testLogger(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "short and stout")
	})
	handler := Chain(inner, RequestID, Logging(log))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items", strings.NewReader("{}"))
	req.Header.Set("User-Agent", "go-test")
	handler.ServeHTTP(rec, req)

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not JSON: %v\n%s", err, buf.String())
	}

	checks := map[string]any{
		"msg":       "request",
		"method":    "POST",
		"path":      "/api/v1/items",
		"status":    float64(http.StatusTeapot),
		"bytes":     float64(len("short and stout")),
		"userAgent": "go-test",
		"proto":     "HTTP/1.1",
	}
	for key, want := range checks {
		if record[key] != want {
			t.Errorf("%s = %v, want %v", key, record[key], want)
		}
	}
	if record["duration"] == nil {
		t.Error("the record has no duration")
	}
	if record["requestId"] == nil {
		t.Error("the record has no request id; RequestID must wrap Logging")
	}
	// 418 is a 4xx, which is a client mistake and should be a warning.
	if record["level"] != "WARN" {
		t.Errorf("level = %v, want WARN for a 4xx", record["level"])
	}
}

func TestLoggingLevelFollowsStatus(t *testing.T) {
	tests := []struct {
		status int
		level  string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusCreated, "INFO"},
		{http.StatusMovedPermanently, "INFO"},
		{http.StatusBadRequest, "WARN"},
		{http.StatusNotFound, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
		{http.StatusServiceUnavailable, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			log, buf := testLogger(t)
			handler := Logging(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

			var record map[string]any
			if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
				t.Fatalf("log output is not JSON: %v", err)
			}
			if record["level"] != tt.level {
				t.Errorf("status %d logged at %v, want %s", tt.status, record["level"], tt.level)
			}
		})
	}
}

func TestStatusRecorderUnwrapReachesOptionalInterfaces(t *testing.T) {
	// http.ResponseController looks for Flush/Hijack through Unwrap. Without
	// Unwrap, wrapping a handler would silently break streaming responses and
	// WebSocket upgrades.
	inner := &flushableResponseWriter{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	controller := http.NewResponseController(rec)
	if err := controller.Flush(); err != nil {
		t.Fatalf("Flush through the wrapper: %v", err)
	}
	if !inner.flushed {
		t.Error("Flush did not reach the wrapped ResponseWriter")
	}
}

// flushableResponseWriter implements http.Flusher, whose Flush takes no
// arguments and returns nothing — the exact shape ResponseController probes for.
type flushableResponseWriter struct {
	http.ResponseWriter
	flushed bool
}

func (w *flushableResponseWriter) Flush() {
	w.flushed = true
}

func TestStatusRecorderIgnoresALateWriteHeader(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	rec.WriteHeader(http.StatusAccepted)
	rec.WriteHeader(http.StatusInternalServerError) // too late, must be ignored
	_, _ = rec.Write([]byte("body"))

	if inner.Code != http.StatusAccepted {
		t.Errorf("status = %d, want the first WriteHeader to win", inner.Code)
	}
	if rec.status != http.StatusAccepted {
		t.Errorf("recorded status = %d", rec.status)
	}
}

func TestStatusRecorderImpliesOKOnFirstWrite(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	n, err := rec.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if inner.Code != http.StatusOK {
		t.Errorf("status = %d, want an implied 200", inner.Code)
	}
	if rec.written != 5 {
		t.Errorf("written = %d", rec.written)
	}
}

func TestRecoveryTurnsAPanicIntoA500(t *testing.T) {
	log, buf := testLogger(t)

	handler := Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("something went badly wrong")
		}),
		RequestID,
		Recovery(log),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), CodeInternal) {
		t.Errorf("body = %s, want the JSON error envelope", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "badly wrong") {
		t.Errorf("the panic value reached the client: %s", rec.Body.String())
	}

	// The panic itself must be in the log, with a stack, or it is undebuggable.
	logged := buf.String()
	for _, want := range []string{"panic recovered in handler", "something went badly wrong", "stack", "/boom"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log does not mention %q:\n%s", want, logged)
		}
	}
}

func TestRecoveryLetsNormalResponsesThrough(t *testing.T) {
	log, buf := testLogger(t)

	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d", rec.Code)
	}
	if buf.Len() != 0 {
		t.Errorf("a clean request was logged as a panic:\n%s", buf.String())
	}
}

func TestCORSAnswersAPreflight(t *testing.T) {
	maxAge := 10 * time.Minute
	handler := CORS(CORSOptions{
		AllowedOrigins: []string{"https://app.example"},
		MaxAge:         &maxAge,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a preflight must not reach the wrapped handler")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/items", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	header := rec.Header()
	if got := header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("Allow-Methods = %q", got)
	}
	if got := header.Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Content-Type") {
		t.Errorf("Allow-Headers = %q", got)
	}
	if got := header.Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age = %q, want 600", got)
	}
	if got := header.Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want Origin so caches do not cross-contaminate", got)
	}
}

func TestCORSAnnotatesASimpleRequest(t *testing.T) {
	reached := false
	handler := CORS(CORSOptions{AllowedOrigins: []string{"https://app.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	req.Header.Set("Origin", "https://app.example")
	handler.ServeHTTP(rec, req)

	if !reached {
		t.Error("a simple CORS request never reached the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, RequestIDHeader) {
		t.Errorf("Expose-Headers = %q, want the request id readable by JavaScript", got)
	}
}

func TestCORSLeavesADisallowedOriginToTheBrowser(t *testing.T) {
	reached := false
	handler := CORS(CORSOptions{AllowedOrigins: []string{"https://app.example"}})(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	req.Header.Set("Origin", "https://evil.example")
	handler.ServeHTTP(rec, req)

	// The request still runs — CORS is enforced by the browser, not the server —
	// but no headers are added, so the browser blocks the response and the
	// developer console says why.
	if !reached {
		t.Error("a disallowed origin should not change server-side behaviour")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want none for a disallowed origin", got)
	}
}

func TestCORSWildcardWithCredentialsRequestedDoesNotEnableCredentials(t *testing.T) {
	// "*" plus Allow-Credentials would let any site on the internet make
	// authenticated requests as the user. The middleware refuses that
	// combination rather than honouring it, which is the safe reading of an
	// almost-certainly mistaken configuration.
	handler := CORS(CORSOptions{AllowedOrigins: []string{"*"}, AllowCredentials: true})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example" {
		t.Errorf("Allow-Origin = %q, want the echoed origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want none with a wildcard allow-list", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want Origin since the response now depends on it", got)
	}
}

func TestCORSWildcardWithoutCredentials(t *testing.T) {
	handler := CORS(CORSOptions{AllowedOrigins: []string{"*"}})(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	// With "*" the response is the same for every origin, so no Vary is needed.
	if got := rec.Header().Get("Vary"); strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want no Origin entry for a literal wildcard", got)
	}
}

func TestCORSEnablesCredentialsForAnExplicitAllowList(t *testing.T) {
	handler := CORS(CORSOptions{
		AllowedOrigins:   []string{"https://app.example"},
		AllowCredentials: true,
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
}

func TestCORSWithoutAnAllowListIsANoOp(t *testing.T) {
	handler := CORS(CORSOptions{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want none when CORS is not configured", got)
	}
}

func TestCORSSkipsNonBrowserRequests(t *testing.T) {
	handler := CORS(CORSOptions{AllowedOrigins: []string{"https://app.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q on a request with no Origin header", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Permissions-Policy":         "geolocation=(), microphone=(), camera=()",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	// The bundle Vite emits is a separate hashed file, so inline scripts must
	// stay blocked; anything looser defeats the point.
	if !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("CSP allows inline scripts: %s", csp)
	}
	if strings.Contains(csp, "unsafe-inline'") && !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Errorf("CSP has unsafe-inline outside style-src: %s", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP does not block framing: %s", csp)
	}
}

func TestNoCache(t *testing.T) {
	handler := NoCache(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/items", nil))

	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestChainAppliesMiddlewaresOutermostFirst(t *testing.T) {
	var order []string
	mark := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "in:"+name)
				next.ServeHTTP(w, r)
				order = append(order, "out:"+name)
			})
		}
	}

	handler := Chain(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		mark("first"),
		mark("second"),
	)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	want := []string{"in:first", "in:second", "handler", "out:second", "out:first"}
	if strings.Join(order, " ") != strings.Join(want, " ") {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestChainWithNoMiddlewareReturnsTheHandler(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	rec := httptest.NewRecorder()
	Chain(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusAccepted {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestStrictTransportSecurity(t *testing.T) {
	handler := StrictTransportSecurity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=31536000; includeSubDomains" {
		t.Errorf("HSTS header = %q", got)
	}
}
